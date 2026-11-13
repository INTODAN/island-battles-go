"""Members upload bot packages to their lobby from both command lines, and
everyone in the lobby sees them (#221). The backend here is the local server
host, which keeps the packages on disk behind its own fake S3."""

import asyncio
import io
import json
import sys
import zipfile
from pathlib import Path

import httpx
import pytest
from integration.test_live import BASE, create
from integration.test_live import guests as guests_fixture
from lobby_client import Scope
from test_lobby_binding import game_auth

guests = guests_fixture
pytestmark = pytest.mark.asyncio

ROOT = Path(__file__).resolve().parents[2]
PYTHON_CLIENT = ROOT / "clients/island-battles-python"


def package(manifest: dict, files: dict[str, bytes]) -> bytes:
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as archive:
        archive.writestr("island-battles-bot.json", json.dumps(manifest))
        for name, data in files.items():
            archive.writestr(name, data)
    return buffer.getvalue()


PYTHON_ZIP = package({"format": 1, "language": "python", "python": "3.14", "entry": "bot:play"}, {"site/bot.py": b"def play(turn): ...\n"})
NATIVE_ZIP = package({"format": 1, "language": "native", "command": ["./bot", "worker"]}, {"bot": b"\x7fELF"})
BAD_ZIP = package({"format": 1, "language": "python", "python": "3.14", "entry": "bot"}, {})


async def command(*arguments: str, cwd: Path) -> tuple[int, str, str]:
    process = await asyncio.create_subprocess_exec(
        *arguments, cwd=cwd, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
    output, error = await asyncio.wait_for(process.communicate(), 300)
    return process.returncode, output.decode(), error.decode()


async def upload_token(http: httpx.AsyncClient, player, lobby_id: str) -> str:
    response = await http.post(f"/game/lobbies/{lobby_id}/bot-packages/upload-token",
                               headers=await game_auth(player))
    assert response.status_code == 200, response.text
    return response.json()["token"]


async def test_members_upload_from_both_command_lines_and_everyone_sees_them(guests, tmp_path):
    alice, bob, stranger = await guests("Alice"), await guests("Bob"), await guests("Stranger")
    room = await create(alice, "island-battles")
    scope = Scope.from_snapshot(room)
    await bob.join_lobby(game_key=scope.game_key, join_code=room.lobby.join_code)
    for name, data in {"python.zip": PYTHON_ZIP, "native.zip": NATIVE_ZIP, "bad.zip": BAD_ZIP}.items():
        (tmp_path / name).write_bytes(data)

    async with httpx.AsyncClient(base_url=BASE) as http:
        # Alice uploads a Python package with the Python SDK.
        token = await upload_token(http, alice, scope.lobby_id)
        status, output, error = await command(
            sys.executable, "-m", "island_battles_client.cli", "upload", "--game-url", BASE, f"--token={token}",
            str(tmp_path / "python.zip"), cwd=PYTHON_CLIENT)
        assert status == 0, error
        assert "python, entry bot:play" in output, output

        # Bob uploads a native package with the Go SDK.
        token = await upload_token(http, bob, scope.lobby_id)
        status, output, error = await command(
            "go", "run", "./clients/island-battles-go/cmd/island-battles-package", "upload", "--game-url", BASE,
            f"--token={token}", str(tmp_path / "native.zip"), cwd=ROOT)
        assert status == 0, error
        assert "native, command ./bot worker" in output, output

        # A bad package is refused with what is wrong, and changes nothing.
        status, _, error = await command(
            sys.executable, "-m", "island_battles_client.cli", "upload", "--game-url", BASE, f"--token={token}",
            str(tmp_path / "bad.zip"), cwd=PYTHON_CLIENT)
        assert status == 1
        assert 'the entry "bot" must be module:function' in error, error

        for player in (alice, bob):
            response = await http.get(f"/game/lobbies/{scope.lobby_id}/bot-packages",
                                      headers=await game_auth(player))
            assert response.status_code == 200, response.text
            listed = response.json()["packages"]
            assert [(item["owner_name"], item["language"], item["entry_or_command"]) for item in listed] == [
                ("Bob", "native", "./bot worker"), ("Alice", "python", "bot:play")]
        response = await http.get(f"/game/lobbies/{scope.lobby_id}/bot-packages",
                                  headers=await game_auth(stranger))
        assert response.status_code == 404
        # The game's routes take player tokens, not the guest credential.
        response = await http.get(f"/game/lobbies/{scope.lobby_id}/bot-packages",
                                  headers={"Authorization": f"Bearer {alice.credential}"})
        assert response.status_code == 401
