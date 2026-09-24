"""Authenticated HTTP turns and recoverable player-channel notifications."""

from __future__ import annotations

import asyncio
import json
from collections.abc import Awaitable, Callable, Iterable, Mapping
from contextlib import suppress
from contextvars import ContextVar
from typing import Any
from uuid import UUID, uuid4

import centrifuge
import httpx

from .errors import (
    GameAuthenticationError, GameClientClosedError, GameError,
    GameProtocolError, GameTransportError,
)
from .models import GenerationState, SoldierGroupOrder


_callback_generation: ContextVar[int | None] = ContextVar("island_battles_callback_generation", default=None)


class _ConnectionEvents(centrifuge.ClientEventHandler):
    def __init__(self, owner: IslandBattlesClient):
        self.owner = owner

    async def on_connected(self, ctx):
        self.owner._wakeup.set()

    async def on_disconnected(self, ctx):
        self.owner._wakeup.set()


class _SubscriptionEvents(centrifuge.SubscriptionEventHandler):
    def __init__(self, owner: IslandBattlesClient):
        self.owner = owner

    async def on_subscribed(self, ctx):
        self.owner._acknowledged.set()
        self.owner._wakeup.set()

    async def on_subscribing(self, ctx):
        self.owner._acknowledged.clear()

    async def on_unsubscribed(self, ctx):
        self.owner._acknowledged.clear()
        self.owner._wakeup.set()

    async def on_publication(self, ctx):
        # Never await HTTP or a user callback from the SDK read loop.
        try:
            data = ctx.pub.data
            if isinstance(data, (str, bytes)):
                data = json.loads(data)
            if not isinstance(data, dict) or data.get("type") != "game.next_generation":
                return
            if type(data.get("schema_version")) is not int or data["schema_version"] != 1:
                return
            event = GenerationState.from_dict(data["state"])
            self.owner._validate_identity(event)
            self.owner._wakeup.set()
        except (KeyError, TypeError, ValueError, GameProtocolError):
            # Malformed or foreign publications are ignored; HTTP is authoritative.
            return


class IslandBattlesClient:
    """One authenticated player's generation-turn client.

    The client owns an injected ``http_client`` and closes it. A subclass may
    override ``next_generation``; callbacks run serially and may submit turns.
    """

    def __init__(
        self, base_url: str, credential: str, lobby_id: str, *,
        http_client: httpx.AsyncClient | None = None,
        sdk_factory=centrifuge.Client,
        refresh_interval: float = 15.0,
        retry_attempts: int = 3,
        subscription_timeout: float = 10.0,
        sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
    ):
        self.base_url = base_url.rstrip("/")
        self.credential = credential
        self.lobby_id = str(UUID(str(lobby_id)))
        if refresh_interval <= 0 or retry_attempts < 1 or subscription_timeout <= 0:
            raise ValueError("interval, attempts, and subscription timeout must be positive")
        self._http = http_client or httpx.AsyncClient(base_url=self.base_url, timeout=10)
        self._sdk_factory = sdk_factory
        self._sleep = sleep
        self._refresh_interval = refresh_interval
        self._retry_attempts = retry_attempts
        self._subscription_timeout = subscription_timeout
        self._sdk = None
        self._subscription = None
        self._state: GenerationState | None = None
        self._expected_identity: tuple[int, str, str] | None = None
        self._last_queued_generation = 0
        self._request_payloads: dict[UUID, dict[str, Any]] = {}
        self._start_lock = asyncio.Lock()
        self._command_lock = asyncio.Lock()
        self._reconcile_lock = asyncio.Lock()
        self._acknowledged = asyncio.Event()
        self._wakeup = asyncio.Event()
        self._stopped = asyncio.Event()
        self._failed = asyncio.Event()
        self._callbacks: asyncio.Queue[GenerationState] = asyncio.Queue()
        self._tasks: set[asyncio.Task] = set()
        self._failure: BaseException | None = None
        self._close_task: asyncio.Task | None = None
        self._started = False
        self._closed = False

    @property
    def state(self) -> GenerationState | None:
        return self._state

    async def next_generation(self, generation: GenerationState) -> None:
        """Override to react to a generation. Called once per generation ID."""

    async def __aenter__(self) -> IslandBattlesClient:
        await self.start()
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        await self.close()

    def _check_open(self) -> None:
        if self._closed:
            raise GameClientClosedError()
        if self._failure is not None:
            raise self._failure
        if not self.credential:
            raise GameAuthenticationError("missing credential", 401)

    def _spawn(self, coroutine) -> None:
        task = asyncio.create_task(coroutine)
        self._tasks.add(task)

        def done(completed: asyncio.Task) -> None:
            self._tasks.discard(completed)
            if not completed.cancelled() and not self._closed:
                error = completed.exception()
                if error is not None:
                    self._fail(error)

        task.add_done_callback(done)

    def _fail(self, error: BaseException) -> None:
        if self._failure is None:
            self._failure = error
            self._failed.set()

    async def _request(self, method: str, path: str, *, payload: dict[str, Any] | None = None,
                       retry_transport: bool = False) -> Any:
        self._check_open()
        attempts = self._retry_attempts if retry_transport else 1
        for attempt in range(attempts):
            try:
                response = await self._http.request(
                    method, f"{self.base_url}{path}",
                    headers={"Authorization": f"Bearer {self.credential}"}, json=payload,
                )
            except httpx.TransportError as exc:
                if attempt + 1 == attempts:
                    raise GameTransportError("network error") from exc
                await self._sleep(min(0.1 * 2 ** attempt, 1.0))
                continue
            if response.status_code >= 400:
                try:
                    problem = response.json()
                except (ValueError, TypeError):
                    problem = {}
                detail = problem.get("detail") if isinstance(problem, dict) else None
                if not isinstance(detail, str) or not detail:
                    detail = f"request failed ({response.status_code})"
                error_class = (GameAuthenticationError if response.status_code == 401 else
                               GameTransportError if response.status_code == 429 or response.status_code >= 500 else
                               GameError)
                error = error_class(detail, response.status_code)
                if isinstance(error, GameAuthenticationError):
                    self._fail(error)
                raise error
            try:
                return response.json()
            except (ValueError, TypeError) as exc:
                raise GameProtocolError("invalid JSON response") from exc
        raise AssertionError("unreachable retry state")

    async def _connection_token(self) -> tuple[str, str]:
        data = await self._request("POST", "/lobby/v1/connection-token", payload={})
        if not isinstance(data, dict) or not isinstance(data.get("token"), str) or not isinstance(data.get("websocket_url"), str):
            raise GameProtocolError("invalid connection-token response")
        return data["token"], data["websocket_url"]

    async def _get_token(self) -> str:
        try:
            token, _ = await self._connection_token()
            return token
        except GameAuthenticationError as exc:
            self._fail(exc)
            raise centrifuge.UnauthorizedError() from exc

    def _validate_identity(self, state: GenerationState) -> None:
        if state.lobby_id != self.lobby_id or state.channel != f"player:{state.player_id}":
            raise GameProtocolError("generation state has wrong lobby or player channel")
        if self._expected_identity is not None and (
            state.game_id, state.player_id, state.channel
        ) != self._expected_identity:
            raise GameProtocolError("generation state has wrong game or player")
        if self._state is not None and (state.game_id != self._state.game_id or
                                        state.player_id != self._state.player_id):
            raise GameProtocolError("generation state has wrong game or player")

    def _install(self, state: GenerationState) -> GenerationState:
        self._validate_identity(state)
        current = self._state
        if current is not None and state.generation_id < current.generation_id:
            return current
        if (current is not None and state.generation_id == current.generation_id and
                current.submitted and not state.submitted):
            # A GET begun before a successful command can finish afterward.
            # Completion is monotone within one generation.
            return current
        self._state = state
        if state.generation_id > self._last_queued_generation:
            self._last_queued_generation = state.generation_id
            self._callbacks.put_nowait(state)
        return state

    async def _fetch_state(self) -> GenerationState:
        data = await self._request("GET", f"/game/from-lobby/{self.lobby_id}/generation")
        if not isinstance(data, dict):
            raise GameProtocolError("invalid generation state")
        return self._install(GenerationState.from_dict(data))

    async def refresh(self) -> GenerationState:
        """Fetch the authorized state and schedule any new generation callback."""
        self._check_open()
        if not self._started:
            raise GameError("client has not synchronized")
        async with self._reconcile_lock:
            return await self._fetch_state()

    async def start(self) -> GenerationState:
        """Subscribe, await acknowledgment, then fetch authoritative state."""
        try:
            async with self._start_lock:
                self._check_open()
                if self._started:
                    return self._state  # type: ignore[return-value]
                token, address = await self._connection_token()
                self._sdk = self._sdk_factory(address, token=token, get_token=self._get_token,
                                              events=_ConnectionEvents(self))
                await self._sdk.connect()
                # The first authorized HTTP read provides the player channel. A
                # personal channel is derivable from the verified session only
                # after this read, so fetch once to discover it, then reconcile
                # again after the subscription acknowledgment.
                discovery = await self._request("GET", f"/game/from-lobby/{self.lobby_id}/generation")
                if not isinstance(discovery, dict):
                    raise GameProtocolError("invalid generation state")
                initial = GenerationState.from_dict(discovery)
                self._validate_identity(initial)
                self._expected_identity = (initial.game_id, initial.player_id, initial.channel)
                self._subscription = self._sdk.new_subscription(
                    initial.channel, events=_SubscriptionEvents(self), recoverable=True, positioned=True)
                await self._subscription.subscribe()
                try:
                    await asyncio.wait_for(self._subscription.ready(), self._subscription_timeout)
                    await asyncio.wait_for(self._acknowledged.wait(), self._subscription_timeout)
                except TimeoutError as exc:
                    # An SDK denial may resolve ready() without an acknowledgment.
                    await self._fetch_state()
                    raise GameError("player-channel subscription was not acknowledged") from exc
                state = await self._fetch_state()
                self._started = True
                self._spawn(self._reconcile_worker())
                self._spawn(self._periodic_worker())
                self._spawn(self._callback_worker())
                return state
        except BaseException:
            await self.close()
            raise

    async def _reconcile_worker(self) -> None:
        while not self._closed:
            await self._wakeup.wait()
            self._wakeup.clear()
            try:
                async with self._reconcile_lock:
                    await self._fetch_state()
            except GameTransportError:
                # A periodic refresh will retry if the connection remains down.
                pass

    async def _periodic_worker(self) -> None:
        while not self._closed:
            await self._sleep(self._refresh_interval)
            self._wakeup.set()

    async def _callback_worker(self) -> None:
        while not self._closed:
            state = await self._callbacks.get()
            token = _callback_generation.set(state.generation_id)
            try:
                await self.next_generation(state)
            finally:
                _callback_generation.reset(token)
                self._callbacks.task_done()

    @staticmethod
    def _commands(groups: Iterable[SoldierGroupOrder | Mapping[str, int]]) -> list[dict[str, Any]]:
        commands = []
        for group in groups:
            if isinstance(group, SoldierGroupOrder):
                commands.append(group.to_command())
            elif isinstance(group, Mapping):
                try:
                    commands.append(SoldierGroupOrder(
                        group["origin_logical_castle_id"],
                        group["destination_logical_castle_id"],
                        group["soldier_amount"],
                    ).to_command())
                except KeyError as exc:
                    raise ValueError(f"missing order parameter: {exc.args[0]}") from exc
            else:
                raise TypeError("orders must be SoldierGroupOrder or a mapping")
        return commands

    async def _register(self, commands: list[dict[str, Any]], complete: bool, *,
                        request_id: UUID | str | None, generation_id: int | None) -> GenerationState:
        self._check_open()
        async with self._command_lock:
            if self._state is None:
                raise GameError("client has not synchronized")
            identifier = uuid4() if request_id is None else UUID(str(request_id))
            previous = self._request_payloads.get(identifier)
            selected_generation = (generation_id if generation_id is not None else
                                   _callback_generation.get() or self._state.generation_id)
            payload = {"game_id": self._state.game_id, "generation_id": selected_generation,
                       "request_id": str(identifier), "complete": complete, "commands": commands}
            if previous is not None:
                if (previous["game_id"], previous["request_id"], previous["complete"], previous["commands"]) != (
                    payload["game_id"], payload["request_id"], complete, commands
                ) or (generation_id is not None and generation_id != previous["generation_id"]):
                    raise GameError("request UUID already used with a different batch", 409)
                payload = previous
            else:
                self._request_payloads[identifier] = payload
            result = await self._request("POST", "/generation/commands", payload=payload,
                                         retry_transport=True)
            if not isinstance(result, dict):
                raise GameProtocolError("invalid generation state")
            return self._install(GenerationState.from_dict(result))

    async def send_soldier_groups(
        self, groups: Iterable[SoldierGroupOrder | Mapping[str, int]], *,
        request_id: UUID | str | None = None, generation_id: int | None = None,
    ) -> GenerationState:
        """Immediately register a batch without completing this turn."""
        return await self._register(self._commands(groups), False,
                                    request_id=request_id, generation_id=generation_id)

    async def send_soldier_group(
        self, origin_logical_castle_id: int, destination_logical_castle_id: int,
        soldier_amount: int, *, request_id: UUID | str | None = None,
        generation_id: int | None = None,
    ) -> GenerationState:
        return await self.send_soldier_groups([
            SoldierGroupOrder(origin_logical_castle_id, destination_logical_castle_id, soldier_amount)
        ], request_id=request_id, generation_id=generation_id)

    async def end_turn(self, *, request_id: UUID | str | None = None,
                       generation_id: int | None = None) -> GenerationState:
        """Immediately seal the turn; an empty final batch is valid."""
        return await self._register([], True, request_id=request_id, generation_id=generation_id)

    async def wait(self) -> None:
        """Wait until closed or a background callback/reconciliation fails."""
        tasks = (asyncio.create_task(self._stopped.wait()),
                 asyncio.create_task(self._failed.wait()))
        try:
            await asyncio.wait(tasks, return_when=asyncio.FIRST_COMPLETED)
        finally:
            for task in tasks:
                task.cancel()
            await asyncio.gather(*tasks, return_exceptions=True)
        if self._failure is not None:
            raise self._failure

    async def run(self) -> None:
        try:
            await self.start()
            await self.wait()
        finally:
            await self.close()

    async def close(self) -> None:
        if self._close_task is None:
            self._closed = True
            self._stopped.set()
            self._close_task = asyncio.create_task(self._shutdown(asyncio.current_task()))
        await asyncio.shield(self._close_task)

    async def _shutdown(self, caller: asyncio.Task | None) -> None:
        tasks = [task for task in self._tasks if task is not caller]
        for task in tasks:
            task.cancel()
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
        if self._subscription is not None:
            with suppress(Exception):
                await asyncio.wait_for(self._subscription.unsubscribe(), 2)
            if self._sdk is not None:
                with suppress(Exception):
                    self._sdk.remove_subscription(self._subscription)
        if self._sdk is not None:
            with suppress(Exception):
                await asyncio.wait_for(self._sdk.disconnect(), 2)
        await asyncio.wait_for(self._http.aclose(), 2)
