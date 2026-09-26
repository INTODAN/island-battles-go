"""Generated Python and TypeScript transports play together over real services."""

import asyncio
import json
import os
from pathlib import Path

import pytest
from integration.test_live import BASE
from integration.test_live import guests as guests_fixture
from island_battles_client import IslandBattlesClient
from test_generation_commands import ObservingPlayer, observed
from test_lobby_binding import started_lobby

guests = guests_fixture
pytestmark = pytest.mark.asyncio
ROOT = Path(__file__).resolve().parents[2]


async def test_python_and_typescript_complete_and_receive_the_same_turn(guests):
    alice, bob, scope = await started_lobby(guests, "island-battles")
    python_player = IslandBattlesClient(
        BASE, alice.credential, scope.lobby_id, player_factory=ObservingPlayer
    )
    node = None
    try:
        await python_player.start()
        initial = await observed(python_player)
        environment = os.environ.copy()
        environment.update(
            {
                "SDK_TEST_BASE_URL": BASE,
                "SDK_TEST_CREDENTIAL": bob.credential,
                "SDK_TEST_LOBBY_ID": scope.lobby_id,
            }
        )
        node = await asyncio.create_subprocess_exec(
            "node",
            "test/live-player.mjs",
            cwd=ROOT / "clients/island-battles-typescript",
            env=environment,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        origin = next(
            castle
            for castle in initial.castles
            if castle.team_color == python_player.player.team_color
        )
        destination = next(
            castle
            for castle in initial.castles
            if castle.team_color != python_player.player.team_color
        )
        await python_player.player.orders.put((origin.id, destination.id, 5))
        advanced, (stdout, stderr) = await asyncio.wait_for(
            asyncio.gather(observed(python_player), node.communicate()), 30
        )
        assert node.returncode == 0, stderr.decode()
        result = json.loads(stdout.decode())
        assert result["first_generation_id"] == initial.id
        assert result["generation_id"] == advanced.id > initial.id
        assert result["team_color"] != python_player.player.team_color
        assert any(
            group.team_color == python_player.player.team_color and group.soldiers == 5
            for group in advanced.soldier_groups
        )
    finally:
        if node is not None and node.returncode is None:
            node.kill()
            await node.wait()
        await python_player.close()
