"""Run the starter bot on turns built from the SDK's fixture turn."""

import json
import logging
from collections.abc import Mapping
from typing import Any

import pytest
from island_battles_client import MEMORY_LIMIT_BYTES, BotTurn, BotTurnResult
from island_battles_client.fixture import fixture_turn_data

from bot import play

# The server's generation IDs are shared by every game and only increase, so a
# game's first turn can have ID 5538. The turns below have IDs like that, so a
# bot that reads `turn.generation.id` as the turn number fails these tests.
FIRST_GENERATION_ID = 5538


def game_turn(turn_number: int = 1, memory: Mapping[str, Any] | None = None) -> BotTurn:
    """The fixture's board as turn `turn_number`, with last turn's memory."""
    data = fixture_turn_data()
    data["turn_number"] = turn_number
    data["generation"]["id"] = FIRST_GENERATION_ID + turn_number - 1
    data["memory"] = memory
    return BotTurn.model_validate(data)


def test_result_is_valid() -> None:
    result = play(game_turn())
    assert isinstance(result, BotTurnResult)
    assert result.commands
    assert BotTurnResult.model_validate(result.model_dump(mode="json")) == result


def test_commands_stay_within_the_castles_soldiers() -> None:
    turn = game_turn()
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
    result = play(game_turn())
    assert result.memory is not None
    size = len(json.dumps(result.memory, separators=(",", ":")).encode())
    assert size < MEMORY_LIMIT_BYTES // 100


def test_memory_counts_across_turns() -> None:
    first = play(game_turn(1, None))
    second = play(game_turn(2, first.memory))
    assert first.memory is not None
    assert second.memory is not None
    assert second.memory["sent_total"] == 2 * first.memory["sent_total"]


def test_same_input_gives_the_same_answer() -> None:
    first = play(game_turn(3, {"sent_total": 5}))
    second = play(game_turn(3, {"sent_total": 5}))
    assert first == second


def test_logs_the_turn_number_not_the_generation_id(
    caplog: pytest.LogCaptureFixture,
) -> None:
    with caplog.at_level(logging.INFO, logger="island_battles.player"):
        play(game_turn(1))
    assert caplog.messages
    assert caplog.messages[-1].startswith("turn 1 of 299:")
