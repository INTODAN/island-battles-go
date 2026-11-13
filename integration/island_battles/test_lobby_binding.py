"""Island Battles integration checks layered on the shared lobby acceptance suite."""
import base64
import json
import os
import signal
from uuid import uuid4

import httpx
import pytest
from lobby_client import LobbyError, Scope

from integration.test_live import BASE, create, control_backend, guests, sql

pytestmark = pytest.mark.asyncio


LOCALHOST_BOT = {"kind": "localhost"}


async def game_auth(player):
    """The Authorization header for the game's routes: a player token that the
    lobby issues for island-battles. The game refuses the guest credential."""
    token = await player.issue_player_token("island-battles")
    return {"Authorization": f"Bearer {token.token}"}


def member_settings_path(lobby_id):
    return f"/lobby/v1/games/island-battles/lobbies/{lobby_id}/member-settings"


async def choose_bot(player, lobby_id, choice):
    """Choose a player's bot for an Island Battles lobby, as every member must
    before getting ready: the bot in their lobby member settings. Returns the
    lobby snapshot."""
    async with httpx.AsyncClient(base_url=BASE, headers={"Authorization": f"Bearer {player.credential}"}) as http:
        response = await http.put(member_settings_path(lobby_id), json={"member_settings": {"bot": choice}})
    assert response.status_code == 200, response.text
    return response.json()


async def started_lobby(guests, game_key, game_settings=None, bots=(LOCALHOST_BOT, LOCALHOST_BOT)):
    """A started two-player lobby. In Island Battles, Alice and Bob play with
    bots, by default their localhost bots, whose turns the tests submit."""
    alice, bob = await guests("Alice"), await guests("Bob")
    room = await create(alice, game_key, game_settings)
    scope = Scope.from_snapshot(room)
    await bob.join_lobby(game_key=scope.game_key, join_code=room.lobby.join_code)
    await alice.select_team(scope, room.teams[0].id)
    await bob.select_team(scope, room.teams[1].id)
    if game_key == "island-battles":
        for player, choice in zip((alice, bob), bots):
            await choose_bot(player, scope.lobby_id, choice)
    await alice.set_ready(scope, True)
    await bob.set_ready(scope, True)
    await alice.start(scope)
    return alice, bob, scope


async def test_start_initializes_one_generation_and_binding_is_idempotent(guests):
    alice, bob, scope = await started_lobby(guests, "island-battles")
    assert (await alice.snapshot(scope)).lobby.game_settings.to_dict() == {}
    assert await sql(f"SELECT count(*) FROM games WHERE lobby_id='{scope.lobby_id}' AND jsonb_array_length(teams) = 2 AND outcome IS NULL") == "1"
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
        bob_auth = await game_auth(bob)
        host_result = await client.get(result_path, headers=await game_auth(alice))
        guest_result = await client.get(result_path, headers=bob_auth)
        assert host_result.status_code == guest_result.status_code == 200
        assert host_result.text == guest_result.text
        assert "Alice" in host_result.text and "Bob" in host_result.text
        assert "battlefield" in host_result.text
        assert (await client.get(result_path)).status_code == 401
        # The game takes player tokens only, not the guest credential.
        assert (await client.get(result_path, headers={"Authorization": f"Bearer {bob.credential}"})).status_code == 401
        outsider = await guests("Outsider")
        assert (await client.get(result_path, headers=await game_auth(outsider))).status_code == 404
        # A token outlives the process that issued it: the game keeps no state for it.
        await control_backend(signal.SIGUSR1)
        assert (await client.get(result_path, headers=bob_auth)).text == guest_result.text


def decode_segment(segment):
    return json.loads(base64.urlsafe_b64decode(segment + "=" * (-len(segment) % 4)))


async def test_the_lobby_issues_player_tokens_for_its_games_and_publishes_their_keys(guests):
    alice = await guests("Alice")
    room = await create(alice, "island-battles")
    player_id = next(str(member.player_id) for member in room.members)
    issued = await alice.issue_player_token("island-battles")
    header, claims, _ = issued.token.split(".")
    header, claims = decode_segment(header), decode_segment(claims)
    assert header["alg"] == "EdDSA" and header["kid"]
    assert (claims["sub"], claims["aud"], claims["name"]) == (player_id, "island-battles", "Alice")
    assert 0 < claims["exp"] - claims["iat"] <= 15 * 60
    assert claims["exp"] == int(issued.expires_at.timestamp())
    with pytest.raises(LobbyError) as unknown:
        await alice.issue_player_token("chess")
    assert (unknown.value.code, unknown.value.status) == ("not_found", 404)
    async with httpx.AsyncClient(base_url=BASE) as http:
        keys = await http.get("/lobby/v1/jwks.json")
    assert keys.status_code == 200 and keys.headers["Cache-Control"] == "public, max-age=300"
    assert header["kid"] in {key["kid"] for key in keys.json()["keys"]}
    assert all((key["kty"], key["crv"], key["alg"]) == ("OKP", "Ed25519", "EdDSA") for key in keys.json()["keys"])


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

    tokens = {player.credential: await game_auth(player) for player in (alice, bob, outsider)}

    def bearer(player):
        return tokens[player.credential]

    async with httpx.AsyncClient(base_url=BASE) as http:
        lobby = await http.get(f"/lobby/v1/games/{scope.game_key}/lobbies/{scope.lobby_id}", headers={"Authorization": f"Bearer {alice.credential}"})
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
            # One frame per stored generation, derived when the page is read.
            assert replay.text.count('"castles":') == 2
    assert await sql(f"SELECT count(*) FROM generations JOIN games ON games.id = generations.game_id WHERE games.lobby_id='{scope.lobby_id}'") == "2"


async def test_other_game_never_creates_island_state(guests):
    _, _, scope = await started_lobby(guests, "quiz-demo")
    assert await sql(f"SELECT count(*) FROM games WHERE lobby_id='{scope.lobby_id}'") == "0"


async def test_game_settings_reach_the_started_snapshot(guests):
    settings = {"rounds": 3, "theme": {"name": "space", "dark": True}}
    _, bob, scope = await started_lobby(guests, "quiz-demo", settings)
    started = await bob.snapshot(scope)
    assert started.lobby.phase == "started"
    assert started.lobby.game_settings.to_dict() == settings


async def test_island_battles_rejects_settings_it_does_not_know(guests):
    alice = await guests("Alice")
    with pytest.raises(LobbyError) as rejected:
        await create(alice, "island-battles", {"map": "archipelago"})
    assert (rejected.value.code, rejected.value.status) == ("invalid_game_settings", 422)


def strait_map(name):
    return {
        "map_name": name, "player_amount": 2, "plane_size_x": 10, "plane_size_y": 6,
        "neutral_starting_soldier_amount": 5,
        "castles": [
            {"team_color": 1, "x": -4, "y": 0, "starting_soldier_amount": 40},
            {"team_color": 0, "x": 0, "y": 0},
            {"team_color": 2, "x": 4, "y": 1, "starting_soldier_amount": 40},
        ],
    }


async def started_map(lobby_id):
    return await sql(
        "SELECT m.map_name || ':' || string_agg(c.team_color || '/' || c.soldier_amount, ',' ORDER BY c.persistent_castle_id) "
        "FROM games g JOIN generations ge ON ge.game_id = g.id JOIN maps m ON m.id = ge.map_id "
        f"JOIN castles c ON c.map_id = m.id WHERE g.lobby_id = '{lobby_id}' GROUP BY m.map_name"
    )


async def test_lobby_starts_on_a_chosen_default_map(guests):
    name = f"Strait {uuid4()}"
    async with httpx.AsyncClient(base_url=BASE) as client:
        refused = await client.post("/map/create", json=strait_map(name))
        assert refused.status_code == 401, refused.text
        created = await client.post("/map/create", json=strait_map(name),
                                    headers={"X-Admin-API-Key": os.environ["ISLAND_BATTLES_ADMIN_API_KEY"]})
        assert created.status_code == 200, created.text
        template_id = created.json()["map_template_id"]
        listed = (await client.get("/map/templates")).json()["maps"]
    assert {"map_template_id": template_id, "map_name": name, "team_count": 2,
            "plane_size_x": 10, "plane_size_y": 6, "castle_count": 3} in listed
    _, bob, scope = await started_lobby(guests, "island-battles", {"map_template_id": template_id})
    assert (await bob.snapshot(scope)).lobby.game_settings.to_dict() == {"map_template_id": template_id}
    assert await started_map(scope.lobby_id) == f"{name}:1/40,0/5,2/40"


async def test_lobby_starts_on_a_loaded_map_file(guests):
    name = f"Loaded {uuid4()}"
    _, _, scope = await started_lobby(guests, "island-battles", {"map": strait_map(name)})
    assert await started_map(scope.lobby_id) == f"{name}:1/40,0/5,2/40"


async def test_lobby_without_a_map_starts_on_the_generated_map(guests):
    _, _, scope = await started_lobby(guests, "island-battles")
    assert await started_map(scope.lobby_id) == "Lobby battle:1/30,2/30"


async def test_island_battles_rejects_a_bad_map_choice(guests):
    alice = await guests("Alice")
    three_teams = strait_map("Three")
    three_teams["player_amount"] = 3
    three_teams["castles"][1]["team_color"] = 3
    outside = strait_map("Outside")
    outside["castles"][2]["x"] = 6
    for settings, detail in [
        ({"map_template_id": 2**53}, f"game_settings.map_template_id: no default map has ID {2**53}"),
        ({"map": outside}, "game_settings.map.castles[2].x: must be inside the plane, from -5 to 5"),
        ({"map": three_teams}, 'game_settings.map: map "Three" has player_amount 3'),
        ({"map": strait_map("Both"), "map_template_id": 1}, "game_settings.map: cannot be combined with map_template_id"),
    ]:
        with pytest.raises(LobbyError) as rejected:
            await create(alice, "island-battles", settings)
        assert (rejected.value.code, rejected.value.status) == ("invalid_game_settings", 422)
        assert str(rejected.value.detail).startswith(detail), rejected.value.detail


async def test_island_schema_survives_restart():
    tables = await sql("SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
    lobby_tables = {table for table in tables.splitlines() if table.startswith("lobby_")}
    # Postgres holds only state: no table holds game events waiting to be sent.
    assert set(tables.splitlines()) == lobby_tables | {
        "games", "maps", "castles", "castle_templates", "map_templates", "map_template_schema_migrations",
        "generations", "soldier_groups", "commands", "island_game_team_bindings",
        "frozen_game_players", "generation_inputs", "generation_requests", "generation_logs",
        "game_player_bot_memory", "island_bot_packages",
    }
    columns = await sql("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='games' ORDER BY ordinal_position")
    assert set(columns.splitlines()) == {"id", "created_at", "updated_at", "deleted_at", "lobby_id", "lobby_round", "teams", "outcome", "next_soldier_group_id", "turn_timeout_seconds"}
    # A lobby plays one game per round: the games are unique by lobby and
    # round, and the old index on the lobby alone is gone.
    indexes = await sql("SELECT indexname FROM pg_indexes WHERE schemaname='public' AND tablename='games' AND indexname LIKE 'idx_games_lobby%' ORDER BY indexname")
    assert indexes.splitlines() == ["idx_games_lobby_round"]
    assert "UNIQUE" in await sql("SELECT indexdef FROM pg_indexes WHERE indexname='idx_games_lobby_round'")
    await control_backend(signal.SIGUSR1)
    assert await sql("SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename") == tables
