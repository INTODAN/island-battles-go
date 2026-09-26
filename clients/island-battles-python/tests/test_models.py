import math

import pytest
from pydantic import ValidationError

from island_battles_client import (
    Castle,
    GameProtocolError,
    Generation,
    SoldierGroup,
    TeamType,
)
from island_battles_client._protocol import GenerationState


def make_entity(entity_type, **values):
    if entity_type is SoldierGroup:
        values.update(
            origin_castle_id=0,
            destination_castle_id=1,
            clash_radius=0.5,
            destination_castle=Castle(id=1, team_color=2, soldiers=10, x=3, y=4),
        )
    return entity_type(**values)


@pytest.mark.parametrize("origin_type", [Castle, SoldierGroup])
@pytest.mark.parametrize("destination_type", [Castle, SoldierGroup])
@pytest.mark.parametrize(
    "destination_position, expected",
    [((-2, -1), 0), ((1, 3), 5), ((-1.5, -0.5), math.sqrt(0.5))],
)
def test_distance_between_entity_types(
    origin_type, destination_type, destination_position, expected
):
    origin = make_entity(origin_type, id=0, team_color=1, soldiers=10, x=-2, y=-1)
    destination = make_entity(
        destination_type,
        id=1,
        team_color=2,
        soldiers=20,
        x=destination_position[0],
        y=destination_position[1],
    )
    assert origin.distance(destination) == pytest.approx(expected)
    assert destination.distance(origin) == pytest.approx(expected)


@pytest.mark.parametrize(
    "field",
    [
        "x",
        "y",
        "origin_castle_id",
        "destination_castle_id",
        "clash_radius",
        "destination_castle",
    ],
)
@pytest.mark.parametrize("missing", [True, False])
def test_soldier_group_requires_complete_data(field, missing):
    data = generation_data()["soldier_groups"][0]
    if missing:
        del data[field]
    else:
        data[field] = None
    with pytest.raises(ValidationError) as error:
        SoldierGroup.model_validate(data)
    assert error.value.errors()[0]["loc"] == (field,)


@pytest.mark.parametrize("origin_type", [Castle, SoldierGroup])
@pytest.mark.parametrize("other_type", [Castle, SoldierGroup])
@pytest.mark.parametrize(
    "origin_team, other_team, expected",
    [
        (1, 1, TeamType.friendly),
        (1, 2, TeamType.enemy),
        (2, 1, TeamType.enemy),
        (1, 0, TeamType.neutral),
        (0, 0, TeamType.neutral),
        (0, 1, TeamType.enemy),
    ],
)
def test_compare_team(origin_type, other_type, origin_team, other_team, expected):
    origin = make_entity(
        origin_type, id=0, team_color=origin_team, soldiers=10, x=0, y=0
    )
    other = make_entity(other_type, id=1, team_color=other_team, soldiers=10, x=0, y=0)
    assert origin.compare_team(other) is expected


def generation_data():
    destination = {"id": 0, "team_color": 2, "soldiers": 10, "x": 3, "y": 4}
    data = {
        "id": 1,
        "castles": [
            {"id": 7, "team_color": 1, "soldiers": 10, "x": -3, "y": -4},
            destination,
        ],
        "soldier_groups": [
            {
                "id": 1,
                "team_color": 1,
                "soldiers": 5,
                "origin_castle_id": 7,
                "destination_castle_id": 0,
                "destination_castle": destination,
                "clash_radius": 0.5,
                "x": 0,
                "y": 0,
            }
        ],
    }
    return data


def test_distance_left_survives_roundtrip():
    generation = Generation.model_validate(generation_data())
    assert generation.soldier_groups[0].distance_left() == 5
    serialized = generation.model_dump_json()
    assert Generation.model_validate_json(serialized) == generation
    assert (
        Generation.model_validate_json(serialized).soldier_groups[0].distance_left()
        == 5
    )
    with pytest.raises(ValidationError, match="frozen"):
        # Intentionally exercise Pydantic's runtime immutability guard.
        # ty: ignore[invalid-assignment]
        generation.soldier_groups[0].x = 100


def test_distance_left_at_destination():
    data = generation_data()
    data["soldier_groups"][0].update(x=3, y=4)
    assert Generation.model_validate(data).soldier_groups[0].distance_left() == 0


def test_wire_state_rejects_unknown_destination():
    data = generation_data()
    data["soldier_groups"][0]["destination_castle_id"] = 99
    state = wire_state(data)
    with pytest.raises(GameProtocolError, match="destination castle"):
        state.public_generation()


def test_standalone_group_has_destination_at_construction():
    destination = Castle(id=0, team_color=2, soldiers=10, x=3, y=4)
    data = generation_data()["soldier_groups"][0]
    data["destination_castle"] = destination
    group = SoldierGroup(**data)
    assert group.destination_castle is destination
    assert group.distance_left() == 5


def test_wire_conversion_preserves_earlier_snapshots():
    first = wire_state(generation_data()).public_generation()
    data = generation_data()
    data["id"] = 2
    data["castles"][1].update(x=6, y=8)
    second = wire_state(data).public_generation()
    assert first.soldier_groups[0].distance_left() == 5
    assert second.soldier_groups[0].distance_left() == 10


def wire_state(data):
    generation_id = data.pop("id")
    for group in data["soldier_groups"]:
        del group["destination_castle"]
        group["origin_persistent_castle_id"] = group.pop("origin_castle_id")
        group["destination_persistent_castle_id"] = group.pop("destination_castle_id")
    return GenerationState.parse(
        {
            "game_id": 1,
            "lobby_id": "11111111-1111-4111-8111-111111111111",
            "generation_id": generation_id,
            "player_id": "22222222-2222-4222-8222-222222222222",
            "team_color": 1,
            "channel": "player:22222222-2222-4222-8222-222222222222",
            "submitted": False,
            "generation": data,
        }
    )


def test_wire_state_provides_utilities_on_public_generation():
    state = wire_state(generation_data())
    generation = state.public_generation()
    group = generation.soldier_groups[0]
    assert group.origin_castle_id == 7
    assert group.destination_castle_id == 0
    assert group.destination_castle is generation.castles[1]
    assert group.distance_left() == 5
    assert group.distance(generation.castles[0]) == 5
    assert group.compare_team(generation.castles[1]) is TeamType.enemy
