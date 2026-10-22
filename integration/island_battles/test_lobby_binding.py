"""Island Battles integration checks layered on the shared lobby acceptance suite."""
import signal

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


async def test_other_game_never_creates_island_state(guests):
    _, _, scope = await started_lobby(guests, "quiz-demo")
    assert await sql(f"SELECT count(*) FROM games WHERE lobby_id='{scope.lobby_id}'") == "0"


async def test_island_schema_survives_restart():
    tables = await sql("SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
    lobby_tables = {table for table in tables.splitlines() if table.startswith("lobby_")}
    assert set(tables.splitlines()) == lobby_tables | {
        "games", "maps", "castles", "castle_templates", "map_templates",
        "generations", "soldier_groups", "commands", "island_game_team_bindings",
        "frozen_game_players", "generation_inputs", "generation_requests", "generation_notifications",
    }
    columns = await sql("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='games' ORDER BY ordinal_position")
    assert set(columns.splitlines()) == {"id", "created_at", "updated_at", "deleted_at", "lobby_id", "replay", "next_soldier_group_id"}
    await control_backend(signal.SIGUSR1)
    assert await sql("SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename") == tables
