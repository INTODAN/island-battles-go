"""Real authenticated commands, generation barriers, and Python realtime callbacks."""

import asyncio
import signal
from uuid import uuid4

import httpx
import pytest
from integration.test_live import BASE, control_backend, game_sql
from integration.test_live import guests as guests_fixture
from island_battles_client import IslandBattlesClient, send_soldier_group, turn_result
from test_lobby_binding import GAME, game_auth, started_lobby

guests = guests_fixture
pytestmark = pytest.mark.asyncio


class ObservingBot:
    """A bot that reports each turn's generation and waits to be told its orders."""

    def __init__(self):
        self.generations = asyncio.Queue()
        self.orders = asyncio.Queue()

    async def play(self, turn):
        await self.generations.put(turn.generation)
        order = await self.orders.get()
        return turn_result([] if order is None else [send_soldier_group(*order)])


def observing_client(credential, lobby_id):
    """A client playing an ObservingBot, which it keeps as ``client.bot``."""
    bot = ObservingBot()
    client = IslandBattlesClient(credential, lobby_id, play=bot.play, game_url=GAME, lobby_url=BASE)
    client.bot = bot
    return client


async def observed(client):
    return await asyncio.wait_for(client.bot.generations.get(), 15)


async def submitted(player):
    async def wait():
        while not player._state.submitted:
            await asyncio.sleep(0.01)

    await asyncio.wait_for(wait(), 15)


async def test_commands_wait_for_every_player_and_push_next_generation(guests):
    alice, bob, scope = await started_lobby(guests, "island-battles")
    # Default clients rely on push notifications without periodic polling.
    first = observing_client(alice.credential, scope.lobby_id)
    second = observing_client(bob.credential, scope.lobby_id)
    try:
        await asyncio.gather(first.start(), second.start())
        initial, other = await asyncio.gather(observed(first), observed(second))
        assert initial.id == other.id
        origin = next(
            castle
            for castle in initial.castles
            if castle.team_color == first.team_color
        )
        target = next(
            castle
            for castle in initial.castles
            if castle.team_color != first.team_color
        )
        await first.bot.orders.put((origin.id, target.id, 5))
        await submitted(first)
        assert (
            await game_sql(
                f"SELECT count(*) FROM generations WHERE game_id={first._state.game_id}"
            )
            == "1"
        )
        assert first.bot.generations.empty() and second.bot.generations.empty()
        await second.bot.orders.put(None)  # Returning without orders passes.
        advanced, other_advanced = await asyncio.gather(
            observed(first), observed(second)
        )
        assert advanced.id == other_advanced.id > initial.id
        assert (
            await game_sql(
                f"SELECT count(*) FROM generations WHERE game_id={first._state.game_id}"
            )
            == "2"
        )
        assert not first._state.submitted and not second._state.submitted
        assert any(
            group.team_color == first.team_color and group.soldiers == 5
            for group in advanced.soldier_groups
        )
    finally:
        await asyncio.gather(first.close(), second.close())


async def test_rejection_retry_restart_and_public_migration_bypass(guests):
    alice, bob, scope = await started_lobby(guests, "island-battles")
    path = f"/game/from-lobby/{scope.lobby_id}/generation"
    async with httpx.AsyncClient(base_url=GAME) as http:

        async def read(player):
            response = await http.get(
                path, headers=await game_auth(player)
            )
            assert response.status_code == 200, response.text
            return response.json()

        initial = await read(alice)
        origin = next(
            castle
            for castle in initial["generation"]["castles"]
            if castle["team_color"] == initial["team_color"]
        )
        target = next(
            castle
            for castle in initial["generation"]["castles"]
            if castle["team_color"] != initial["team_color"]
        )
        headers = await game_auth(alice)
        batch = {
            "game_id": initial["game_id"],
            "generation_id": initial["generation_id"],
            "request_id": str(uuid4()),
            "complete": False,
            "commands": [],
        }
        for forbidden in (
            "move_soldier_groups",
            "increase_soldier_amount",
            "remove_arrived_soldier_groups",
        ):
            rejected = await http.post(
                "/generation/commands",
                headers=headers,
                json={
                    **batch,
                    "commands": [{"command_type": forbidden, "parameters": {}}],
                },
            )
            assert rejected.status_code == 403, rejected.text
        send = {
            "command_type": "send_soldier_group",
            "parameters": {
                "origin_castle_id": target["id"],
                "destination_castle_id": origin["id"],
                "soldier_amount": 5,
            },
        }
        rejected = await http.post(
            "/generation/commands", headers=headers, json={**batch, "commands": [send]}
        )
        assert rejected.status_code == 403, rejected.text
        assert (await read(alice))["submitted"] is False
        assert (await http.get(path)).status_code == 401
        outsider = await guests("Outside game")
        assert (
            await http.get(
                path, headers=await game_auth(outsider)
            )
        ).status_code == 404
        for old_path in ("/generation/migrate", "/generation/create"):
            assert (
                await http.post(
                    old_path, json={"game_id": initial["game_id"], "commands": []}
                )
            ).status_code == 404

        send["parameters"].update(
            origin_castle_id=origin["id"],
            destination_castle_id=target["id"],
        )
        batch["commands"] = [send]
        accepted = await http.post("/generation/commands", headers=headers, json=batch)
        assert accepted.status_code == 200, accepted.text
        await control_backend(signal.SIGUSR1)
        retried = await http.post("/generation/commands", headers=headers, json=batch)
        assert retried.status_code == 200, retried.text
        conflict = await http.post(
            "/generation/commands", headers=headers, json={**batch, "commands": []}
        )
        assert conflict.status_code == 409, conflict.text
        alice_done = {
            **batch,
            "request_id": str(uuid4()),
            "commands": [],
            "complete": True,
        }
        response = await http.post(
            "/generation/commands", headers=headers, json=alice_done
        )
        assert response.status_code == 200 and response.json()["submitted"], (
            response.text
        )
        bob_done = {**alice_done, "request_id": str(uuid4())}
        response = await http.post(
            "/generation/commands",
            headers=await game_auth(bob),
            json=bob_done,
        )
        assert response.status_code == 200, response.text
        advanced = response.json()
        assert advanced["generation_id"] > initial["generation_id"]
        assert len(advanced["generation"]["soldier_groups"]) == 1
        assert advanced["generation"]["soldier_groups"][0]["soldiers"] == 5
        assert (
            await http.post("/generation/commands", headers=headers, json=alice_done)
        ).status_code == 200
        stale = await http.post(
            "/generation/commands",
            headers=headers,
            json={**alice_done, "request_id": str(uuid4())},
        )
        assert stale.status_code == 409
        assert (
            await game_sql(
                f"SELECT count(*) FROM generations WHERE game_id={initial['game_id']}"
            )
            == "2"
        )


class EndingBot:
    """Passes every turn and keeps how the game ended."""

    def __init__(self):
        self.ended = None

    def play(self, turn):
        return turn_result()

    def on_game_ended(self, outcome, generation):
        self.ended = (outcome, generation)


async def test_a_player_who_never_plays_loses_at_the_turn_deadline(guests):
    alice, bob, scope = await started_lobby(
        guests, "island-battles", {"turn_timeout_seconds": 5}
    )
    # Alice's client plays her turn and then waits for Bob, who never plays.
    # The server runs no timers: her client applies the deadline a second
    # after it, and the game ends with Bob's team, Blue, losing.
    bot = EndingBot()
    client = IslandBattlesClient(
        alice.credential,
        scope.lobby_id,
        play=bot.play,
        game_url=GAME,
        lobby_url=BASE,
        on_game_ended=bot.on_game_ended,
    )
    try:
        await client.start()
        first = client._state
        assert first.turn_deadline_at is not None
        await asyncio.wait_for(client.wait(), 20)
        outcome, final = bot.ended
        assert outcome.reason == "turn_timeout"
        assert outcome.winner_team_color == client.team_color
        assert outcome.summary == (
            "Blue lost: Bob's bot didn't answer within 5 s; Red wins"
        )
        assert final.id == first.generation_id
    finally:
        await client.close()
    game_id = first.game_id
    assert (
        await game_sql(f"SELECT count(*) FROM generations WHERE game_id={game_id}") == "1"
    )
    assert (
        await game_sql(f"SELECT outcome->>'reason' FROM games WHERE id={game_id}")
        == "turn_timeout"
    )
    headers = await game_auth(bob)
    async with httpx.AsyncClient(base_url=GAME) as http:
        # Bob's late batch for the expired generation conflicts.
        late = await http.post(
            "/generation/commands",
            headers=headers,
            json={
                "game_id": game_id,
                "generation_id": first.generation_id,
                "request_id": str(uuid4()),
                "complete": True,
                "commands": [],
            },
        )
        assert late.status_code == 409, late.text
        assert late.json()["type"] == "urn:island-battles:problem:game-ended"
        # Applying the deadline again changes nothing.
        again = await http.post(
            f"/game/from-lobby/{scope.lobby_id}/deadline", headers=headers
        )
        assert again.status_code == 200, again.text
        state = again.json()
        assert state["outcome"]["summary"] == outcome.summary
        assert state["turn_deadline_at"] is None and not state["submitted"]
