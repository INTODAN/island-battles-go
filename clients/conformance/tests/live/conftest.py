"""Live tier: a started two-player game. The client under test plays Alice through
a fault-injecting proxy; the runner plays Bob over plain HTTP."""

import asyncio

import httpx
import pytest_asyncio
from integration.test_live import BASE, create
from integration.test_live import guests as guests_fixture
from lobby_client import Scope

from island_conformance.adapter import AdapterProcess
from island_conformance.live import Match, Seat, choose_localhost_bots
from island_conformance.proxy import FaultProxy

guests = guests_fixture


@pytest_asyncio.fixture
async def match(guests):
    alice, bob = await guests("Conformance Alice"), await guests("Conformance Bob")
    room = await create(alice, "island-battles")
    scope = Scope.from_snapshot(room)
    await bob.join_lobby(game_key=scope.game_key, join_code=room.lobby.join_code)
    await alice.select_team(scope, room.teams[0].id)
    await bob.select_team(scope, room.teams[1].id)
    await choose_localhost_bots(BASE, scope.lobby_id, alice.credential, bob.credential)
    await alice.set_ready(scope, True)
    await bob.set_ready(scope, True)
    await alice.start(scope)
    async with httpx.AsyncClient(base_url=BASE, timeout=10) as http:
        game = Match(http, scope.lobby_id, Seat(alice.credential), Seat(bob.credential))
        game.alice.state = await game.read(game.alice)
        game.bob.state = await game.read(game.bob)
        yield game


@pytest_asyncio.fixture
async def proxy():
    async with FaultProxy(BASE) as fault_proxy:
        yield fault_proxy


@pytest_asyncio.fixture
async def launch(match, proxy):
    """Start an adapter playing Alice, or another credential, through the proxy."""
    processes = []

    async def start(
        credential: str | None = None, log_file: str | None = None
    ) -> AdapterProcess:
        process = AdapterProcess()
        await process.__aenter__()
        processes.append(process)
        message = {
            "type": "start",
            "base_url": proxy.url,
            "credential": credential or match.alice.credential,
            "lobby_id": match.lobby_id,
        }
        if log_file is not None:
            message["log_file"] = log_file
        await process.send(message)
        return process

    yield start
    await asyncio.gather(*(process.close() for process in processes))
