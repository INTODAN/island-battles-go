"""Authenticated HTTP turns and recoverable player-channel notifications."""

from __future__ import annotations

import asyncio
import json
from collections.abc import Awaitable, Callable
from contextlib import suppress
from typing import Any, Self
from uuid import UUID, uuid4

import centrifuge
import httpx

from ._protocol import GenerationState
from .commands import CommandReceiver
from .errors import (
    GameAuthenticationError,
    GameClientClosedError,
    GameError,
    GameProtocolError,
    GameTransportError,
)
from .models import Generation
from .player import Player


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
        if self.owner._closed:
            return
        try:
            data = ctx.pub.data
            if isinstance(data, (str, bytes)):
                data = json.loads(data)
            if not isinstance(data, dict) or data.get("type") != "game.next_generation":
                return
            if (
                type(data.get("schema_version")) is not int
                or data["schema_version"] != 1
            ):
                return
            event = GenerationState.parse(data["state"])
            self.owner._validate_identity(event)
            self.owner._install(event)
        except KeyError, TypeError, ValueError, GameProtocolError:
            # Malformed or foreign publications are ignored; HTTP is authoritative.
            return


class IslandBattlesClient:
    """One authenticated player's generation-turn client.

    The client owns an injected ``http_client`` and closes it. ``player_factory``
    receives the authenticated team color once at startup. The resulting player's
    callbacks run serially; successful return automatically completes the turn.
    """

    def __init__(
        self,
        base_url: str,
        credential: str,
        lobby_id: str,
        *,
        player_factory: Callable[[int], Player],
        http_client: httpx.AsyncClient | None = None,
        sdk_factory=centrifuge.Client,
        refresh_interval: float | None = None,
        retry_attempts: int = 3,
        subscription_timeout: float = 10.0,
        sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.credential = credential
        self.lobby_id = str(UUID(str(lobby_id)))
        self._player_factory = player_factory
        self._player: Player | None = None
        if (
            (refresh_interval is not None and refresh_interval <= 0)
            or retry_attempts < 1
            or subscription_timeout <= 0
        ):
            raise ValueError(
                "interval, attempts, and subscription timeout must be positive"
            )
        self._http = http_client or httpx.AsyncClient(
            base_url=self.base_url, timeout=10
        )
        self._sdk_factory = sdk_factory
        self._sleep = sleep
        self._refresh_interval = refresh_interval
        self._retry_attempts = retry_attempts
        self._subscription_timeout = subscription_timeout
        self._sdk = None
        self._subscription = None
        self._state: GenerationState | None = None
        self._expected_identity: tuple[int, str, str, int] | None = None
        self._last_queued_generation = 0
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
    def generation(self) -> Generation | None:
        """The latest board snapshot, without transport metadata."""
        return self._state.public_generation() if self._state is not None else None

    @property
    def player(self) -> Player | None:
        """The strategy created after the first authorized state read."""
        return self._player

    async def __aenter__(self) -> Self:
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

    async def _request(
        self,
        method: str,
        path: str,
        *,
        payload: dict[str, Any] | None = None,
        retry_transport: bool = False,
    ) -> Any:
        self._check_open()
        attempts = self._retry_attempts if retry_transport else 1
        for attempt in range(attempts):
            try:
                response = await self._http.request(
                    method,
                    f"{self.base_url}{path}",
                    headers={"Authorization": f"Bearer {self.credential}"},
                    json=payload,
                )
            except httpx.TransportError as exc:
                if attempt + 1 == attempts:
                    raise GameTransportError("network error") from exc
                await self._sleep(min(0.1 * 2**attempt, 1.0))
                continue
            if response.status_code >= 400:
                try:
                    problem = response.json()
                except ValueError, TypeError:
                    problem = {}
                detail = problem.get("detail") if isinstance(problem, dict) else None
                if not isinstance(detail, str) or not detail:
                    detail = f"request failed ({response.status_code})"
                error_class = (
                    GameAuthenticationError
                    if response.status_code == 401
                    else GameTransportError
                    if response.status_code == 429 or response.status_code >= 500
                    else GameError
                )
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
        if (
            not isinstance(data, dict)
            or not isinstance(data.get("token"), str)
            or not isinstance(data.get("websocket_url"), str)
        ):
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
        if (
            state.lobby_id != self.lobby_id
            or state.channel != f"player:{state.player_id}"
        ):
            raise GameProtocolError(
                "generation state has wrong lobby or player channel"
            )
        if (
            self._expected_identity is not None
            and (state.game_id, state.player_id, state.channel, state.team_color)
            != self._expected_identity
        ):
            raise GameProtocolError(
                "generation state has wrong game, player, or team color"
            )
        if self._state is not None and (
            state.game_id != self._state.game_id
            or state.player_id != self._state.player_id
        ):
            raise GameProtocolError("generation state has wrong game or player")

    def _install(self, state: GenerationState) -> GenerationState:
        self._validate_identity(state)
        current = self._state
        if current is not None and state.generation_id < current.generation_id:
            return current
        if (
            current is not None
            and state.generation_id == current.generation_id
            and current.submitted
            and not state.submitted
        ):
            # A GET begun before a successful command can finish afterward.
            # Completion is monotone within one generation.
            return current
        self._state = state
        if state.generation_id > self._last_queued_generation:
            self._last_queued_generation = state.generation_id
            self._callbacks.put_nowait(state)
        return state

    async def _fetch_state(self) -> GenerationState:
        data = await self._request(
            "GET", f"/game/from-lobby/{self.lobby_id}/generation"
        )
        if not isinstance(data, dict):
            raise GameProtocolError("invalid generation state")
        return self._install(GenerationState.parse(data))

    async def refresh(self) -> Generation:
        """Fetch the authorized state and schedule any new generation callback."""
        self._check_open()
        if not self._started:
            raise GameError("client has not synchronized")
        async with self._reconcile_lock:
            state = await self._fetch_state()
            return state.public_generation()

    async def start(self) -> Generation:
        """Subscribe, await acknowledgment, then fetch authoritative state."""
        try:
            async with self._start_lock:
                self._check_open()
                if self._started:
                    assert self._state is not None
                    return self._state.public_generation()
                token, address = await self._connection_token()
                self._sdk = self._sdk_factory(
                    address,
                    token=token,
                    get_token=self._get_token,
                    events=_ConnectionEvents(self),
                )
                await self._sdk.connect()
                # The first authorized HTTP read provides the player channel. A
                # personal channel is derivable from the verified session only
                # after this read, so fetch once to discover it, then reconcile
                # again after the subscription acknowledgment.
                discovery = await self._request(
                    "GET", f"/game/from-lobby/{self.lobby_id}/generation"
                )
                if not isinstance(discovery, dict):
                    raise GameProtocolError("invalid generation state")
                initial = GenerationState.parse(discovery)
                self._validate_identity(initial)
                self._expected_identity = (
                    initial.game_id,
                    initial.player_id,
                    initial.channel,
                    initial.team_color,
                )
                self._player = self._player_factory(initial.team_color)
                if self._player.team_color != initial.team_color:
                    raise GameProtocolError(
                        "player factory returned a different team color"
                    )
                self._subscription = self._sdk.new_subscription(
                    initial.channel,
                    events=_SubscriptionEvents(self),
                    recoverable=True,
                    positioned=True,
                )
                await self._subscription.subscribe()
                try:
                    await asyncio.wait_for(
                        self._subscription.ready(), self._subscription_timeout
                    )
                    await asyncio.wait_for(
                        self._acknowledged.wait(), self._subscription_timeout
                    )
                except TimeoutError as exc:
                    # An SDK denial may resolve ready() without an acknowledgment.
                    await self._fetch_state()
                    raise GameError(
                        "player-channel subscription was not acknowledged"
                    ) from exc
                state = await self._fetch_state()
                self._started = True
                self._spawn(self._reconcile_worker())
                if self._refresh_interval is not None:
                    self._spawn(self._periodic_worker())
                self._spawn(self._callback_worker())
                return state.public_generation()
        except BaseException:
            await self.close()
            raise

    async def _reconcile_worker(self) -> None:
        failures = 0
        while not self._closed:
            await self._wakeup.wait()
            self._wakeup.clear()
            try:
                async with self._reconcile_lock:
                    await self._fetch_state()
                failures = 0
            except GameTransportError:
                # Retry failed subscription reconciliation without idle polling.
                failures += 1
                await self._sleep(min(0.1 * 2 ** min(failures - 1, 7), 10.0))
                self._wakeup.set()

    async def _periodic_worker(self) -> None:
        if self._refresh_interval is None:
            return
        while not self._closed:
            await self._sleep(self._refresh_interval)
            self._wakeup.set()

    async def _callback_worker(self) -> None:
        assert self._player is not None
        while not self._closed:
            state = await self._callbacks.get()
            receiver = self._receiver(state.generation_id)
            try:
                if self._state is not None and (
                    self._state.generation_id != state.generation_id
                    or self._state.submitted
                ):
                    continue
                try:
                    await self._player.on_generation(
                        state.public_generation(), receiver
                    )
                finally:
                    receiver._close()
                if not self._closed:
                    await self._register([], True, generation_id=state.generation_id)
            finally:
                receiver._close()
                self._callbacks.task_done()

    def _receiver(self, generation_id: int) -> CommandReceiver:
        async def submit(command: dict[str, Any]) -> None:
            await self._register([command], False, generation_id=generation_id)

        return CommandReceiver(submit)

    async def _register(
        self,
        commands: list[dict[str, Any]],
        complete: bool,
        *,
        generation_id: int,
    ) -> GenerationState:
        self._check_open()
        async with self._command_lock:
            if self._state is None:
                raise GameError("client has not synchronized")
            if complete and (
                generation_id < self._state.generation_id
                or (
                    generation_id == self._state.generation_id and self._state.submitted
                )
            ):
                # A callback or concurrent request may have already sealed this
                # turn. Check under the command lock to avoid a second completion.
                return self._state
            payload = {
                "game_id": self._state.game_id,
                "generation_id": generation_id,
                "request_id": str(uuid4()),
                "complete": complete,
                "commands": commands,
            }
            # The transport retries this exact payload, including its generated UUID.
            result = await self._request(
                "POST", "/generation/commands", payload=payload, retry_transport=True
            )
            if not isinstance(result, dict):
                raise GameProtocolError("invalid generation state")
            return self._install(GenerationState.parse(result))

    async def wait(self) -> None:
        """Wait until closed or a background callback/reconciliation fails."""
        tasks = (
            asyncio.create_task(self._stopped.wait()),
            asyncio.create_task(self._failed.wait()),
        )
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
            self._close_task = asyncio.create_task(
                self._shutdown(asyncio.current_task())
            )
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
