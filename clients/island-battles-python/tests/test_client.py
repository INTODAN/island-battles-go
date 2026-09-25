import asyncio
import json
from types import SimpleNamespace
from uuid import UUID

import httpx
import pytest
from island_battles_client import (
    GameAuthenticationError,
    GameClientClosedError,
    GameError,
    GameProtocolError,
    IslandBattlesClient,
    Player,
)
from pydantic import ValidationError

LOBBY = "00000000-0000-0000-0000-000000000001"
PLAYER = "00000000-0000-0000-0000-000000000002"
OTHER = "00000000-0000-0000-0000-000000000003"


def state(generation=1, *, player=PLAYER, lobby=LOBBY, team_color=1, submitted=False):
    return {
        "game_id": 7,
        "lobby_id": lobby,
        "generation_id": generation,
        "player_id": player,
        "team_color": team_color,
        "channel": f"player:{player}",
        "submitted": submitted,
        "generation": {
            "castles": [
                {"id": 0, "team_color": 1, "soldiers": 30, "x": -2, "y": 0},
                {"id": 1, "team_color": 2, "soldiers": 30, "x": 2, "y": 0},
            ],
            "soldier_groups": [],
        },
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
        await self.events.on_publication(
            SimpleNamespace(pub=SimpleNamespace(data=payload))
        )

    async def reconnect(self):
        await self.events.on_subscribed(SimpleNamespace(recovered=False))


class FakeSDK:
    def __init__(self, address, **options):
        self.address, self.options = address, options
        self.sub = None
        self.disconnected = False

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
        self.team_color = 1
        self.calls = []
        self.gets = 0
        self.post_error = None
        self.get_error = None
        self.transport_failures = 0
        self.get_hook = None
        self.advance_on_complete = False
        self.submitted = set()

    async def __call__(self, request):
        self.calls.append(request)
        assert request.headers["Authorization"] == "Bearer guest-secret"
        if request.url.path == "/lobby/v1/connection-token":
            return httpx.Response(
                200, json={"token": "connection-token", "websocket_url": "ws://test/ws"}
            )
        if request.method == "GET":
            self.gets += 1
            if self.get_hook is not None:
                response = await self.get_hook(request)
                if response is not None:
                    return response
            if self.get_error:
                return httpx.Response(self.get_error, json={"detail": "read forbidden"})
            return httpx.Response(
                200,
                json=state(
                    self.generation,
                    team_color=self.team_color,
                    submitted=self.generation in self.submitted,
                ),
            )
        assert request.method == "POST" and request.url.path == "/generation/commands"
        if self.transport_failures:
            self.transport_failures -= 1
            raise httpx.ConnectError("temporary loss")
        if self.post_error:
            return httpx.Response(self.post_error, json={"detail": "command forbidden"})
        body = json.loads(request.content)
        if body["complete"]:
            self.submitted.add(body["generation_id"])
            if self.advance_on_complete:
                self.generation += 1
        return httpx.Response(
            200,
            json=state(
                self.generation,
                team_color=self.team_color,
                submitted=self.generation in self.submitted,
            ),
        )

    @property
    def posts(self):
        return [
            json.loads(call.content)
            for call in self.calls
            if call.url.path == "/generation/commands"
        ]


class RecordingPlayer(Player):
    def __init__(self, team_color):
        super().__init__(team_color)
        self.seen = asyncio.Queue()
        self.receivers = asyncio.Queue()

    async def on_generation(self, generation, commands):
        await self.seen.put(generation)
        await self.receivers.put(commands)


class WaitingPlayer(RecordingPlayer):
    def __init__(self, team_color):
        super().__init__(team_color)
        self.release = asyncio.Event()

    async def on_generation(self, generation, commands):
        await super().on_generation(generation, commands)
        await self.release.wait()


def client(server, player_factory=RecordingPlayer, **options):
    transport = httpx.MockTransport(server)
    return IslandBattlesClient(
        "http://test",
        "guest-secret",
        LOBBY,
        player_factory=player_factory,
        http_client=httpx.AsyncClient(transport=transport),
        sdk_factory=FakeSDK,
        **options,
    )


async def until(predicate, timeout=1):
    async def poll():
        while not predicate():
            await asyncio.sleep(0.001)

    await asyncio.wait_for(poll(), timeout)


async def seen(game):
    return await asyncio.wait_for(game.player.seen.get(), 1)


async def test_orders_then_automatic_completion_empty_turn_and_dedup():
    server = Server()
    server.advance_on_complete = True

    class OrderingPlayer(RecordingPlayer):
        async def on_generation(self, generation, commands):
            await super().on_generation(generation, commands)
            if generation.id == 1:
                assert await commands.send_soldier_group(0, 1, 5) is None
            else:
                server.advance_on_complete = False

    async with client(server, OrderingPlayer) as game:
        assert game.generation.castles[0].id == 0
        assert (await seen(game)).id == 1
        assert (await seen(game)).id == 2
        await until(lambda: len(server.posts) == 3)
        assert [
            (batch["generation_id"], batch["complete"]) for batch in server.posts
        ] == [
            (1, False),
            (1, True),
            (2, True),
        ]
        assert (
            server.posts[0]["commands"][0]["parameters"]["origin_logical_castle_id"]
            == 0
        )
        assert server.posts[1]["commands"] == server.posts[2]["commands"] == []
        event = {"schema_version": 1, "type": "game.next_generation", "state": state(2)}
        await game._sdk.sub.publish(event)
        await game._sdk.sub.publish(event)
        await asyncio.sleep(0.01)
        assert game.player.seen.empty()
        assert len(server.posts) == 3
    assert game._sdk.disconnected
    assert game._sdk.sub.unsubscribed
    assert game._http.is_closed
    with pytest.raises(GameClientClosedError):
        await game.refresh()


async def test_foreign_pushes_ignored_lost_push_poll_and_reconnect():
    server = Server()
    async with client(server, refresh_interval=0.02) as game:
        assert (await seen(game)).id == 1
        baseline = server.gets
        for event in (
            {
                "schema_version": 1,
                "type": "game.next_generation",
                "state": state(9, player=OTHER),
            },
            {
                "schema_version": 1,
                "type": "game.next_generation",
                "state": state(9, lobby=OTHER),
            },
            {"schema_version": 2, "type": "game.next_generation", "state": state(9)},
            {"schema_version": 1, "type": "other", "state": state(9)},
        ):
            await game._sdk.sub.publish(event)
        await asyncio.sleep(0.005)
        assert game.player.seen.empty()
        server.generation = 2  # Missing push: periodic authorized GET recovers it.
        assert (await seen(game)).id == 2
        assert server.gets > baseline
        server.generation = 3
        await game._sdk.sub.reconnect()
        assert (await seen(game)).id == 3


async def test_http_error_detail_and_auth_failure_propagation():
    server = Server()
    async with client(server, WaitingPlayer) as game:
        await seen(game)
        receiver = await game.player.receivers.get()
        server.post_error = 403
        with pytest.raises(GameError) as caught:
            await receiver.send_soldier_group(0, 1, 1)
        assert caught.value.status == 403
        assert caught.value.detail == "command forbidden"
        server.get_error = 401
        game._wakeup.set()
        with pytest.raises(GameAuthenticationError) as auth:
            await asyncio.wait_for(game.wait(), 1)
        assert auth.value.status == 401
        assert auth.value.detail == "read forbidden"


async def test_temporary_http_outage_recovers_after_reconnect():
    server = Server()
    async with client(server) as game:
        assert (await seen(game)).id == 1
        server.get_error = 503
        await game._sdk.sub.reconnect()
        await asyncio.sleep(0.03)
        assert game._failure is None
        server.get_error = None
        server.generation = 2
        await game._sdk.sub.reconnect()
        assert (await seen(game)).id == 2


async def test_cancelled_wait_and_close_cancel_callback_without_completion():
    server = Server()
    game = client(server, WaitingPlayer)
    await game.start()
    await seen(game)
    waiter = asyncio.create_task(game.wait())
    await asyncio.sleep(0)
    waiter.cancel()
    with pytest.raises(asyncio.CancelledError):
        await waiter
    await asyncio.gather(game.close(), game.close())
    assert game._http.is_closed
    assert game._sdk.disconnected
    assert all(task.done() for task in game._tasks)
    assert server.posts == []


async def test_generated_uuid_survives_retry_but_new_calls_send_distinct_groups():
    server = Server()
    async with client(server, WaitingPlayer) as game:
        await seen(game)
        receiver = await game.player.receivers.get()
        server.transport_failures = 1
        await receiver.send_soldier_group(0, 1, 3)
        assert len(server.posts) == 2
        first, retry = server.posts
        assert first == retry
        assert UUID(first["request_id"]).version == 4
        assert first["commands"] == [
            {
                "command_type": "send_soldier_group",
                "parameters": {
                    "origin_logical_castle_id": 0,
                    "destination_logical_castle_id": 1,
                    "soldier_amount": 3,
                },
            }
        ]
        assert first["complete"] is False
        await receiver.send_soldier_group(0, 1, 3)
        assert len(server.posts) == 3
        second = server.posts[-1]
        assert second["request_id"] != first["request_id"]
        assert second["commands"] == first["commands"]
        assert second["generation_id"] == first["generation_id"] == 1


async def test_late_same_generation_get_cannot_undo_submitted_state():
    server = Server()
    async with client(server) as game:
        await until(lambda: game._state.submitted)
        entered, release = asyncio.Event(), asyncio.Event()

        async def delayed_get(_):
            server.get_hook = None
            entered.set()
            await release.wait()
            return httpx.Response(200, json=state(1, submitted=False))

        server.get_hook = delayed_get
        fetch = asyncio.create_task(game.refresh())
        await entered.wait()
        release.set()
        await fetch
        assert game._state.submitted is True


async def test_receiver_keeps_original_generation_during_reconcile():
    started, release = asyncio.Event(), asyncio.Event()

    class BlockingPlayer(RecordingPlayer):
        async def on_generation(self, generation, commands):
            await super().on_generation(generation, commands)
            if generation.id == 1:
                started.set()
                await release.wait()
                await commands.send_soldier_group(0, 1, 1)
            else:
                await asyncio.Future()  # Keep the next turn open for inspection.

    server = Server()
    async with client(server, BlockingPlayer) as game:
        await started.wait()
        server.generation = 2
        await game.refresh()
        release.set()
        assert (await seen(game)).id == 1
        assert (await seen(game)).id == 2
        await until(lambda: len(server.posts) == 1)
        await asyncio.sleep(0.01)
        # Returning from the superseded callback must not complete generation 2.
        assert len(server.posts) == 1
        assert server.posts[0]["generation_id"] == 1
        assert server.posts[0]["complete"] is False
        assert game.generation.id == 2


async def test_initial_auth_failure_does_not_construct_player():
    server = Server()
    constructed = []

    def factory(color):
        constructed.append(color)
        return RecordingPlayer(color)

    game = client(server, factory)
    assert game.player is None and game.generation is None
    server.get_error = 401
    with pytest.raises(GameAuthenticationError):
        await game.start()
    assert constructed == []
    assert game._http.is_closed
    assert game._sdk.disconnected


async def test_default_no_polling_and_push_uses_authorized_state():
    server = Server()
    async with client(server) as game:
        assert (await seen(game)).id == 1
        await asyncio.sleep(0.01)
        initial_gets = server.gets
        server.generation = 2
        await game._sdk.sub.publish(
            {
                "schema_version": 1,
                "type": "game.next_generation",
                "state": state(2),
            }
        )
        assert (await seen(game)).id == 2
        await asyncio.sleep(0.03)
        assert server.gets == initial_gets
        assert game._refresh_interval is None
        assert game.generation.id == 2


async def test_empty_callback_completes_with_same_uuid_after_transport_retry():
    server = Server()
    server.transport_failures = 1
    async with client(server) as game:
        await until(lambda: game._state.submitted)
        assert len(server.posts) == 2
        assert server.posts[0] == server.posts[1]
        assert server.posts[0]["complete"] is True
        assert server.posts[0]["commands"] == []
        assert server.posts[0]["generation_id"] == 1


async def test_failed_callback_does_not_complete_partial_commands():
    class FailingPlayer(Player):
        async def on_generation(self, generation, commands):
            self.receiver = commands
            await commands.send_soldier_group(0, 1, 1)
            raise RuntimeError("strategy failed")

    server = Server()
    async with client(server, FailingPlayer) as game:
        with pytest.raises(RuntimeError, match="strategy failed"):
            await asyncio.wait_for(game.wait(), 1)
        assert len(server.posts) == 1
        assert server.posts[0]["complete"] is False
        assert not game._state.submitted
        with pytest.raises(GameError, match="closed|expired|ended"):
            await game.player.receiver.send_soldier_group(0, 1, 1)


async def test_automatic_completion_error_surfaces_from_wait():
    server = Server()
    server.post_error = 403
    async with client(server) as game:
        with pytest.raises(GameError, match="command forbidden") as caught:
            await asyncio.wait_for(game.wait(), 1)
        assert caught.value.status == 403
        assert len(server.posts) == 1
        assert server.posts[0]["complete"] is True


async def test_already_submitted_generation_is_not_replayed():
    server = Server()
    server.submitted.add(1)
    async with client(server) as game:
        await asyncio.sleep(0.01)
        assert game.player.seen.empty()
        assert server.posts == []


async def test_receiver_expires_when_callback_returns():
    server = Server()
    async with client(server) as game:
        await seen(game)
        receiver = await game.player.receivers.get()
        await until(lambda: game._state.submitted)
        with pytest.raises(GameError, match="closed|expired|ended"):
            await receiver.send_soldier_group(0, 1, 1)
        assert len(server.posts) == 1


async def test_factory_called_once_and_team_identity_cannot_change():
    server = Server()
    colors = []

    def factory(color):
        colors.append(color)
        return RecordingPlayer(color)

    async with client(server, factory) as game:
        first = game.player
        assert colors == [1]
        assert first.team_color == 1
        server.generation = 2
        await game.refresh()
        assert game.player is first and colors == [1]
        server.team_color = 2
        with pytest.raises(GameProtocolError):
            await game.refresh()
        assert game.player is first and first.team_color == 1


async def test_factory_must_return_player_for_authenticated_team():
    server = Server()

    class WrongTeamPlayer(RecordingPlayer):
        def __init__(self, team_color):
            super().__init__(team_color + 1)

    game = client(server, WrongTeamPlayer)
    with pytest.raises(GameProtocolError):
        await game.start()
    assert game._http.is_closed


async def test_public_generation_is_lean_frozen_and_nested_values_are_tuples():
    server = Server()

    async def with_group(_):
        payload = state()
        payload["generation"]["soldier_groups"] = [
            {
                "id": 0,
                "team_color": 1,
                "soldiers": 5,
                "origin_logical_castle_id": 0,
                "destination_logical_castle_id": 1,
                "clash_radius": 0.5,
                "x": -1.0,
                "y": 0.0,
            }
        ]
        return httpx.Response(200, json=payload)

    server.get_hook = with_group
    async with client(server, WaitingPlayer) as game:
        generation = await seen(game)
        assert game.generation == generation
        assert set(type(generation).model_fields) == {
            "id",
            "castles",
            "soldier_groups",
            "clashes",
        }
        assert isinstance(generation.castles, tuple)
        assert isinstance(generation.soldier_groups, tuple)
        assert generation.soldier_groups[0].id == 0
        assert not hasattr(generation, "game_id")
        assert not hasattr(generation, "player_id")
        assert not hasattr(generation, "submitted")
        with pytest.raises(ValidationError):
            generation.id = 99
        with pytest.raises(ValidationError):
            generation.castles[0].soldiers = 999
        with pytest.raises(ValidationError):
            generation.soldier_groups[0].soldiers = 999
        with pytest.raises(TypeError):
            generation.castles[0] = generation.castles[1]


@pytest.mark.parametrize(
    "field,bad",
    [
        ("id", True),
        ("id", "0"),
        ("soldiers", True),
        ("soldiers", "30"),
    ],
)
async def test_malformed_wire_castle_rejected(field, bad):
    server = Server()

    async def malformed(_):
        payload = state()
        payload["generation"]["castles"][0][field] = bad
        return httpx.Response(200, json=payload)

    server.get_hook = malformed
    game = client(server)
    with pytest.raises(GameProtocolError):
        await game.start()
    assert game._http.is_closed
    assert game.player is None


@pytest.mark.parametrize(
    "field,bad,error",
    [
        ("origin_logical_castle_id", True, TypeError),
        ("destination_logical_castle_id", "1", TypeError),
        ("soldier_amount", False, TypeError),
        ("soldier_amount", "5", TypeError),
        ("origin_logical_castle_id", -1, ValueError),
        ("destination_logical_castle_id", -1, ValueError),
        ("soldier_amount", 0, ValueError),
        ("soldier_amount", -1, ValueError),
    ],
)
async def test_send_rejects_invalid_parameters_before_sending(field, bad, error):
    values = {
        "origin_logical_castle_id": 0,
        "destination_logical_castle_id": 1,
        "soldier_amount": 5,
    }
    values[field] = bad
    server = Server()
    async with client(server, WaitingPlayer) as game:
        await seen(game)
        receiver = await game.player.receivers.get()
        with pytest.raises(error):
            await receiver.send_soldier_group(**values)
        assert server.posts == []


def test_generation_preserves_clash_history_and_accepts_older_states():
    from island_battles_client import Clash, ClashParticipant
    from island_battles_client._protocol import GenerationState

    payload = state()
    assert GenerationState.parse(payload).public_generation().clashes == ()
    payload["generation"]["clashes"] = [
        {
            "id": 1,
            "time": 0.75,
            "participants": [
                {
                    "group_id": 0,
                    "team_color": 1,
                    "x": -0.125,
                    "y": 0,
                    "clash_radius": 0.25,
                    "soldiers_before": 20,
                    "soldiers_after": 10,
                },
                {
                    "group_id": 1,
                    "team_color": 2,
                    "x": 0.125,
                    "y": 0,
                    "clash_radius": 0.25,
                    "soldiers_before": 10,
                    "soldiers_after": 0,
                },
            ],
        }
    ]
    generation = GenerationState.parse(payload).public_generation()
    clash = generation.clashes[0]
    assert isinstance(clash, Clash)
    assert isinstance(clash.participants[0], ClashParticipant)
    assert clash.participants[0].soldiers_after == 10
    assert clash.participants[1].soldiers_after == 0
    with pytest.raises(ValidationError):
        clash.participants[0].soldiers_after = 100
    payload["generation"]["clashes"][0]["time"] = 2
    with pytest.raises(GameProtocolError):
        GenerationState.parse(payload)
