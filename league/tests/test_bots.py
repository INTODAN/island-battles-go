"""Every bot of the league keeps the rules that make its games count."""

import ast
import importlib
import json
import logging
import sys
from collections.abc import Callable, Mapping
from typing import Any

import pytest
from island_battles_client import MEMORY_LIMIT_BYTES, BotTurn, BotTurnResult
from island_battles_client.fixture import fixture_turn_data

from league_tools.paths import BOTS
from league_tools.roster import ANCHOR, NAME, Bot, load_roster

ROSTER = load_roster()
NAMES = [bot.name for bot in ROSTER]

# What a bot may import besides the SDK and its own modules: nothing that
# reads a clock, a file, the network or the process, and no randomness.
ALLOWED_IMPORTS = {
    "__future__",
    "bisect",
    "collections",
    "dataclasses",
    "enum",
    "functools",
    "heapq",
    "island_battles_client",
    "itertools",
    "math",
    "operator",
    "typing",
}
FORBIDDEN_CALLS = {"open", "eval", "exec", "compile", "__import__", "input", "breakpoint"}

# The server's generation IDs don't start at 1, so a bot that reads the ID as
# the turn number shows it here.
FIRST_GENERATION_ID = 5538


def play_of(name: str) -> Callable[[BotTurn], BotTurnResult]:
    return importlib.import_module(f"bots.{name}.bot").play


def game_turn(turn_number: int = 1, memory: Mapping[str, Any] | None = None, team_color: int | None = None) -> BotTurn:
    data = fixture_turn_data()
    data["turn_number"] = turn_number
    data["generation"]["id"] = FIRST_GENERATION_ID + turn_number - 1
    data["memory"] = memory
    if team_color is not None:
        data["team_color"] = team_color
    return BotTurn.model_validate(data)


def teams_of_the_fixture() -> list[int]:
    return sorted({castle["team_color"] for castle in fixture_turn_data()["generation"]["castles"]} - {0})


def test_the_league_has_bots() -> None:
    assert NAMES


@pytest.mark.parametrize("bot", ROSTER, ids=NAMES)
def test_bot_toml_is_complete(bot: Bot) -> None:
    assert NAME.fullmatch(bot.name) and not ANCHOR.fullmatch(bot.name)
    assert bot.idea.strip()
    assert bot.parent == "" or bot.parent != bot.name


@pytest.mark.parametrize("name", NAMES)
@pytest.mark.parametrize("team_color", teams_of_the_fixture())
def test_result_is_valid_and_affordable(name: str, team_color: int) -> None:
    turn = game_turn(team_color=team_color)
    result = play_of(name)(turn)
    assert isinstance(result, BotTurnResult)
    assert BotTurnResult.model_validate(result.model_dump(mode="json")) == result
    castles = {castle.id: castle for castle in turn.generation.castles}
    soldiers = {castle.id: castle.soldiers for castle in turn.generation.castles}
    levels = {castle.id: dict(castle.upgrades) for castle in turn.generation.castles}
    costs = {(upgrade.type, entry.level): entry.soldier_cost for upgrade in turn.upgrades for entry in upgrade.levels}
    for command in result.model_dump(mode="json")["commands"]:
        parameters = command["parameters"]
        castle_id = parameters.get("origin_castle_id", parameters.get("castle_id"))
        assert castles[castle_id].team_color == team_color
        if command["command_type"] == "send_soldier_group":
            assert parameters["destination_castle_id"] != castle_id
            soldiers[castle_id] -= parameters["soldier_amount"]
        elif command["command_type"] == "upgrade_castle":
            levels[castle_id][parameters["upgrade_type"]] += 1
            soldiers[castle_id] -= costs[(parameters["upgrade_type"], levels[castle_id][parameters["upgrade_type"]])]
    assert all(amount >= 0 for amount in soldiers.values())


@pytest.mark.parametrize("name", NAMES)
def test_same_turn_gives_the_same_result(name: str) -> None:
    play = play_of(name)
    for turn_number in (1, 2, 150, 299):
        memory = play(game_turn(1)).memory if turn_number > 1 else None
        first = play(game_turn(turn_number, memory))
        second = play(game_turn(turn_number, memory))
        assert first.model_dump(mode="json") == second.model_dump(mode="json")


@pytest.mark.parametrize("name", NAMES)
def test_memory_is_a_small_json_object(name: str) -> None:
    result = play_of(name)(game_turn())
    if result.memory is not None:
        assert len(json.dumps(result.memory, separators=(",", ":")).encode()) < MEMORY_LIMIT_BYTES // 8


@pytest.mark.parametrize("name", NAMES)
def test_bot_logs_its_turn(name: str) -> None:
    """A game can only be studied if its bots say what they decided."""
    records: list[logging.LogRecord] = []

    class Collector(logging.Handler):
        def emit(self, record: logging.LogRecord) -> None:
            records.append(record)

    handler = Collector()
    logger = logging.getLogger("island_battles.player")
    logger.addHandler(handler)
    previous = logger.level
    logger.setLevel(logging.DEBUG)
    try:
        result = play_of(name)(game_turn())
    finally:
        logger.removeHandler(handler)
        logger.setLevel(previous)
    assert records or result.logs


@pytest.mark.parametrize("name", NAMES)
def test_bot_is_self_contained_and_pure(name: str) -> None:
    """A bot imports only the SDK, a few pure standard modules and itself."""
    for path in sorted((BOTS / name).rglob("*.py")):
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                modules = [alias.name for alias in node.names]
            elif isinstance(node, ast.ImportFrom):
                # A relative import stays inside the bot's own package.
                modules = [] if node.level else [node.module or ""]
            else:
                modules = []
            for module in modules:
                assert module.split(".")[0] in ALLOWED_IMPORTS, f"{path}: imports {module}"
            assert not isinstance(node, ast.Global | ast.Nonlocal), f"{path}: keeps state in a global"
            if isinstance(node, ast.Call) and isinstance(node.func, ast.Name):
                assert node.func.id not in FORBIDDEN_CALLS, f"{path}: calls {node.func.id}"
    assert f"bots.{name}.bot" in sys.modules or importlib.import_module(f"bots.{name}.bot")
