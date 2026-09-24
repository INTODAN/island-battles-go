import asyncio
import json
from types import SimpleNamespace
from uuid import uuid4

import httpx
import pytest

from island_battles_client import (
    GameAuthenticationError, GameClientClosedError, GameError, IslandBattlesClient,
    SoldierGroupOrder,
)


LOBBY = "00000000-0000-0000-0000-000000000001"
PLAYER = "00000000-0000-0000-0000-000000000002"
OTHER = "00000000-0000-0000-0000-000000000003"


def state(generation=1, *, player=PLAYER, lobby=LOBBY, submitted=False):
    return {
        "game_id": 7, "lobby_id": lobby, "generation_id": generation,
        "player_id": player, "team_color": 1, "channel": f"player:{player}",
        "submitted": submitted,
        "generation": {"castles": [
            {"id": 0, "team_color": 1, "soldiers": 30, "x": -2, "y": 0},
            {"id": 1, "team_color": 2, "soldiers": 30, "x": 2, "y": 0},
        ], "soldier_groups": []},
    }


class FakeSubscription:
    def __init__(self, channel, events):
        self.channel, self.events = channel, events
        self.ack = asyncio.Event()
        self.unsubscribed = False

    async def subscribe(self):
        self.ack.set()
        await self.events.on_subscribed(None)

    async def ready(self):
        await self.ack.wait()

    async def unsubscribe(self):
        self.unsubscribed = True
        self.ack.clear()

    async def publish(self, payload):
        await self.events.on_publication(SimpleNamespace(pub=SimpleNamespace(data=payload)))

    async def reconnect(self):
        await self.events.on_subscribed(SimpleNamespace(recovered=False))


class FakeSDK:
    instances = []

    def __init__(self, address, **options):
        self.address, self.options = address, options
        self.sub = None
        self.disconnected = False
        self.instances.append(self)

    async def connect(self):
        await self.options["events"].on_connected(None)

    def new_subscription(self, channel, **options):
        self.sub = FakeSubscription(channel, options["events"])
        return self.sub

    def remove_subscription(self, sub):
        assert sub is self.sub

    async def disconnect(self):
        self.disconnected = True


class Server:
    def __init__(self):
        self.generation = 1
        self.calls = []
        self.gets = 0
        self.post_error = None
        self.get_error = None
        self.transport_failures = 0
        self.get_hook = None
        self.advance_on_complete = True

    async def __call__(self, request):
        self.calls.append(request)
        assert request.headers["Authorization"] == "Bearer guest-secret"
        if request.url.path == "/lobby/v1/connection-token":
            return httpx.Response(200, json={"token": "connection-token", "websocket_url": "ws://test/ws"})
        if request.method == "GET":
            self.gets += 1
            if self.get_hook is not None:
                response = await self.get_hook(request)
                if response is not None:
                    return response
            if self.get_error:
                return httpx.Response(self.get_error, json={"detail": "read forbidden"})
            return httpx.Response(200, json=state(self.generation))
        assert request.method == "POST" and request.url.path == "/generation/commands"
        if self.transport_failures:
            self.transport_failures -= 1
            raise httpx.ConnectError("temporary loss")
        if self.post_error:
            return httpx.Response(self.post_error, json={"detail": "command forbidden"})
        body = json.loads(request.content)
        if body["complete"] and self.advance_on_complete:
            self.generation += 1
        return httpx.Response(200, json=state(
            self.generation, submitted=body["complete"] and not self.advance_on_complete))

    @property
    def posts(self):
        return [json.loads(call.content) for call in self.calls
                if call.url.path == "/generation/commands"]


class RecordingClient(IslandBattlesClient):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.seen = asyncio.Queue()

    async def next_generation(self, generation):
        await self.seen.put(generation.generation_id)


def client(server, cls=RecordingClient, **options):
    transport = httpx.MockTransport(server)
    return cls("http://test", "guest-secret", LOBBY,
               http_client=httpx.AsyncClient(transport=transport),
               sdk_factory=FakeSDK, **options)


async def until(predicate, timeout=1):
    async def poll():
        while not predicate():
            await asyncio.sleep(0.001)
    await asyncio.wait_for(poll(), timeout)


async def test_initial_callback_commands_empty_completion_and_dedup():
    server = Server()
    async with client(server) as game:
        assert game.state.generation.castles[0].id == 0
        assert await asyncio.wait_for(game.seen.get(), 1) == 1
        result = await game.send_soldier_groups([SoldierGroupOrder(0, 1, 5)])
        assert result.submitted is False
        assert server.posts[-1]["complete"] is False
        assert server.posts[-1]["commands"][0]["parameters"]["origin_logical_castle_id"] == 0
        await game.end_turn()
        assert server.posts[-1]["complete"] is True
        assert server.posts[-1]["commands"] == []
        assert await asyncio.wait_for(game.seen.get(), 1) == 2
        event = {"schema_version": 1, "type": "game.next_generation", "state": state(2)}
        await game._sdk.sub.publish(event)
        await game._sdk.sub.publish(event)
        await asyncio.sleep(0.01)
        assert game.seen.empty()
    assert game._sdk.disconnected
    assert game._sdk.sub.unsubscribed
    assert game._http.is_closed
    with pytest.raises(GameClientClosedError):
        await game.end_turn()


async def test_foreign_and_malformed_pushes_ignored_and_lost_push_reconciled():
    server = Server()
    async with client(server, refresh_interval=0.02) as game:
        assert await game.seen.get() == 1
        baseline = server.gets
        for event in (
            {"schema_version": 1, "type": "game.next_generation", "state": state(9, player=OTHER)},
            {"schema_version": 1, "type": "game.next_generation", "state": state(9, lobby=OTHER)},
            {"schema_version": 2, "type": "game.next_generation", "state": state(9)},
            {"schema_version": 1, "type": "other", "state": state(9)},
        ):
            await game._sdk.sub.publish(event)
        await asyncio.sleep(0.005)
        assert game.seen.empty()
        server.generation = 2  # Missing push: periodic authorized GET recovers it.
        assert await asyncio.wait_for(game.seen.get(), 1) == 2
        assert server.gets > baseline
        server.generation = 3
        await game._sdk.sub.reconnect()
        assert await asyncio.wait_for(game.seen.get(), 1) == 3


async def test_http_error_detail_and_auth_failure_propagation():
    server = Server()
    async with client(server) as game:
        await game.seen.get()
        server.post_error = 403
        with pytest.raises(GameError) as caught:
            await game.end_turn()
        assert caught.value.status == 403
        assert caught.value.detail == "command forbidden"
        server.get_error = 401
        game._wakeup.set()
        with pytest.raises(GameAuthenticationError) as auth:
            await asyncio.wait_for(game.wait(), 1)
        assert auth.value.status == 401
        assert auth.value.detail == "read forbidden"


async def test_temporary_http_outage_recovers_without_failing_callbacks():
    server = Server()
    async with client(server, refresh_interval=0.02) as game:
        assert await game.seen.get() == 1
        server.get_error = 503
        await game._sdk.sub.reconnect()
        await asyncio.sleep(0.03)
        assert game._failure is None
        server.get_error = None
        server.generation = 2
        assert await asyncio.wait_for(game.seen.get(), 1) == 2


async def test_cancelled_wait_and_concurrent_close_clean_up():
    server = Server()
    game = client(server)
    await game.start()
    waiter = asyncio.create_task(game.wait())
    await asyncio.sleep(0)
    waiter.cancel()
    with pytest.raises(asyncio.CancelledError):
        await waiter
    await asyncio.gather(game.close(), game.close())
    assert game._http.is_closed
    assert game._sdk.disconnected
    assert all(task.done() for task in game._tasks)


async def test_request_uuid_and_payload_survive_transport_retry_and_explicit_replay():
    server = Server()
    async with client(server) as game:
        await game.seen.get()
        server.transport_failures = 1
        request_id = uuid4()
        await game.send_soldier_group(0, 1, 3, request_id=request_id)
        attempts = [json.loads(call.content) for call in server.calls
                    if call.url.path == "/generation/commands"]
        assert len(attempts) == 2
        assert attempts[0] == attempts[1]
        assert attempts[0]["request_id"] == str(request_id)
        server.generation = 2
        await game.refresh()
        await game.send_soldier_group(0, 1, 3, request_id=request_id)
        assert server.posts[-1] == attempts[0]
        with pytest.raises(GameError) as conflict:
            await game.send_soldier_group(0, 1, 4, request_id=request_id)
        assert conflict.value.status == 409


async def test_late_same_generation_get_cannot_undo_submitted_state():
    server = Server()
    server.advance_on_complete = False  # Another player has not completed the barrier.
    async with client(server) as game:
        await game.seen.get()
        entered, release = asyncio.Event(), asyncio.Event()

        async def delayed_get(_):
            server.get_hook = None
            entered.set()
            await release.wait()
            return httpx.Response(200, json=state(1, submitted=False))

        server.get_hook = delayed_get
        fetch = asyncio.create_task(game.refresh())
        await entered.wait()
        await game.end_turn()
        assert game.state.submitted is True
        release.set()
        assert (await fetch).submitted is True
        assert game.state.submitted is True


async def test_callback_turn_uses_original_generation_during_reconcile():
    started = asyncio.Event()
    release = asyncio.Event()

    class BlockingClient(RecordingClient):
        async def next_generation(self, generation):
            await self.seen.put(generation.generation_id)
            if generation.generation_id == 1:
                started.set()
                await release.wait()
                await self.send_soldier_group(0, 1, 1)

    server = Server()
    async with client(server, BlockingClient) as game:
        await started.wait()
        server.generation = 2
        await game.refresh()
        release.set()
        assert await asyncio.wait_for(game.seen.get(), 1) == 1
        assert await asyncio.wait_for(game.seen.get(), 1) == 2
        assert server.posts[-1]["generation_id"] == 1


async def test_reject_send_before_sync_and_initial_auth_error_closes():
    server = Server()
    game = client(server)
    with pytest.raises(GameError, match="not synchronized"):
        await game.end_turn()
    server.get_error = 401
    with pytest.raises(GameAuthenticationError):
        await game.start()
    assert game._http.is_closed
    assert game._sdk.disconnected
