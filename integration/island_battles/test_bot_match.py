"""`island-battles-bot match` plays bots against each other on map files,
through the lobby, with no browser (#407).

A hosted bot, `island-battles-bot serve` on a test port, which the local
server host accepts, plays a default bot on two map files from both sides;
with uv and PyPI, an uploaded package, the bot template, plays the hosted bot
too. The command creates the lobby, seats the bots and saves each game's
result and demo.
"""

import asyncio
import json
import shutil
import sys
from pathlib import Path

import pytest
from integration.test_live import BASE
from test_bot_choice import PYTHON_CLIENT
from test_lobby_binding import GAME, circle_map
from test_server_bots import hosted_bot, network_available, package_template

hosted_bot = hosted_bot
pytestmark = pytest.mark.asyncio


async def match(*arguments: str) -> tuple[int, str, str]:
    process = await asyncio.create_subprocess_exec(
        sys.executable, "-m", "island_battles_client.cli", "match", "--lobby-url", BASE, "--game-url", GAME,
        *arguments, cwd=PYTHON_CLIENT, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
    try:
        output, error = await asyncio.wait_for(process.communicate(), 540)
    except TimeoutError:
        process.kill()
        await process.wait()
        raise
    return process.returncode, output.decode(), error.decode()


def map_files(directory: Path, *names: str) -> list[str]:
    directory.mkdir()
    paths = []
    for name in names:
        path = directory / f"{name}.json"
        path.write_text(json.dumps({**circle_map(2), "map_name": name}))
        paths.append(str(path))
    return paths


def check_results(output: str, directory: Path, expected: list[tuple[str, list[str]]]) -> list[dict]:
    results = [json.loads(line) for line in output.splitlines()]
    assert [(result["id"], result["players"]) for result in results] == expected
    for result in results:
        assert result["outcome"]["reason"] in ("last_team_standing", "all_eliminated", "generation_cap"), result
        assert result["winner"] in (*result["players"], None)
        assert json.loads((directory / f"{result['id']}.json").read_text()) == result
        demo = json.loads(Path(result["demo"]).read_text())
        assert (demo["format"], demo["game_id"], demo["outcome"]) == (
            "island-battles-demo", result["game_id"], result["outcome"])
        assert len(demo["frames"]) == result["generations"]
        assert [team["players"] for team in demo["teams"]] == [[name] for name in result["players"]], demo["teams"]
    # One lobby played them all, a round each.
    assert len({result["lobby_id"] for result in results}) == 1
    assert [result["lobby_round"] for result in results] == list(range(1, len(results) + 1))
    return results


async def test_a_hosted_bot_plays_a_default_bot_on_two_maps_from_both_sides(tmp_path, hosted_bot):
    hosted_url, hosted_process = hosted_bot
    maps = map_files(tmp_path / "maps", "strait", "crossing")
    out = tmp_path / "results"
    status, output, error = await match(
        "--map", *maps, "--seat", f"mine=hosted:{hosted_url}", "--seat", "default:1",
        "--max-generations", "50", "--output", str(out))
    assert status == 0, error
    check_results(output, out, [
        ("strait-1", ["mine", "level 1"]), ("strait-2", ["level 1", "mine"]),
        ("crossing-1", ["mine", "level 1"]), ("crossing-2", ["level 1", "mine"])])
    assert "lobby " in error and "crossing-2:" in error, error
    assert hosted_process.poll() is None


@pytest.mark.skipif(sys.platform != "linux", reason="the package's dependencies are built for Linux")
@pytest.mark.skipif(not shutil.which("uv") or not network_available(), reason="needs uv and PyPI")
async def test_an_uploaded_package_plays_a_hosted_bot_from_both_sides(tmp_path, hosted_bot):
    hosted_url, _ = hosted_bot
    packaged = await asyncio.to_thread(package_template, tmp_path)
    maps = map_files(tmp_path / "maps", "strait")
    out = tmp_path / "results"
    status, output, error = await match(
        "--map", *maps, "--seat", f"package:{packaged}", "--seat", f"hosted:{hosted_url}",
        "--turn-timeout", "60", "--max-generations", "50", "--output", str(out))
    assert status == 0, error
    assert "uploaded" in error, error
    check_results(output, out, [
        ("strait-1", ["island-battles-bot.zip", "127.0.0.1"]),
        ("strait-2", ["127.0.0.1", "island-battles-bot.zip"])])
