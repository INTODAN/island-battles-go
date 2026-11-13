"""The server sends each turn to hosted and uploaded bots (#225), and they
play a whole game with no client open.

The backend here is the local server host without runner functions, so it
runs the bot runners in its own process, as `./scripts/dev.sh` does: the
Python runner plays Alice's uploaded package, the bot template packaged by
the SDK's own `package` (#216), and the native runner delivers the turns of
a hosted bot, `island-battles-bot serve` on a test port, which submits them
itself. A default level 1 bot plays the third team. The test only watches.

Like the Python runner's end-to-end test, it packages the template from this
repository's SDK for this machine's architecture, since the local runner
runs the package here, and it needs uv and PyPI.
"""

import asyncio
import os
import platform
import shutil
import socket
import subprocess
import sys
import time
import urllib.request
from pathlib import Path
from uuid import uuid4

import httpx
import pytest
from integration.test_live import BASE
from integration.test_live import guests as guests_fixture
from lobby_api_client.models import TeamDefinition
from lobby_client import Scope
from test_bot_packages import command, upload_token
from test_lobby_binding import choose_bot, game_auth

guests = guests_fixture
pytestmark = pytest.mark.asyncio

ROOT = Path(__file__).resolve().parents[2]
PYTHON_CLIENT = ROOT / "clients/island-battles-python"
TEMPLATE = ROOT / "templates/python-bot"

# The hosted bot sends half of each castle's soldiers to the weakest castle
# it doesn't own.
HOSTED_BOT = """
from island_battles_client import send_soldier_group, turn_result


def play(turn):
    castles = turn.generation.castles
    targets = [castle for castle in castles if castle.team_color != turn.team_color]
    commands = []
    for castle in castles:
        if castle.team_color == turn.team_color and castle.soldiers > 1 and targets:
            target = min(targets, key=lambda other: other.soldiers)
            commands.append(send_soldier_group(castle.id, target.id, castle.soldiers // 2))
    return turn_result(commands)
"""


def network_available() -> bool:
    try:
        urllib.request.urlopen("https://pypi.org/simple/", timeout=5).close()
    except OSError:
        return False
    return True


def host_platform() -> str:
    """uv's platform for this machine's packages, empty for the Lambda's own."""
    machine = platform.machine().lower()
    if machine in ("aarch64", "arm64"):
        return ""
    return f"{machine}-manylinux_2_28"


def package_template(work: Path) -> Path:
    """The bot template, packaged by the SDK's `package` from this
    repository's SDK, which has the worker mode, for this machine."""

    def run(arguments: list[str], cwd: Path) -> None:
        done = subprocess.run(arguments, cwd=cwd, capture_output=True, text=True, check=False)
        assert done.returncode == 0, f"{' '.join(arguments)}:\n{done.stdout}\n{done.stderr}"

    run(["uv", "build", "--wheel", "--out-dir", str(work / "wheels"), str(PYTHON_CLIENT)], ROOT)
    [wheel] = (work / "wheels").glob("island_battles_python-*.whl")
    project = work / "bot"
    shutil.copytree(TEMPLATE, project, ignore=shutil.ignore_patterns(".venv", "dist", "__pycache__", "uv.lock"))
    pyproject = project / "pyproject.toml"
    text = pyproject.read_text().replace('"island-battles-python>=0.2.0"', '"island-battles-python"')
    assert '"island-battles-python"' in text
    pyproject.write_text(text + f'\n[tool.uv.sources]\nisland-battles-python = {{ path = "{wheel.as_posix()}" }}\n')
    run(["uv", "lock"], project)
    packaged = work / "island-battles-bot.zip"
    script = (
        "import sys\nfrom pathlib import Path\nfrom island_battles_client import bundle\n"
        "if sys.argv[3]:\n    bundle.PYTHON_PLATFORM = sys.argv[3]\n"
        "bundle.package(Path(sys.argv[1]), 'bot:play', Path(sys.argv[2]))\n"
    )
    run([sys.executable, "-I", "-c", script, str(project), str(packaged), host_platform()], ROOT)
    return packaged


def free_port() -> int:
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]


@pytest.fixture
def hosted_bot(tmp_path):
    """`island-battles-bot serve` on a test port, as a hosted bot's server."""
    (tmp_path / "hosted_bot.py").write_text(HOSTED_BOT)
    port = free_port()
    environment = {**os.environ, "PYTHONPATH": str(PYTHON_CLIENT)}
    # It submits to the test backend on http://127.0.0.1, which serve always
    # allows; a backend elsewhere would need --allow-submit-host.
    process = subprocess.Popen(
        [sys.executable, "-m", "island_battles_client.cli", "serve", "hosted_bot:play", "--port", str(port)],
        cwd=tmp_path, env=environment, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    url = f"http://127.0.0.1:{port}"
    for _ in range(100):
        try:
            if httpx.get(f"{url}/health", timeout=1).status_code == 200:
                break
        except httpx.HTTPError:
            pass
        assert process.poll() is None, process.stdout.read()
        time.sleep(0.1)
    else:
        raise AssertionError("serve didn't start")
    yield f"{url}/turn", process
    process.terminate()
    process.wait(timeout=10)


@pytest.mark.skipif(sys.platform != "linux", reason="the package's dependencies are built for Linux")
@pytest.mark.skipif(not shutil.which("uv") or not network_available(), reason="needs uv and PyPI")
async def test_uploaded_and_hosted_bots_play_a_whole_game_with_no_client(guests, tmp_path, hosted_bot):
    hosted_url, hosted_process = hosted_bot
    packaged = await asyncio.to_thread(package_template, tmp_path)

    alice = await guests("Alice")
    room = await alice.create_lobby(
        game_key="island-battles", name="Server bots",
        teams=[TeamDefinition(name="Red"), TeamDefinition(name="Blue"), TeamDefinition(name="Green")],
        request_id=uuid4(), game_settings={"turn_timeout_seconds": None})
    scope = Scope.from_snapshot(room)
    red, blue, green = (str(team.id) for team in room.teams)

    async with httpx.AsyncClient(base_url=BASE, timeout=30) as http:
        # Alice uploads the packaged template and plays Red with it.
        token = await upload_token(http, alice, scope.lobby_id)
        status, output, error = await command(
            sys.executable, "-m", "island_battles_client.cli", "upload", "--game-url", BASE, f"--token={token}",
            str(packaged), cwd=PYTHON_CLIENT)
        assert status == 0, error
        listed = (await http.get(f"/game/lobbies/{scope.lobby_id}/bot-packages", headers=await game_auth(alice))).json()
        [uploaded] = listed["packages"]
        await alice.select_team(scope, red)
        await choose_bot(alice, scope.lobby_id, {"kind": "uploaded", "package_id": uploaded["package_id"]})
        # Blue is the hosted bot, and Green a default bot. The local server
        # host accepts a hosted bot on this machine.
        await alice.add_seat(scope, blue, {"bot_url": hosted_url})
        await alice.add_seat(scope, green, {"bot_level": 1})
        await alice.set_ready(scope, True)
        await alice.start(scope)

        # Nobody relays or submits anything from here: the test only reads.
        game = f"/game/from-lobby/{scope.lobby_id}"
        auth = await game_auth(alice)
        deadline = asyncio.get_running_loop().time() + 600
        while True:
            assert asyncio.get_running_loop().time() < deadline, "the game didn't end"
            response = await http.get(f"{game}/generation", headers=auth)
            if response.status_code == 401:
                auth = await game_auth(alice)
                continue
            assert response.status_code == 200, response.text
            state = response.json()
            if state.get("outcome") is not None:
                break
            await asyncio.sleep(0.5)

        # Turns have no time limit, so the game reached its end only because
        # every team still in it completed every turn: the uploaded bot and
        # the hosted bot too, by themselves.
        assert state["outcome"]["reason"] in ("last_team_standing", "all_eliminated", "generation_cap"), state["outcome"]
        assert state["bot"]["kind"] == "uploaded"
        # The template remembers how many soldiers it sent.
        assert state["memory"]["sent_total"] > 0, state["memory"]
        # The uploaded bot's own lines of its last turn, and no line that
        # the server couldn't start it.
        logs = await http.get(f"{game}/logs", params={"generation_id": state["generation_id"]}, headers=auth)
        assert logs.status_code == 200, logs.text
        for player in logs.json()["players"]:
            assert not any("couldn't start your bot" in line["message"] for line in player["lines"]), player
    assert hosted_process.poll() is None
