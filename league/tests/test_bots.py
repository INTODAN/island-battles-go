"""Every bot of the league keeps the rules that make its games count."""

import ast
import importlib
import sys
from collections.abc import Callable, Mapping
from typing import Any

import pytest
from island_battles_client import (
    BotTurn,
    BotTurnResult,
    UpgradeType,
    burst_castle,
    send_soldier_group,
    turn_result,
    upgrade_castle,
)
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


def game_turn(
    turn_number: int = 1,
    memory: Mapping[str, Any] | None = None,
    team_color: int | None = None,
    board: str = "the fixture",
) -> BotTurn:
    data = fixture_turn_data()
    data["turn_number"] = turn_number
    data["generation"]["id"] = FIRST_GENERATION_ID + turn_number - 1
    data["memory"] = memory
    if team_color is not None:
        data["team_color"] = team_color
    BOARDS[board](data["generation"], data["team_color"])
    return BotTurn.model_validate(data)


def _group(group_id: int, team_color: int, soldiers: int, origin: dict, target: dict, share: float) -> dict:
    """A group of a team on its way from one castle to another, `share` of the way there."""
    return {
        "id": group_id,
        "team_color": team_color,
        "soldiers": soldiers,
        "origin_castle_id": origin["id"],
        "destination_castle_id": target["id"],
        "x": origin["x"] + (target["x"] - origin["x"]) * share,
        "y": origin["y"] + (target["y"] - origin["y"]) * share,
        "clash_radius": 0.25,
        "upgrades": dict(origin["upgrades"]),
    }


def _under_attack(generation: dict, team_color: int) -> None:
    """Every castle of the team has a large enemy group about to land on it."""
    enemy = next(castle for castle in generation["castles"] if castle["team_color"] not in (0, team_color))
    own = [castle for castle in generation["castles"] if castle["team_color"] == team_color]
    generation["soldier_groups"] = [
        _group(index, enemy["team_color"], 25 + 20 * index, enemy, castle, 0.9) for index, castle in enumerate(own)
    ]


def _debt_and_top_levels(generation: dict, team_color: int) -> None:
    """The team's castles are rich; one owes a burst, one has every upgrade at its top level."""
    _under_attack(generation, team_color)
    own = [castle for castle in generation["castles"] if castle["team_color"] == team_color]
    for index, castle in enumerate(own):
        castle["soldiers"] = 200
        castle["upgrades"]["burst_potential"] = 2
        if index == 0:
            castle["burst_debt"] = 3
        else:
            castle["upgrades"] = {"burst_potential": 4, "clash_stop_generations": 3, "generation_amount": 4, "speed": 4}


def _poor(generation: dict, team_color: int) -> None:
    """The team's castles hold no soldiers or one."""
    _under_attack(generation, team_color)
    own = [castle for castle in generation["castles"] if castle["team_color"] == team_color]
    for index, castle in enumerate(own):
        castle["soldiers"] = index % 2


def _no_enemy_castles(generation: dict, team_color: int) -> None:
    """Every other team's castles are neutral; its groups are still out."""
    for castle in generation["castles"]:
        if castle["team_color"] != team_color:
            castle["team_color"] = 0


def _no_castles(generation: dict, team_color: int) -> None:
    """The team has lost its castles and has one group left."""
    enemy = next(castle for castle in generation["castles"] if castle["team_color"] not in (0, team_color))
    own = [castle for castle in generation["castles"] if castle["team_color"] == team_color]
    generation["soldier_groups"] = [_group(0, team_color, 9, own[0], enemy, 0.5)]
    for castle in own:
        castle["team_color"] = enemy["team_color"]


def _fixture(generation: dict, team_color: int) -> None:
    """The SDK's fixture board, unchanged."""


BOARDS = {
    "the fixture": _fixture,
    "under attack": _under_attack,
    "debt and top levels": _debt_and_top_levels,
    "poor": _poor,
    "no enemy castles": _no_enemy_castles,
    "no castles": _no_castles,
}


def teams_of_the_fixture() -> list[int]:
    return sorted({castle["team_color"] for castle in fixture_turn_data()["generation"]["castles"]} - {0})


def rejection(turn: BotTurn, result: BotTurnResult) -> str | None:
    """Why the server would reject the result, or None: the rules of `validateCommands`.

    The sends and upgrades are checked in order, each against what the ones
    before it spent; then the bursts, each against the castle's potential with
    every level the result bought.
    """
    castles = {castle.id: castle for castle in turn.generation.castles}
    soldiers = {castle.id: castle.soldiers for castle in turn.generation.castles}
    levels = {castle.id: dict(castle.upgrades) for castle in turn.generation.castles}
    table = {upgrade.type: {entry.level: entry for entry in upgrade.levels} for upgrade in turn.upgrades}
    commands = result.model_dump(mode="json")["commands"]
    for command in commands:
        parameters = command["parameters"]
        castle_id = parameters.get("origin_castle_id", parameters.get("castle_id"))
        if castle_id not in castles or castles[castle_id].team_color != turn.team_color:
            return f"castle {castle_id} is not the team's"
        if command["command_type"] == "send_soldier_group":
            if parameters["destination_castle_id"] == castle_id or parameters["destination_castle_id"] not in castles:
                return f"castle {castle_id} sends to {parameters['destination_castle_id']}"
            soldiers[castle_id] -= parameters["soldier_amount"]
        elif command["command_type"] == "upgrade_castle":
            upgrade_type = parameters["upgrade_type"]
            levels[castle_id][upgrade_type] += 1
            if levels[castle_id][upgrade_type] not in table[upgrade_type]:
                return f"castle {castle_id} upgrades {upgrade_type} past its top level"
            soldiers[castle_id] -= table[upgrade_type][levels[castle_id][upgrade_type]].soldier_cost
        if soldiers[castle_id] < 0:
            return f"castle {castle_id} spends {-soldiers[castle_id]} soldiers more than it has"
    burst: set[int] = set()
    for command in commands:
        if command["command_type"] != "burst_castle":
            continue
        castle_id, amount = command["parameters"]["castle_id"], command["parameters"]["soldier_amount"]
        potential = table[UpgradeType.burst_potential][levels[castle_id][UpgradeType.burst_potential]].value
        if castles[castle_id].burst_debt > 0 or castle_id in burst:
            return f"castle {castle_id} bursts with a debt, or twice"
        if amount > potential:
            return f"castle {castle_id} bursts {amount}, above its potential of {potential:g}"
        burst.add(castle_id)
    return None


def test_the_league_has_bots() -> None:
    assert NAMES


@pytest.mark.parametrize("bot", ROSTER, ids=NAMES)
def test_bot_toml_is_complete(bot: Bot) -> None:
    assert NAME.fullmatch(bot.name) and not ANCHOR.fullmatch(bot.name)
    assert bot.idea.strip()
    assert bot.parent == "" or bot.parent != bot.name


@pytest.mark.parametrize("name", NAMES)
@pytest.mark.parametrize("board", BOARDS)
@pytest.mark.parametrize("team_color", teams_of_the_fixture())
def test_result_is_one_the_server_accepts(name: str, team_color: int, board: str) -> None:
    turn = game_turn(team_color=team_color, board=board)
    result = play_of(name)(turn)
    assert isinstance(result, BotTurnResult)
    assert BotTurnResult.model_validate(result.model_dump(mode="json")) == result
    assert rejection(turn, result) is None


def test_the_check_rejects_what_the_server_rejects() -> None:
    """The check above would be worthless if it let these through."""
    turn = game_turn(board="debt and top levels")
    owing, rich = [castle for castle in turn.generation.castles if castle.team_color == turn.team_color]
    enemy = next(castle for castle in turn.generation.castles if castle.team_color == 2)
    rejected = {
        "overspending across commands": [
            send_soldier_group(rich.id, enemy.id, 150),
            send_soldier_group(rich.id, enemy.id, 51),
        ],
        "a castle of another team": [send_soldier_group(enemy.id, rich.id, 1)],
        "an upgrade past the top level": [upgrade_castle(rich.id, UpgradeType.speed)],
        "a burst with a debt": [burst_castle(owing.id, 1)],
        "a burst above the potential": [burst_castle(rich.id, 17)],
        "two bursts of one castle": [burst_castle(rich.id, 1), burst_castle(rich.id, 1)],
    }
    for name, commands in rejected.items():
        assert rejection(turn, turn_result(commands)) is not None, name
    assert rejection(turn, turn_result([burst_castle(rich.id, 16), send_soldier_group(rich.id, enemy.id, 200)])) is None
    # A burst counts a level bought anywhere in the same result.
    fixture = game_turn()
    assert rejection(fixture, turn_result([burst_castle(0, 4), upgrade_castle(0, UpgradeType.burst_potential)])) is None
    assert rejection(fixture, turn_result([burst_castle(0, 4)])) is not None


MUTABLE_NODES = (ast.Dict, ast.List, ast.Set, ast.DictComp, ast.ListComp, ast.SetComp)
MUTABLE_CALLS = {"dict", "list", "set", "defaultdict", "Counter", "deque", "OrderedDict", "bytearray"}


def impurities(source: str) -> list[str]:
    """What makes a bot's module depend on more than the turn: imports, state, forbidden calls."""
    found = []
    tree = ast.parse(source)
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            modules = [alias.name for alias in node.names]
        elif isinstance(node, ast.ImportFrom):
            # One dot stays inside the bot's own package; two reach another bot.
            modules = [] if node.level == 1 else [node.module or "." * node.level]
            if node.level > 1:
                found.append(f"imports from outside its package: {'.' * node.level}{node.module or ''}")
                modules = []
        else:
            modules = []
        for module in modules:
            if module.split(".")[0] not in ALLOWED_IMPORTS:
                found.append(f"imports {module}")
        if isinstance(node, ast.Global | ast.Nonlocal):
            found.append("keeps state in a global")
        if isinstance(node, ast.Call) and isinstance(node.func, ast.Name) and node.func.id in FORBIDDEN_CALLS:
            found.append(f"calls {node.func.id}")
    # A value that lives between turns must not be one a turn can change:
    # the same process plays many games.
    scopes = [tree, *(node for node in ast.walk(tree) if isinstance(node, ast.ClassDef))]
    for scope in scopes:
        for statement in scope.body:
            if not isinstance(statement, ast.Assign | ast.AnnAssign) or statement.value is None:
                continue
            value = statement.value
            called = value.func.id if isinstance(value, ast.Call) and isinstance(value.func, ast.Name) else ""
            if isinstance(value, MUTABLE_NODES) or called in MUTABLE_CALLS:
                found.append(f"keeps a value that a turn can change at line {statement.lineno}")
    return found


@pytest.mark.parametrize("name", NAMES)
def test_bot_is_self_contained_and_pure(name: str) -> None:
    """A bot imports only the SDK, a few pure standard modules and itself, and keeps nothing between turns."""
    for path in sorted((BOTS / name).rglob("*.py")):
        assert impurities(path.read_text(encoding="utf-8")) == [], str(path)
    assert f"bots.{name}.bot" in sys.modules or importlib.import_module(f"bots.{name}.bot")


def test_the_purity_check_finds_what_it_is_for() -> None:
    for source, problem in {
        "from ..economist.bot import GUARD": "imports from outside its package: ..economist.bot",
        "import random": "imports random",
        "from os import path": "imports os",
        "SEEN = {}": "keeps a value that a turn can change at line 1",
        "class Board:\n    cache: list[int] = []": "keeps a value that a turn can change at line 2",
        "PLANS = defaultdict(int)": "keeps a value that a turn can change at line 1",
        "def play(turn):\n    global COUNT": "keeps state in a global",
        "data = open('plan.txt')": "calls open",
    }.items():
        assert impurities(source) == [problem], source
    assert impurities("from .plan import choose\nfrom math import ceil\nWAVE = 8\nSHARES = (0.5, 1)") == []
