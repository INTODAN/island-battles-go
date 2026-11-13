"""Authenticated HTTP turns and recoverable player-channel notifications."""

from __future__ import annotations

import asyncio
import inspect
import json
import logging
from collections.abc import Awaitable, Callable, Mapping
from contextlib import suppress
from datetime import UTC, datetime
from os import PathLike
from typing import Any, Self
from uuid import UUID, uuid4

import centrifuge
import httpx
from pydantic import ValidationError

from ._generated import (
    BotChoice,
    BotTurn,
    ConnectionToken,
    GameEndedEvent,
    GameOutcome,
    GenerationEvent,
    GenerationLogLine,
    GenerationState,
    PublicGenerationCommand,
    RegisterGenerationCommands,
    Upgrade,
)
from .bot import Play, add_result_lines, call_play
from .errors import (
    PROBLEM_TYPE_ERRORS,
    GameAuthenticationError,
    GameClientClosedError,
    GameEndedError,
    GameError,
    GameLogLimitError,
    GameProtocolError,
    GameTransportError,
)
from .game_log import SDK_LOGGER, ClientLogging, GameLog, report_server_limit
from .models import Generation

_sdk_logger = logging.getLogger(SDK_LOGGER)

# The server runs no timers, so a client waiting for other players asks it to
# apply the turn's deadline this many seconds after the deadline.
DEADLINE_SLACK_SECONDS = 1.0
# If the turn is still open after that, for example because the server's clock
# is behind, the client asks again after these many seconds, doubling up to
# the maximum.
DEADLINE_RETRY_SECONDS = 2.0
DEADLINE_RETRY_MAX_SECONDS = 30.0

# The game's key in the lobby, the audience of its player tokens.
GAME_KEY = "island-battles"
# The game's routes take a player token from the lobby, which lasts at most 15
# minutes. The client gets a new one once this share of its life has passed,
# and once more when the game answers 401.
PLAYER_TOKEN_REFRESH_SHARE = 0.8


def _is_lobby_path(path: str) -> bool:
    """The lobby's own routes take the guest credential; the game's take a
    player token."""
    return path.startswith("/lobby/")


def played_by_server(bot: BotChoice) -> str:
    """Says which bot the server plays a seat with, for a seat whose bot is
    not the player's localhost bot, and how to play it with this client."""
    instead = (
        "; to play it with your own bot here, choose your localhost bot "
        "in the lobby before getting ready"
    )
    if bot.kind == "default":
        return f"the backend plays this seat with its level {bot.level} default bot{instead}"
    if bot.kind == "hosted":
        return f"the server plays this seat with your hosted bot at {bot.host}{instead}"
    return f"the server plays this seat with your uploaded bot{instead}"


def _parse_state(data: object) -> GenerationState:
    try:
        return GenerationState.model_validate(data)
    except ValidationError as exc:
        raise GameProtocolError("invalid generation state") from exc


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
            # The turn that ends the game pushes game.ended instead.
            ended = isinstance(data, dict) and data.get("type") == "game.ended"
            event = (GameEndedEvent if ended else GenerationEvent).model_validate(data)
            if ended and event.state.outcome is None:
                raise GameProtocolError("game.ended without an outcome")
            self.owner._validate_identity(event.state)
            self.owner._install(event.state)
        except TypeError, ValueError, GameProtocolError:
            # Malformed or foreign publications are ignored; HTTP is authoritative.
            return


class IslandBattlesClient:
    """One authenticated player's generation-turn client.

    The client owns an injected ``http_client`` and closes it. ``play`` is the
    bot, a function (sync or async) from ``BotTurn`` to ``BotTurnResult``. The
    client calls it once per generation, serially, and sends its result as the
    turn's one completing batch: the commands, the game log lines and the
    memory, which the server stores for the next ``BotTurn.memory``. When the
    game ends, the optional ``on_game_ended(outcome, generation)`` runs once and
    ``wait()`` and ``run()`` return. While the player waits for others to complete a turn
    that has a deadline, the client asks the server to apply the deadline a
    second after it, which ends the game when someone didn't answer.

    From ``start()`` to ``close()`` the client attaches its log handlers to the
    ``island_battles`` logger, which the bot's ``island_battles.player`` logger
    propagates to: stdout unless ``log_to_stdout`` is false, ``log_file``
    when it is set, and always the game log, which sends the lines logged
    while ``play`` runs with that generation's completing request. ``log_level``
    applies to all three.
    """

    def __init__(
        self,
        base_url: str,
        credential: str,
        lobby_id: str,
        *,
        play: Play,
        on_game_ended: Callable[[GameOutcome, Generation], Awaitable[None] | None]
        | None = None,
        http_client: httpx.AsyncClient | None = None,
        sdk_factory=centrifuge.Client,
        refresh_interval: float | None = None,
        retry_attempts: int = 3,
        subscription_timeout: float = 10.0,
        sleep: Callable[[float], Awaitable[None]] = asyncio.sleep,
        clock: Callable[[], datetime] = lambda: datetime.now(UTC),
        log_level: int = logging.INFO,
        log_to_stdout: bool = True,
        log_file: str | PathLike[str] | None = None,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.credential = credential
        self.lobby_id = str(UUID(str(lobby_id)))
        self._play = play
        self._on_game_ended = on_game_ended
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
        self._clock = clock
        self._refresh_interval = refresh_interval
        self._retry_attempts = retry_attempts
        self._subscription_timeout = subscription_timeout
        self._sdk = None
        self._subscription = None
        self._state: GenerationState | None = None
        # The upgrade cost table, read once for the first turn and reused.
        self._upgrades: list[Upgrade] | None = None
        self._expected_identity: tuple[int, UUID, str, int] | None = None
        self._last_queued_generation = 0
        # Whether the state with the outcome was queued for on_game_ended. A
        # turn deadline ends the game on a generation already queued.
        self._ended_queued = False
        self._state_changed = asyncio.Event()
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
        self._logging = ClientLogging(self, log_level, log_to_stdout, log_file)
        # The player token for the game's routes, and when to get a new one.
        self._player_token: str | None = None
        self._player_token_renew_at: datetime | None = None
        self._player_token_lock = asyncio.Lock()
        # The game log of the generation whose callback runs.
        self._game_log: GameLog | None = None

    @property
    def generation(self) -> Generation | None:
        """The latest board snapshot, without transport metadata."""
        return self._state.generation if self._state is not None else None

    @property
    def team_color(self) -> int | None:
        """The player's frozen team color, known after the first state read."""
        return self._expected_identity[3] if self._expected_identity else None

    @property
    def outcome(self) -> GameOutcome | None:
        """How the game ended, or None while it runs."""
        return self._state.outcome if self._state is not None else None

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
        """Send one request: with the guest credential to the lobby, and with
        a player token to the game. A game request answered 401 is sent once
        more with a new token. A 401 that remains fails the client."""
        self._check_open()
        try:
            if _is_lobby_path(path):
                return await self._send(
                    method, path, self.credential, payload, retry_transport
                )
            token = await self._game_token()
            try:
                return await self._send(method, path, token, payload, retry_transport)
            except GameAuthenticationError:
                token = await self._game_token(stale=token)
                return await self._send(method, path, token, payload, retry_transport)
        except GameAuthenticationError as error:
            self._fail(error)
            raise

    async def _game_token(self, *, stale: str | None = None) -> str:
        """The player token for the game's routes. A new one is issued once
        PLAYER_TOKEN_REFRESH_SHARE of the current one's life has passed, or
        when ``stale``, a token the game refused, is still the current one."""
        async with self._player_token_lock:
            now = self._clock()
            if (
                self._player_token is not None
                and self._player_token != stale
                and self._player_token_renew_at is not None
                and now < self._player_token_renew_at
            ):
                return self._player_token
            data = await self._send(
                "POST",
                "/lobby/v1/player-tokens",
                self.credential,
                {"game_key": GAME_KEY},
                True,
            )
            try:
                token = data["token"]
                expires_at = datetime.fromisoformat(data["expires_at"])
            except (KeyError, TypeError, ValueError) as exc:
                raise GameProtocolError("invalid player-token response") from exc
            if not isinstance(token, str) or not token or expires_at.tzinfo is None:
                raise GameProtocolError("invalid player-token response")
            self._player_token = token
            self._player_token_renew_at = now + (expires_at - now) * (
                PLAYER_TOKEN_REFRESH_SHARE
            )
            return token

    async def _send(
        self,
        method: str,
        path: str,
        credential: str,
        payload: dict[str, Any] | None,
        retry_transport: bool,
    ) -> Any:
        """Send one request with ``credential`` and return its JSON body."""
        attempts = self._retry_attempts if retry_transport else 1
        for attempt in range(attempts):
            self._check_open()
            try:
                response = await self._http.request(
                    method,
                    f"{self.base_url}{path}",
                    headers={"Authorization": f"Bearer {credential}"},
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
                if not isinstance(problem, dict):
                    problem = {}
                detail = problem.get("detail")
                if not isinstance(detail, str) or not detail:
                    detail = f"request failed ({response.status_code})"
                problem_type = problem.get("type")
                error_class = (
                    GameAuthenticationError
                    if response.status_code == 401
                    else GameTransportError
                    if response.status_code == 429 or response.status_code >= 500
                    else PROBLEM_TYPE_ERRORS.get(problem_type, GameError)
                    if isinstance(problem_type, str)
                    else GameError
                )
                raise error_class(detail, response.status_code)
            try:
                return response.json()
            except (ValueError, TypeError) as exc:
                raise GameProtocolError("invalid JSON response") from exc
        raise AssertionError("unreachable retry state")

    async def _connection_token(self) -> tuple[str, str]:
        data = await self._request("POST", "/lobby/v1/connection-token", payload={})
        try:
            token = ConnectionToken.model_validate(data)
        except ValidationError as exc:
            raise GameProtocolError("invalid connection-token response") from exc
        return token.token, str(token.websocket_url)

    async def _get_token(self) -> str:
        try:
            token, _ = await self._connection_token()
            return token
        except GameAuthenticationError as exc:
            self._fail(exc)
            raise centrifuge.UnauthorizedError() from exc

    def _validate_identity(self, state: GenerationState) -> None:
        if (
            str(state.lobby_id) != self.lobby_id
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
        if current is not None and state.generation_id == current.generation_id:
            if current.outcome is not None and state.outcome is None:
                # A GET begun before a turn deadline ended the game can finish
                # afterward. The outcome is final.
                return current
            if current.submitted and not state.submitted:
                # A GET begun before a successful command can finish afterward.
                # Completion is monotone within one generation. A deadline that
                # ends the game on this generation still brings its outcome.
                if state.outcome is None or current.outcome is not None:
                    return current
                state = state.model_copy(update={"submitted": True})
            if state.log_lines_stored < current.log_lines_stored:
                # So is the stored line count. A pushed event says 0 for its
                # generation, which a restarted client may receive after its
                # read.
                state = state.model_copy(
                    update={"log_lines_stored": current.log_lines_stored}
                )
        self._state = state
        self._state_changed.set()
        ends = state.outcome is not None and not self._ended_queued
        if state.generation_id > self._last_queued_generation or ends:
            self._last_queued_generation = state.generation_id
            self._ended_queued = state.outcome is not None
            self._callbacks.put_nowait(state)
        return state

    async def _fetch_state(self) -> GenerationState:
        data = await self._request(
            "GET", f"/game/from-lobby/{self.lobby_id}/generation"
        )
        return self._install(_parse_state(data))

    async def _fetch_upgrades(self) -> list[Upgrade]:
        """Read the upgrade cost table, the same for the whole game."""
        data = await self._request("GET", "/game/upgrades", retry_transport=True)
        try:
            return [Upgrade.model_validate(entry) for entry in data["upgrades"]]
        except (KeyError, TypeError, ValidationError) as error:
            raise GameProtocolError("invalid upgrade table") from error

    async def refresh(self) -> Generation:
        """Fetch the authorized state and schedule any new generation callback."""
        self._check_open()
        if not self._started:
            raise GameError("client has not synchronized")
        async with self._reconcile_lock:
            state = await self._fetch_state()
            return state.generation

    async def start(self) -> Generation:
        """Subscribe, await acknowledgment, then fetch authoritative state."""
        try:
            async with self._start_lock:
                self._check_open()
                if self._started:
                    assert self._state is not None
                    return self._state.generation
                self._logging.attach()
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
                initial = _parse_state(discovery)
                self._validate_identity(initial)
                if initial.bot.kind != "localhost":
                    # The server plays this seat and completes its turns itself.
                    raise GameError(played_by_server(initial.bot))
                self._expected_identity = (
                    initial.game_id,
                    initial.player_id,
                    initial.channel,
                    initial.team_color,
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
                self._spawn(self._deadline_worker())
                return state.generation
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
        while not self._closed:
            state = await self._callbacks.get()
            if state.outcome is not None:
                # The final generation has no turn: report the end, then stop.
                try:
                    if self._on_game_ended is not None:
                        with self._logging.scope():
                            ended = self._on_game_ended(state.outcome, state.generation)
                            if inspect.isawaitable(ended):
                                await ended
                finally:
                    self._callbacks.task_done()
                self._stopped.set()
                return
            try:
                if self._state is not None and (
                    self._state.generation_id != state.generation_id
                    or self._state.submitted
                    or self._state.outcome is not None
                ):
                    continue
                # Count on from the lines an earlier run stored for this turn,
                # as the latest state of this generation says.
                latest = self._state or state
                self._game_log = GameLog(state.generation_id, latest.log_lines_stored)
                if self._upgrades is None:
                    self._upgrades = await self._fetch_upgrades()
                turn = BotTurn(
                    game_id=state.game_id,
                    generation=state.generation,
                    upgrades=self._upgrades,
                    memory=state.memory,
                    team_color=state.team_color,
                    turn_deadline_at=state.turn_deadline_at,
                )
                with self._logging.scope(self._game_log):
                    try:
                        result = await call_play(self._play, turn)
                        add_result_lines(self._game_log, result)
                        if not self._closed:
                            # The one completing batch: the commands, the lines
                            # and the memory, which the server stores.
                            await self._register(
                                list(result.commands),
                                True,
                                generation_id=state.generation_id,
                                memory=result.memory,
                            )
                    except (
                        GameEndedError,
                        GameTransportError,
                        GameAuthenticationError,
                        GameClientClosedError,
                    ):
                        raise
                    except Exception as error:
                        # The bot raised, returned an invalid result, or the
                        # server rejected the result's commands or memory. The
                        # turn stays open and the client stops after the
                        # error and the remaining lines are stored.
                        await self._send_failure_logs(state.generation_id, error)
                        raise
            except GameEndedError:
                # The turn's deadline passed during the callback, and the game
                # ended. Reconciling reads the final state, which queues
                # on_game_ended, instead of failing the client.
                self._wakeup.set()
            finally:
                self._game_log = None
                self._callbacks.task_done()

    async def _deadline_worker(self) -> None:
        """Apply the turn's deadline while this player waits for the others.

        The server runs no timers: a turn times out when a request applies its
        deadline. Once this player has completed a turn that has a deadline,
        the client posts to the deadline endpoint a second after it, unless a
        new state arrives first. The server answers with this player's state,
        which carries the outcome when someone didn't answer in time.
        """
        attempts: tuple[int, int] = (0, 0)
        while not self._closed:
            self._state_changed.clear()
            state = self._state
            if (
                state is None
                or state.outcome is not None
                or state.turn_deadline_at is None
                or not state.submitted
            ):
                await self._state_changed.wait()
                continue
            tries = attempts[1] if attempts[0] == state.generation_id else 0
            delay = (
                state.turn_deadline_at - self._clock()
            ).total_seconds() + DEADLINE_SLACK_SECONDS
            if tries:
                # The turn was still open after an earlier attempt.
                delay = max(
                    delay,
                    min(
                        DEADLINE_RETRY_SECONDS * 2 ** (tries - 1),
                        DEADLINE_RETRY_MAX_SECONDS,
                    ),
                )
            if delay > 0 and await self._state_changes_within(delay):
                continue
            attempts = (state.generation_id, tries + 1)
            try:
                async with self._reconcile_lock:
                    data = await self._request(
                        "POST",
                        f"/game/from-lobby/{self.lobby_id}/deadline",
                        retry_transport=True,
                    )
                    self._install(_parse_state(data))
            except GameAuthenticationError:
                raise
            except GameError as error:
                # The next attempt waits longer; HTTP reads still recover the state.
                _sdk_logger.warning("could not apply the turn deadline: %s", error)

    async def _state_changes_within(self, delay: float) -> bool:
        """Wait up to ``delay`` seconds; return whether the state changed meanwhile."""
        changed = asyncio.create_task(self._state_changed.wait())
        timer = asyncio.ensure_future(self._sleep(delay))
        try:
            done, _ = await asyncio.wait(
                (changed, timer), return_when=asyncio.FIRST_COMPLETED
            )
        finally:
            for task in (changed, timer):
                task.cancel()
            await asyncio.gather(changed, timer, return_exceptions=True)
        return changed in done

    async def _send_failure_logs(self, generation_id: int, error: Exception) -> None:
        """Log the failure and send the generation's remaining lines.

        The batch leaves the turn open, and the client stops after it, so a
        crashing bot's last lines and traceback are kept.
        """
        # Logged inside the turn's scope, so the game log stores it too.
        _sdk_logger.error(
            "the bot failed on generation %d", generation_id, exc_info=error
        )
        try:
            await self._register([], False, generation_id=generation_id, last=True)
        except GameError as send_error:
            _sdk_logger.warning(
                "could not send the failed generation's logs: %s", send_error
            )

    async def _register(
        self,
        commands: list[PublicGenerationCommand],
        complete: bool,
        *,
        generation_id: int,
        last: bool | None = None,
        memory: Mapping[str, Any] | None = None,
    ) -> GenerationState:
        """Send one batch; ``last`` (default: ``complete``) marks the turn's last request."""
        self._check_open()
        last = complete if last is None else last
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
            # The lines logged since the previous request for this generation.
            game_log = self._game_log
            if game_log is not None and game_log.generation_id != generation_id:
                game_log = None
            logs, sent = game_log.batch(last) if game_log is not None else ([], 0)
            game_id = self._state.game_id

            async def post(lines: list[GenerationLogLine]) -> Any:
                payload = RegisterGenerationCommands(
                    game_id=game_id,
                    generation_id=generation_id,
                    request_id=uuid4(),
                    complete=complete,
                    commands=commands,
                    memory=memory,
                    logs=lines or None,
                ).model_dump(mode="json", by_alias=True, exclude_none=True)
                # The transport retries this exact payload, including its UUID.
                return await self._request(
                    "POST",
                    "/generation/commands",
                    payload=payload,
                    retry_transport=True,
                )

            try:
                result = await post(logs)
            except GameLogLimitError as error:
                if game_log is None or not logs:
                    raise
                # The server holds more lines than this client counted, for
                # example from another client of the same player. Drop the
                # lines, and send the commands and completion without them.
                game_log.seal()
                report_server_limit(generation_id, error.detail)
                result = await post([])
            except GameTransportError:
                # The server may have stored the lines; sending them again could
                # pass the line limit, so they are not resent.
                if game_log is not None:
                    game_log.commit(sent, last)
                raise
            if game_log is not None:
                game_log.commit(sent, last)
            return self._install(_parse_state(result))

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
        try:
            await asyncio.wait_for(self._http.aclose(), 2)
        finally:
            self._logging.detach()
