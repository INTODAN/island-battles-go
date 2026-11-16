"""The upgrade cost table in a ``BotTurn``, and the example bot that reads it."""

import pytest
from pydantic import ValidationError

from examples.bot import play
from island_battles_client import (
    BotTurn,
    UpgradeType,
    burst_castle,
    burst_potential,
    can_burst,
    next_upgrade_cost,
    production,
)
from island_battles_client.fixture import fixture_turn, fixture_turn_data


def upgrades_bought(turn: BotTurn) -> set[int]:
    return {
        command.root.parameters.castle_id
        for command in play(turn).commands
        if command.root.command_type == "upgrade_castle"
    }


def test_next_upgrade_cost_reads_the_turns_table() -> None:
    turn = fixture_turn()
    castle = turn.generation.castles[0]
    assert next_upgrade_cost(turn, castle, UpgradeType.speed) == 10
    assert next_upgrade_cost(turn, castle, "clash_stop_generations") == 10
    castle.upgrades[UpgradeType.speed] = 4
    assert next_upgrade_cost(turn, castle, UpgradeType.speed) is None


def test_next_upgrade_cost_is_none_for_a_type_the_table_lacks() -> None:
    data = fixture_turn_data()
    data["upgrades"] = []
    turn = BotTurn.model_validate(data)
    assert next_upgrade_cost(turn, turn.generation.castles[0], "speed") is None


def test_the_example_bot_buys_upgrades_at_the_costs_in_its_turn() -> None:
    # Castle 0 has 30 soldiers and castle 3 has 12; speed level 2 costs 10.
    assert upgrades_bought(fixture_turn()) == {0, 3}
    data = fixture_turn_data()
    for upgrade in data["upgrades"]:
        for level in upgrade["levels"]:
            if upgrade["type"] == "speed" and level["level"] == 2:
                level["soldier_cost"] = 20
    # The table changed, so castle 3 can no longer afford it.
    assert upgrades_bought(BotTurn.model_validate(data)) == {0}


def test_production_and_burst_potential_read_the_turns_table() -> None:
    turn = fixture_turn()
    castle = turn.generation.castles[0]
    assert production(turn, castle) == 1
    assert burst_potential(turn, castle) == 0
    assert can_burst(castle)
    upgraded = castle.model_copy(
        update={
            "upgrades": {
                UpgradeType.generation_amount: 4,
                UpgradeType.burst_potential: 3,
            }
        }
    )
    assert production(turn, upgraded) == 4
    assert burst_potential(turn, upgraded) == 8
    assert not can_burst(castle.model_copy(update={"burst_debt": 1}))
    assert not can_burst(castle.model_copy(update={"pending_burst": 4}))


def test_burst_castle_builds_the_command() -> None:
    command = burst_castle(3, 8)
    assert command.model_dump(mode="json") == {
        "command_type": "burst_castle",
        "parameters": {"castle_id": 3, "soldier_amount": 8},
    }
    with pytest.raises(ValidationError):
        burst_castle(3, 0)
