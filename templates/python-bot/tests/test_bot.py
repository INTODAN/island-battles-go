"""Run the starter bot on the SDK's fixture turn."""

import json
from collections.abc import Mapping
from typing import Any

from island_battles_client import MEMORY_LIMIT_BYTES, BotTurn, BotTurnResult
from island_battles_client.fixture import fixture_turn, fixture_turn_data

from bot import play


def turn_with_memory(memory: Mapping[str, Any] | None) -> BotTurn:
    """The fixture turn, with the memory a previous turn would have returned."""
    data = fixture_turn_data()
    data["memory"] = memory
    return BotTurn.model_validate(data)


def test_result_is_valid() -> None:
    result = play(fixture_turn())
    assert isinstance(result, BotTurnResult)
    assert result.commands
    assert BotTurnResult.model_validate(result.model_dump(mode="json")) == result


def test_commands_stay_within_the_castles_soldiers() -> None:
    turn = fixture_turn()
    result = play(turn)
    soldiers = {castle.id: castle.soldiers for castle in turn.generation.castles}
    costs = {"speed": 10}
    for command in result.model_dump(mode="json")["commands"]:
        parameters = command["parameters"]
        if command["command_type"] == "send_soldier_group":
            soldiers[parameters["origin_castle_id"]] -= parameters["soldier_amount"]
        else:
            soldiers[parameters["castle_id"]] -= costs[parameters["upgrade_type"]]
    assert all(amount >= 0 for amount in soldiers.values())


def test_memory_is_small_json() -> None:
    result = play(fixture_turn())
    assert result.memory is not None
    size = len(json.dumps(result.memory, separators=(",", ":")).encode())
    assert size < MEMORY_LIMIT_BYTES // 100


def test_memory_counts_across_turns() -> None:
    first = play(turn_with_memory(None))
    second = play(turn_with_memory(first.memory))
    assert first.memory is not None
    assert second.memory is not None
    assert second.memory["sent_total"] == 2 * first.memory["sent_total"]


def test_same_input_gives_the_same_answer() -> None:
    first = play(turn_with_memory({"sent_total": 5}))
    second = play(turn_with_memory({"sent_total": 5}))
    assert first == second
