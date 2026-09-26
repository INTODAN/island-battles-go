"""Language adapter for the shared game SDK scenarios; no runtime policy here."""

import asyncio
import json
import re
from copy import deepcopy
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID

import httpx
import pytest
from jsonschema import Draft202012Validator

from island_battles_client import (
    GameAuthenticationError,
    GameClientClosedError,
    GameError,
    GameProtocolError,
    GameTransportError,
    IslandBattlesClient,
    Player,
)

ROOT = Path(__file__).resolve().parents[3]
CONFORMANCE = ROOT / "clients/conformance"
FIXTURE = json.loads((CONFORMANCE / "state.json").read_text())
SCENARIOS = json.loads((CONFORMANCE / "scenarios.json").read_text())["scenarios"]


def test_shared_scenario_contract():
    document = json.loads((CONFORMANCE / "scenarios.json").read_text())
    schema = json.loads((CONFORMANCE / "schema.json").read_text())
    Draft202012Validator.check_schema(schema)
    Draft202012Validator(schema).validate(document)
    identifiers = [scenario["id"] for scenario in SCENARIOS]
    assert len(identifiers) == len(set(identifiers))
    requirements = set(
        re.findall(r"\*\*([A-Z]+-\d+)\*\*", (ROOT / "docs/sdk/behavior.md").read_text())
    )
    for scenario in SCENARIOS:
        assert set(scenario["requirements"]) <= requirements


async def drain():
    # Give scheduled tasks deterministic execution opportunities, with no
    # elapsed-time condition. All fake I/O and injected retry sleeps yield only.
    for _ in range(40):
        await asyncio.sleep(0)


class ScenarioServer:
    def __init__(self, initial):
        self.state = deepcopy(FIXTURE)
        self.state.update(initial)
        self.trace = []
        self.requests = []
        self.accepted = {}
        self.gets = 0
        self.get_status = 0
        self.post_status = 0
        self.transport_failures = 0

    async def __call__(self, request):
        assert request.headers["Authorization"] == "Bearer conformance-guest"
        if request.url.path == "/lobby/v1/connection-token":
            self.trace.append("token")
            assert request.method == "POST"
            return httpx.Response(
                200,
                json={
                    "token": "connection-token",
                    "expires_at": "2099-01-01T00:00:00Z",
                    "websocket_url": "ws://test/connection/websocket",
                },
            )
        if request.method == "GET":
            assert (
                request.url.path == f"/game/from-lobby/{FIXTURE['lobby_id']}/generation"
            )
            self.trace.append("get")
            self.gets += 1
            if self.get_status:
                return httpx.Response(self.get_status, json={"detail": "read failed"})
            return httpx.Response(200, json=deepcopy(self.state))
        assert request.method == "POST" and request.url.path == "/generation/commands"
        self.trace.append("post")
        body = json.loads(request.content)
        assert UUID(body["request_id"]).version == 4
        self.requests.append(body)
        if self.post_status:
            return httpx.Response(self.post_status, json={"detail": "command failed"})
        previous = self.accepted.setdefault(body["request_id"], body)
        assert previous == body, "a retried UUID must retain its payload"
        if body["complete"] and body["generation_id"] == self.state["generation_id"]:
            self.state["submitted"] = True
        if self.transport_failures:
            self.transport_failures -= 1
            # The request was accepted, but its response did not reach the SDK.
            raise httpx.ReadError("response lost", request=request)
        return httpx.Response(200, json=deepcopy(self.state))


class Subscription:
    def __init__(self, channel, events, trace):
        self.channel, self.events, self.trace = channel, events, trace
        self.unsubscribed = False

    async def subscribe(self):
        self.trace.extend(["subscribe", "ack"])
        await self.events.on_subscribed(None)

    async def ready(self):
        pass

    async def unsubscribe(self):
        self.unsubscribed = True

    async def publish(self, event):
        await self.events.on_publication(
            SimpleNamespace(pub=SimpleNamespace(data=event))
        )

    async def reconnect(self):
        await self.events.on_subscribing(None)
        await self.events.on_subscribed(SimpleNamespace(recovered=False))


class Realtime:
    def __init__(self, trace, address, **options):
        self.trace, self.options = trace, options
        self.subscription = None
        self.disconnected = False

    async def connect(self):
        self.trace.append("connect")
        await self.options["events"].on_connected(None)

    def new_subscription(self, channel, **options):
        assert options["recoverable"] and options["positioned"]
        assert channel == FIXTURE["channel"]
        self.subscription = Subscription(channel, options["events"], self.trace)
        return self.subscription

    def remove_subscription(self, subscription):
        assert subscription is self.subscription

    async def disconnect(self):
        self.disconnected = True


class ScriptedPlayer(Player):
    def __init__(self, team_color, adapter):
        super().__init__(team_color)
        self.adapter = adapter

    async def on_generation(self, generation, commands_receiver):
        adapter = self.adapter
        assert adapter.active_callbacks == 0, "callbacks must be serial"
        adapter.active_callbacks += 1
        adapter.callbacks.append(generation.id)
        adapter.receiver = commands_receiver
        adapter.release = asyncio.get_running_loop().create_future()
        try:
            await adapter.release
        finally:
            adapter.active_callbacks -= 1


class Adapter:
    def __init__(self, scenario):
        self.server = ScenarioServer(scenario.get("initial", {}))
        self.callbacks = []
        self.colors = []
        self.receiver = None
        self.release = None
        self.active_callbacks = 0
        self.realtime = None
        self.marked_gets = None
        self.http = httpx.AsyncClient(transport=httpx.MockTransport(self.server))

        def player_factory(color):
            self.colors.append(color)
            return ScriptedPlayer(color, self)

        def realtime_factory(address, **options):
            self.realtime = Realtime(self.server.trace, address, **options)
            return self.realtime

        async def yield_retry(_delay):
            await asyncio.sleep(0)

        self.client = IslandBattlesClient(
            "http://test",
            "conformance-guest",
            FIXTURE["lobby_id"],
            player_factory=player_factory,
            http_client=self.http,
            sdk_factory=realtime_factory,
            sleep=yield_retry,
        )

    async def perform(self, step):
        action = step["action"]
        match action:
            case "start":
                await self.client.start()
            case "server":
                self.server.state.update(deepcopy(step.get("state", {})))
                for name in ("get_status", "post_status", "transport_failures"):
                    if name in step:
                        setattr(self.server, name, step[name])
            case "send":
                assert self.receiver is not None
                await self.receiver.send_soldier_group(
                    step["origin"], step["destination"], step["amount"]
                )
            case "return":
                assert self.release is not None
                self.release.set_result(None)
            case "fail":
                assert self.release is not None
                self.release.set_exception(RuntimeError("scripted strategy failure"))
            case "wait_error":
                await asyncio.wait_for(self.client.wait(), 2)
            case "publish":
                assert self.realtime is not None
                state = deepcopy(self.server.state)
                state.update(deepcopy(step.get("state", {})))
                await self.realtime.subscription.publish(
                    {
                        "schema_version": step.get("schema_version", 1),
                        "type": step.get("type", "game.next_generation"),
                        "state": state,
                    }
                )
            case "reconnect":
                assert self.realtime is not None
                await self.realtime.subscription.reconnect()
            case "refresh":
                await self.client.refresh()
            case "expired_send":
                assert self.receiver is not None
                before = len(self.server.requests)
                with pytest.raises(GameError, match="closed|expired|ended"):
                    await self.receiver.send_soldier_group(0, 1, 1)
                assert len(self.server.requests) == before
            case "close":
                await self.client.close()
            case "mark_gets":
                self.marked_gets = self.server.gets
            case "assert":
                self.assert_observations(step)
            case _:
                raise AssertionError(f"unsupported shared action: {action}")

    def assert_observations(self, step):
        if "callbacks" in step:
            assert self.callbacks == step["callbacks"]
        if "factory_colors" in step:
            assert self.colors == step["factory_colors"]
        if "generation" in step:
            assert self.client.generation is not None
            assert self.client.generation.id == step["generation"]
        if "requests" in step:
            observed = [
                {key: value for key, value in request.items() if key != "request_id"}
                for request in self.server.requests
            ]
            assert observed == step["requests"]
        for first, second in step.get("same_requests", []):
            assert self.server.requests[first] == self.server.requests[second]
        for first, second in step.get("different_request_ids", []):
            assert (
                self.server.requests[first]["request_id"]
                != self.server.requests[second]["request_id"]
            )
        if step.get("startup_order"):
            trace = self.server.trace
            assert trace.index("token") < trace.index("connect") < trace.index("get")
            acknowledgment = trace.index("ack")
            assert trace.index("get") < trace.index("subscribe") < acknowledgment
            assert "get" in trace[acknowledgment + 1 :]
        if step.get("unchanged_gets"):
            assert self.marked_gets is not None
            assert self.server.gets == self.marked_gets
        if step.get("closed"):
            assert self.http.is_closed
            assert self.active_callbacks == 0
            if self.realtime is not None:
                assert self.realtime.disconnected
                if self.realtime.subscription is not None:
                    assert self.realtime.subscription.unsubscribed


ERRORS = {
    "authentication": GameAuthenticationError,
    "protocol": GameProtocolError,
    "closed": GameClientClosedError,
    "validation": (ValueError, TypeError),
    "transport": GameTransportError,
    "http": GameError,
    "strategy": RuntimeError,
}


@pytest.mark.parametrize("scenario", SCENARIOS, ids=lambda scenario: scenario["id"])
async def test_shared_game_scenario(scenario):
    adapter = Adapter(scenario)
    try:
        for number, step in enumerate(scenario["steps"]):
            try:
                if "error" in step:
                    with pytest.raises(ERRORS[step["error"]]):
                        await adapter.perform(step)
                else:
                    await adapter.perform(step)
                await drain()
            except AssertionError as error:
                raise AssertionError(
                    f"{scenario['id']}, step {number}: {step}"
                ) from error
    finally:
        await adapter.client.close()
