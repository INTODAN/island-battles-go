"""Real authenticated commands, generation barriers, and Python realtime callbacks."""

import asyncio
import signal
from uuid import uuid4

import httpx
import pytest
from integration.test_live import BASE, control_backend, sql
from integration.test_live import guests as guests_fixture
from island_battles_client import IslandBattlesClient, Player
from test_lobby_binding import started_lobby

guests = guests_fixture
pytestmark = pytest.mark.asyncio


class ObservingPlayer(Player):
    def __init__(self, team_color):
        super().__init__(team_color)
        self.generations = asyncio.Queue()
        self.orders = asyncio.Queue()

    async def on_generation(self, generation, commands):
        await self.generations.put(generation)
        order = await self.orders.get()
        if order is not None:
            await commands.send_soldier_group(*order)


async def observed(player):
    return await asyncio.wait_for(player.player.generations.get(), 15)


async def submitted(player):
    async def wait():
        while not player._state.submitted:
            await asyncio.sleep(0.01)

    await asyncio.wait_for(wait(), 15)


async def test_commands_wait_for_every_player_and_push_next_generation(guests):
    alice, bob, scope = await started_lobby(guests, "island-battles")
    # Default clients rely on push notifications without periodic polling.
    first = IslandBattlesClient(
        BASE, alice.credential, scope.lobby_id, player_factory=ObservingPlayer
    )
    second = IslandBattlesClient(
        BASE, bob.credential, scope.lobby_id, player_factory=ObservingPlayer
    )
    try:
        await asyncio.gather(first.start(), second.start())
        initial, other = await asyncio.gather(observed(first), observed(second))
        assert initial.id == other.id
        origin = next(
            castle
            for castle in initial.castles
            if castle.team_color == first.player.team_color
        )
        target = next(
            castle
            for castle in initial.castles
            if castle.team_color != first.player.team_color
        )
        await first.player.orders.put((origin.id, target.id, 5))
        await submitted(first)
        assert (
            await sql(
                f"SELECT count(*) FROM generations WHERE game_id={first._state.game_id}"
            )
            == "1"
        )
        assert first.player.generations.empty() and second.player.generations.empty()
        await second.player.orders.put(None)  # Returning without orders passes.
        advanced, other_advanced = await asyncio.gather(
            observed(first), observed(second)
        )
        assert advanced.id == other_advanced.id > initial.id
        assert (
            await sql(
                f"SELECT count(*) FROM generations WHERE game_id={first._state.game_id}"
            )
            == "2"
        )
        assert not first._state.submitted and not second._state.submitted
        assert any(
            group.team_color == first.player.team_color and group.soldiers == 5
            for group in advanced.soldier_groups
        )
    finally:
        await asyncio.gather(first.close(), second.close())


async def test_rejection_retry_restart_and_public_migration_bypass(guests):
    alice, bob, scope = await started_lobby(guests, "island-battles")
    path = f"/game/from-lobby/{scope.lobby_id}/generation"
    async with httpx.AsyncClient(base_url=BASE) as http:

        async def read(player):
            response = await http.get(
                path, headers={"Authorization": f"Bearer {player.credential}"}
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
        headers = {"Authorization": f"Bearer {alice.credential}"}
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
                "origin_logical_castle_id": target["id"],
                "destination_logical_castle_id": origin["id"],
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
                path, headers={"Authorization": f"Bearer {outsider.credential}"}
            )
        ).status_code == 404
        for old_path in ("/generation/migrate", "/generation/create"):
            assert (
                await http.post(
                    old_path, json={"game_id": initial["game_id"], "commands": []}
                )
            ).status_code == 404

        send["parameters"].update(
            origin_logical_castle_id=origin["id"],
            destination_logical_castle_id=target["id"],
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
            headers={"Authorization": f"Bearer {bob.credential}"},
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
            await sql(
                f"SELECT count(*) FROM generations WHERE game_id={initial['game_id']}"
            )
            == "2"
        )
