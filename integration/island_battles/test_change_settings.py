"""The host changes a waiting lobby's map and turn timeout, then plays on the new map."""
import asyncio
import os
from uuid import uuid4

import httpx
import pytest
from lobby_api_client.models import TeamDefinition
from lobby_client import LobbyError, Scope

from integration.test_live import BASE, create, guests, latest, sql
from test_lobby_binding import LOCALHOST_BOT, choose_bot, started_map, strait_map

pytestmark = pytest.mark.asyncio


def delta_map(name):
    """A three-team map: one castle per team and a neutral one."""
    return {
        "map_name": name, "player_amount": 3, "plane_size_x": 10, "plane_size_y": 6,
        "neutral_starting_soldier_amount": 5,
        "castles": [
            {"team_color": 1, "x": -4, "y": -2, "starting_soldier_amount": 30},
            {"team_color": 0, "x": 0, "y": 0},
            {"team_color": 2, "x": 4, "y": -2, "starting_soldier_amount": 30},
            {"team_color": 3, "x": 0, "y": 2, "starting_soldier_amount": 30},
        ],
    }


async def default_map(client, map_file):
    created = await client.post("/map/create", json=map_file,
                                headers={"X-Admin-API-Key": os.environ["ISLAND_BATTLES_ADMIN_API_KEY"]})
    assert created.status_code == 200, created.text
    return created.json()["map_template_id"]


def teams(*names):
    return [TeamDefinition(name=name) for name in names]


async def test_the_host_changes_the_map_and_then_plays_the_first_turn_on_it(guests):
    strait_name, delta_name = f"Strait {uuid4()}", f"Delta {uuid4()}"
    async with httpx.AsyncClient(base_url=BASE) as client:
        strait = await default_map(client, strait_map(strait_name))
        delta = await default_map(client, delta_map(delta_name))

    alice, bob = await guests("Alice"), await guests("Bob")
    room = await create(alice, "island-battles", {"map_template_id": strait})
    scope = Scope.from_snapshot(room)
    await bob.join_lobby(game_key=scope.game_key, join_code=room.lobby.join_code)
    for player, team in ((alice, room.teams[0]), (bob, room.teams[1])):
        await player.select_team(scope, team.id)
        await choose_bot(player, scope.lobby_id, LOCALHOST_BOT)
        await player.set_ready(scope, True)

    # Only the host changes the settings, and the map must fit the teams sent.
    with pytest.raises(LobbyError) as refused:
        await bob.change_settings(scope, teams=teams("Blue", "Red", "Green"), game_settings={"map_template_id": delta})
    assert (refused.value.code, refused.value.status) == ("not_host", 403)
    with pytest.raises(LobbyError) as refused:
        await alice.change_settings(scope, teams=teams("Blue", "Red"), game_settings={"map_template_id": delta})
    assert (refused.value.code, refused.value.status) == ("invalid_game_settings", 422)
    assert "player_amount 3" in refused.value.detail

    # Three teams: the first two keep their IDs and players, a third is added,
    # and Bob sees the change pushed, with his ready cleared.
    changed = await alice.change_settings(
        scope, teams=teams("Blue", "Red", "Green"),
        game_settings={"map_template_id": delta, "turn_timeout_seconds": 120})
    assert [str(team.id) for team in changed.teams[:2]] == [str(team.id) for team in room.teams]
    assert (len(changed.teams), changed.lobby.capacity) == (3, 3)
    seen = await latest(bob, scope, changed.lobby.version)
    assert seen.lobby.game_settings.to_dict() == {"map_template_id": delta, "turn_timeout_seconds": 120}
    assert [(member.display_name, member.ready) for member in seen.members] == [("Alice", False), ("Bob", False)]

    # Back to two teams and on to three again: the change can be made as
    # often as the host likes while the lobby waits.
    await alice.change_settings(scope, teams=teams("Blue", "Red"), game_settings={"map_template_id": strait})
    changed = await alice.change_settings(
        scope, teams=teams("Blue", "Red", "Green"),
        game_settings={"map_template_id": delta, "turn_timeout_seconds": 120})
    green = changed.teams[2]
    await alice.add_seat(scope, str(green.id), {"bot_level": 1})
    for player in (alice, bob):
        await player.set_ready(scope, True)
    started = await alice.start(scope)
    assert len(started.start_roster) == 3

    # The game starts on the new map with the new timeout, and plays its first turn.
    assert (await started_map(scope.lobby_id)).startswith(f"{delta_name}:")
    assert await sql(f"SELECT turn_timeout_seconds FROM games WHERE lobby_id='{scope.lobby_id}'") == "120"
    async with httpx.AsyncClient(base_url=BASE) as http:
        async def state(player):
            response = await http.get(f"/game/from-lobby/{scope.lobby_id}/generation",
                                      headers={"Authorization": f"Bearer {player.credential}"})
            assert response.status_code == 200, response.text
            return response.json()

        first = await state(alice)
        assert len({castle["team_color"] for castle in first["generation"]["castles"]} - {0}) == 3
        for player in (alice, bob):
            current = await state(player)
            batch = {"game_id": current["game_id"], "generation_id": current["generation_id"], "request_id": str(uuid4()), "commands": [], "complete": True}
            response = await http.post("/generation/commands", json=batch,
                                       headers={"Authorization": f"Bearer {player.credential}"})
            assert response.status_code == 200, response.text
        # The bot seat on Green plays its own turn, so the generation advances.
        async with asyncio.timeout(20):
            while (await state(alice))["generation_id"] == first["generation_id"]:
                await asyncio.sleep(0.2)

    # Once started, the settings are fixed.
    with pytest.raises(LobbyError) as late:
        await alice.change_settings(scope, teams=teams("Blue", "Red"), game_settings={"map_template_id": strait})
    assert late.value.code == "phase_conflict"
