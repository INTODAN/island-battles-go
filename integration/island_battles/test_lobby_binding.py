"""Island Battles integration checks layered on the shared lobby acceptance suite."""
import signal
from uuid import uuid4

import httpx
import pytest
from lobby_client import Scope

from integration.test_live import BASE, create, control_backend, guests, sql

pytestmark = pytest.mark.asyncio


async def started_lobby(guests, game_key):
    alice, bob = await guests("Alice"), await guests("Bob")
    room = await create(alice, game_key)
    scope = Scope.from_snapshot(room)
    await bob.join_lobby(game_key=scope.game_key, join_code=room.lobby.join_code)
    await alice.select_team(scope, room.teams[0].id)
    await bob.select_team(scope, room.teams[1].id)
    await alice.set_ready(scope, True)
    await bob.set_ready(scope, True)
    await alice.start(scope)
    return alice, bob, scope


async def test_start_initializes_one_generation_and_binding_is_idempotent(guests):
    alice, bob, scope = await started_lobby(guests, "island-battles")
    assert await sql(f"SELECT count(*) FROM games WHERE lobby_id='{scope.lobby_id}' AND replay IS NOT NULL") == "1"
    async with httpx.AsyncClient(base_url=BASE, headers={"Authorization": f"Bearer {alice.credential}"}) as adapter:
        first = await adapter.post("/game/from-lobby", json={"game_key": scope.game_key, "lobby_id": scope.lobby_id})
        replay = await adapter.post("/game/from-lobby", json={"game_key": scope.game_key, "lobby_id": scope.lobby_id})
    assert first.status_code == replay.status_code == 200
    assert first.json() == replay.json()
    assert await sql(f"SELECT count(*) FROM games WHERE lobby_id='{scope.lobby_id}'") == "1"
    game_id = first.json()["game_id"]
    assert await sql(f"SELECT count(*) FROM generations WHERE game_id={game_id}") == "1"
    result_path = f"/game/from-lobby/{scope.lobby_id}/replay"
    async with httpx.AsyncClient(base_url=BASE) as client:
        host_result = await client.get(result_path, headers={"Authorization": f"Bearer {alice.credential}"})
        guest_result = await client.get(result_path, headers={"Authorization": f"Bearer {bob.credential}"})
        assert host_result.status_code == guest_result.status_code == 200
        assert host_result.text == guest_result.text
        assert "Alice" in host_result.text and "Bob" in host_result.text
        assert "battlefield" in host_result.text
        assert (await client.get(result_path)).status_code == 401
        outsider = await guests("Outsider")
        assert (await client.get(result_path, headers={"Authorization": f"Bearer {outsider.credential}"})).status_code == 404
        await control_backend(signal.SIGUSR1)
        assert (await client.get(result_path, headers={"Authorization": f"Bearer {bob.credential}"})).text == guest_result.text


async def delete_lobby(lobby_id):
    """Delete a lobby's rows, keeping guest sessions, as lobby retention will."""
    dependents = ("lobby_start_roster", "lobby_leases", "lobby_memberships", "lobby_teams")
    # Locking the room first makes concurrent heartbeats wait, then find no lobby.
    await sql(
        f"SELECT 1 FROM lobby_rooms WHERE id='{lobby_id}' FOR UPDATE; "
        + "".join(f"DELETE FROM {table} WHERE lobby_id='{lobby_id}'; " for table in dependents)
        + f"DELETE FROM lobby_rooms WHERE id='{lobby_id}'"
    )


async def test_roster_keeps_game_access_after_lobby_is_deleted(guests):
    alice, bob, scope = await started_lobby(guests, "island-battles")
    await delete_lobby(scope.lobby_id)
    state_path = f"/game/from-lobby/{scope.lobby_id}/generation"
    replay_path = f"/game/from-lobby/{scope.lobby_id}/replay"
    outsider = await guests("Outsider")

    def bearer(player):
        return {"Authorization": f"Bearer {player.credential}"}

    async with httpx.AsyncClient(base_url=BASE) as http:
        lobby = await http.get(f"/lobby/v1/games/{scope.game_key}/lobbies/{scope.lobby_id}", headers=bearer(alice))
        assert lobby.status_code == 404, lobby.text
        response = await http.get(state_path, headers=bearer(alice))
        assert response.status_code == 200, response.text
        initial = response.json()
        castles = initial["generation"]["castles"]
        origin = next(castle["id"] for castle in castles if castle["team_color"] == initial["team_color"])
        target = next(castle["id"] for castle in castles if castle["team_color"] != initial["team_color"])

        async def complete_turn(player, commands):
            batch = {"game_id": initial["game_id"], "generation_id": initial["generation_id"], "request_id": str(uuid4()), "commands": commands}
            return await http.post("/generation/commands", headers=bearer(player), json=batch)

        for path in (state_path, replay_path):
            assert (await http.get(path, headers=bearer(outsider))).status_code == 404
        assert (await complete_turn(outsider, [])).status_code == 404
        order = {"command_type": "send_soldier_group", "parameters": {"origin_castle_id": origin, "destination_castle_id": target, "soldier_amount": 5}}
        response = await complete_turn(alice, [order])
        assert response.status_code == 200, response.text
        response = await complete_turn(bob, [])
        assert response.status_code == 200, response.text
        advanced = response.json()
        assert advanced["generation_id"] > initial["generation_id"]
        assert [group["soldiers"] for group in advanced["generation"]["soldier_groups"]] == [5]
        for player in (alice, bob):
            replay = await http.get(replay_path, headers=bearer(player))
            assert replay.status_code == 200, replay.text
            assert "Alice" in replay.text and "Bob" in replay.text
    assert await sql(f"SELECT jsonb_array_length(replay->'frames') FROM games WHERE lobby_id='{scope.lobby_id}'") == "2"


async def test_other_game_never_creates_island_state(guests):
    _, _, scope = await started_lobby(guests, "quiz-demo")
    assert await sql(f"SELECT count(*) FROM games WHERE lobby_id='{scope.lobby_id}'") == "0"


async def test_island_schema_survives_restart():
    tables = await sql("SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
    lobby_tables = {table for table in tables.splitlines() if table.startswith("lobby_")}
    assert set(tables.splitlines()) == lobby_tables | {
        "games", "maps", "castles", "castle_templates", "map_templates", "map_template_schema_migrations",
        "generations", "soldier_groups", "commands", "island_game_team_bindings",
        "frozen_game_players", "generation_inputs", "generation_requests", "generation_notifications",
    }
    columns = await sql("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='games' ORDER BY ordinal_position")
    assert set(columns.splitlines()) == {"id", "created_at", "updated_at", "deleted_at", "lobby_id", "replay", "next_soldier_group_id"}
    await control_backend(signal.SIGUSR1)
    assert await sql("SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename") == tables
