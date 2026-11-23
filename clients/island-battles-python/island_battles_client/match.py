"""``island-battles-bot match``: bots against each other on map files, through
the lobby, without a browser.

    island-battles-bot match --map maps/*.json \\
        --seat hosted:https://bot.example.org/turn --seat package:dist/island-battles-bot.zip

The command does what a person does on the lobby page, with the lobby's and
the game's own APIs: one guest session per seat, each a member of one lobby
on its own team, whose bot is the seat's (a hosted bot's URL, a package that
the guest uploads, or a default bot level). The first guest creates the
lobby and hosts it. Each game loads a map file into the lobby's settings,
puts each seat on its team, gets everyone ready and starts; the server plays
every bot, and the command reads the game until it ends, then saves its demo
and its result. Between games it reopens the same lobby, so a package is
uploaded once. Each map is played from every side: with two seats, each seat
plays each colour once, and ``--games N`` plays every map and side N times.
"""

from __future__ import annotations

import argparse
import json
import sys
import threading
import time
import uuid
from collections.abc import Callable, Sequence
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any, Self
from urllib.parse import urlsplit

import httpx

from .upload import UploadError, describe, upload

GAME_KEY = "island-battles"
# The names the lobby page gives a map's teams, colour 1 first.
TEAM_NAMES = ("Blue", "Red", "Green", "Purple", "Amber", "Teal")
# The lobby's limit on a display name.
MAX_NAME_LENGTH = 40
# The lobby renews a member's lease with a heartbeat every 15 seconds; a lease
# lasts 45, so a missed heartbeat or two loses nothing.
HEARTBEAT_SECONDS = 15.0
# How often the command reads a running game.
POLL_SECONDS = 1.0
# A start whose game the lobby couldn't create (502 game_unavailable) is
# started again, as the lobby page's Retry starting game does.
START_ATTEMPTS = 5
START_RETRY_SECONDS = 2.0
# How often the command looks for Ctrl+C while lobbies play.
JOIN_SECONDS = 0.5
# A read that a proxy or the server failed (5xx) or that never got an answer
# is sent again, this long apart, up to this many times in all, before it fails.
READ_ATTEMPTS = 4
READ_RETRY_SECONDS = 2.0
# A game whose turn number doesn't advance for twice the turn timeout and this
# margin is given up: its deadline should have ended the turn long before.
STALL_MARGIN_SECONDS = 60.0
DEFAULT_TURN_TIMEOUT_SECONDS = 30
# One line at a time on standard error, from every lobby's thread.
_STDERR_LOCK = threading.Lock()


def say(line: str) -> None:
    """Write one whole line to standard error."""
    with _STDERR_LOCK:
        sys.stderr.write(line + "\n")
        sys.stderr.flush()


# The game's routes take a player token from the lobby, which lasts at most 15
# minutes; a new one is fetched once this share of its life has passed.
PLAYER_TOKEN_REFRESH_SHARE = 0.8
SEAT_KINDS = ("hosted", "package", "default")


class MatchError(Exception):
    """Why a match couldn't go on, in words for the person running it."""


class MatchCancelled(MatchError):
    """The person stopped the command."""

    def __init__(self) -> None:
        super().__init__("stopped")


class GameQueue:
    """The games still to play, which every lobby takes from, one at a time.
    Once stopped, it gives no more."""

    def __init__(self, games: Sequence[Game]) -> None:
        self._games = list(games)
        self._lock = threading.Lock()
        self.stopped = threading.Event()

    def next(self) -> Game | None:
        with self._lock:
            if self.stopped.is_set() or not self._games:
                return None
            return self._games.pop(0)

    def stop(self) -> None:
        self.stopped.set()


@dataclass(frozen=True)
class Seat:
    """A bot that plays one team: ``kind`` is hosted (``value`` a URL),
    package (``value`` a zip file) or default (``value`` a level)."""

    name: str
    kind: str
    value: str

    def member_bot(self, package_id: str | None) -> dict[str, Any]:
        """The seat's bot in the guest's member settings."""
        if self.kind == "hosted":
            return {"kind": "hosted", "url": self.value}
        if self.kind == "package":
            return {"kind": "uploaded", "package_id": package_id}
        return {"kind": "default", "level": int(self.value)}


def parse_seat(text: str) -> Seat:
    """A ``--seat``: ``[NAME=]hosted:URL``, ``[NAME=]package:ZIP`` or
    ``[NAME=]default:LEVEL``."""
    kind, separator, value = text.partition(":")
    name = ""
    if "=" in kind:
        name, _, kind = kind.partition("=")
    if not separator or kind not in SEAT_KINDS or not value:
        raise MatchError(
            f"--seat {text!r} is not hosted:URL, package:ZIP or default:LEVEL "
            "(optionally after NAME=)"
        )
    if kind == "hosted":
        parts = urlsplit(value)
        if parts.scheme not in ("http", "https") or not parts.hostname:
            raise MatchError(f"--seat {text!r}: {value!r} is not a URL")
        default_name = parts.hostname
    elif kind == "package":
        if not Path(value).is_file():
            raise MatchError(
                f"--seat {text!r}: {value} is not a file; build it with "
                "`island-battles-bot package`"
            )
        default_name = Path(value).name
    else:
        if not (value.isascii() and value.isdigit()) or int(value) < 1:
            raise MatchError(
                f"--seat {text!r}: the level must be a whole number from 1"
            )
        default_name = f"level {int(value)}"
    return Seat(name=(name or default_name)[:MAX_NAME_LENGTH], kind=kind, value=value)


def unique_names(seats: Sequence[Seat]) -> list[Seat]:
    """The seats, numbered where two have the same name."""
    renamed: list[Seat] = []
    for index, seat in enumerate(seats):
        name = seat.name
        if sum(other.name == seat.name for other in seats) > 1:
            suffix = f" ({index + 1})"
            name = seat.name[: MAX_NAME_LENGTH - len(suffix)] + suffix
        renamed.append(Seat(name=name, kind=seat.kind, value=seat.value))
    return renamed


def load_map(path: Path) -> dict[str, Any]:
    """A map file, the MapTemplate schema; the game checks the rest of it."""
    try:
        data = json.loads(path.read_text())
    except (OSError, ValueError) as error:
        raise MatchError(f"could not read the map file {path}: {error}") from error
    if not isinstance(data, dict) or not isinstance(data.get("player_amount"), int):
        raise MatchError(f"{path} is not a map file: it has no player_amount")
    return data


@dataclass(frozen=True)
class Game:
    """One game of the schedule: a map, and the seats in colour order."""

    id: str
    map_path: Path
    map: dict[str, Any]
    seat_order: tuple[int, ...]


def schedule(maps: Sequence[Path], seat_count: int, games: int) -> list[Game]:
    """Every map, from every side (the seats' rotations), ``games`` times."""
    loaded = [(path, load_map(path)) for path in maps]
    stems = [path.stem for path in maps]
    for path, data in loaded:
        if data["player_amount"] != seat_count:
            raise MatchError(
                f"{path} is a map for {data['player_amount']} teams, but there are "
                f"{seat_count} seats: give one --seat per team"
            )
    planned: list[Game] = []
    for (path, data), stem in zip(loaded, stems, strict=True):
        if stems.count(stem) > 1:
            raise MatchError(f"two map files are named {stem}; rename one")
        for repeat in range(games):
            for side in range(seat_count):
                game_id = f"{stem}-{side + 1}" + (f"-{repeat + 1}" if games > 1 else "")
                order = tuple(
                    (index + side) % seat_count for index in range(seat_count)
                )
                planned.append(Game(game_id, path, data, order))
    return planned


def team_names(count: int) -> list[dict[str, str]]:
    """The lobby's teams for a map of ``count`` teams, named as the lobby page
    names them."""
    return [
        {"name": TEAM_NAMES[index] if index < len(TEAM_NAMES) else f"Team {index + 1}"}
        for index in range(count)
    ]


def _problem(response: httpx.Response) -> str:
    """The server's own words for a refused request: the lobby's code and
    detail, or the game's detail."""
    try:
        body = response.json()
    except ValueError:
        body = None
    if isinstance(body, dict):
        detail = body.get("detail") or body.get("title") or ""
        code = body.get("code")
        return f"{code}: {detail}" if code else str(detail)
    return f"HTTP {response.status_code}"


@dataclass
class Guest:
    """One guest session, a member of the lobby who plays one seat."""

    seat: Seat
    credential: str
    player_id: str
    client_instance_id: str = field(default_factory=lambda: str(uuid.uuid4()))
    membership_generation: int | None = None
    package_id: str | None = None
    player_token: str | None = None
    player_token_renew_at: datetime | None = None


class Servers:
    """The lobby's and the game's APIs, as the guests call them."""

    def __init__(
        self,
        lobby_url: str,
        game_url: str,
        client: httpx.Client,
        clock: Callable[[], datetime] = lambda: datetime.now(UTC),
        pause: Callable[[float], None] = time.sleep,
        retry_seconds: float = READ_RETRY_SECONDS,
    ) -> None:
        self.lobby_url = lobby_url.rstrip("/")
        self.game_url = game_url.rstrip("/")
        self.client = client
        self.clock = clock
        self.pause = pause
        self.retry_seconds = retry_seconds

    def _send(
        self, method: str, url: str, retry: bool, **arguments: Any
    ) -> httpx.Response:
        """One request, sent again after a 5xx answer or none when ``retry``
        says it is safe to; raises httpx.HTTPError when no answer came."""
        attempts = READ_ATTEMPTS if retry else 1
        for attempt in range(attempts):
            if attempt:
                self.pause(self.retry_seconds)
            try:
                response = self.client.request(method, url, **arguments)
            except httpx.TransportError:
                if attempt == attempts - 1:
                    raise
                continue
            if response.status_code < 500 or attempt == attempts - 1:
                return response
        raise AssertionError("unreachable")

    def lobby(
        self,
        method: str,
        path: str,
        guest: Guest | None = None,
        body: dict[str, Any] | None = None,
        expected: tuple[int, ...] = (200, 201),
        retry: bool = False,
    ) -> httpx.Response:
        """A request to the lobby, with the guest's credential; sent again
        after a 5xx answer or none when ``retry`` says it is safe to."""
        headers = {"Accept": "application/json"}
        if guest is not None:
            headers["Authorization"] = f"Bearer {guest.credential}"
        try:
            response = self._send(
                method, f"{self.lobby_url}{path}", retry, json=body, headers=headers
            )
        except httpx.HTTPError as error:
            raise MatchError(
                f"the lobby didn't answer {method} {path}: {error}"
            ) from error
        if response.status_code not in expected:
            raise LobbyRefusal(method, path, response)
        return response

    def lobby_path(self, lobby_id: str, rest: str = "") -> str:
        return f"/lobby/v1/games/{GAME_KEY}/lobbies/{lobby_id}{rest}"

    def token(self, guest: Guest, renew: bool = False) -> str:
        """The guest's player token for the game's routes, renewed at 80 % of
        its life or when ``renew`` says the game refused it."""
        now = self.clock()
        if (
            renew
            or guest.player_token is None
            or guest.player_token_renew_at is None
            or now >= guest.player_token_renew_at
        ):
            issued = self.lobby(
                "POST",
                "/lobby/v1/player-tokens",
                guest,
                {"game_key": GAME_KEY},
                retry=True,
            ).json()
            expires = datetime.fromisoformat(issued["expires_at"])
            guest.player_token = issued["token"]
            guest.player_token_renew_at = now + max(
                timedelta(0), (expires - now) * PLAYER_TOKEN_REFRESH_SHARE
            )
        assert guest.player_token is not None
        return guest.player_token

    def game(
        self,
        method: str,
        path: str,
        guest: Guest,
        retry: bool = False,
        **arguments: Any,
    ) -> httpx.Response:
        """A request to the game with the guest's player token, again with a
        new token when the game answers 401, and after a 5xx answer or none
        when ``retry`` says it is safe to."""
        response: httpx.Response | None = None
        for renew in (False, True):
            headers = {"Authorization": f"Bearer {self.token(guest, renew)}"}
            try:
                response = self._send(
                    method,
                    f"{self.game_url}{path}",
                    retry,
                    headers=headers,
                    **arguments,
                )
            except httpx.HTTPError as error:
                raise MatchError(
                    f"the game didn't answer {method} {path}: {error}"
                ) from error
            if response.status_code != 401:
                break
        assert response is not None
        return response


class LobbyRefusal(MatchError):
    """The lobby refused a request."""

    def __init__(self, method: str, path: str, response: httpx.Response) -> None:
        self.status = response.status_code
        try:
            body = response.json()
        except ValueError:
            body = {}
        self.code = body.get("code") if isinstance(body, dict) else None
        super().__init__(f"the lobby refused {method} {path}: {_problem(response)}")


class Heartbeats:
    """Renews every guest's lease in the background, so the lobby counts them
    as connected while games play and doesn't delete the lobby as idle."""

    def __init__(
        self,
        servers: Servers,
        guests: Sequence[Guest],
        lobby_id: str,
        interval: float,
        report: Callable[[str], None] = say,
    ) -> None:
        self.servers = servers
        self.report = report
        self.guests = guests
        self.lobby_id = lobby_id
        self.interval = interval
        self.stopped = threading.Event()
        self.thread = threading.Thread(target=self._run, daemon=True, name="heartbeats")

    def __enter__(self) -> Self:
        self.thread.start()
        return self

    def __exit__(self, *_: object) -> None:
        self.stopped.set()
        self.thread.join(timeout=30)

    def beat(self) -> None:
        for guest in self.guests:
            try:
                self.servers.lobby(
                    "POST",
                    self.servers.lobby_path(self.lobby_id, "/heartbeat"),
                    guest,
                    {
                        "client_instance_id": guest.client_instance_id,
                        "membership_generation": guest.membership_generation,
                    },
                )
            except MatchError as error:
                self.report(f"heartbeat of {guest.seat.name}: {error}")

    def _run(self) -> None:
        while not self.stopped.wait(self.interval):
            self.beat()


class Match:
    """One lobby whose guests play the scheduled games, one after another."""

    def __init__(
        self,
        servers: Servers,
        seats: Sequence[Seat],
        output: Path,
        settings: dict[str, Any],
        *,
        cancel: threading.Event | None = None,
        poll_seconds: float = POLL_SECONDS,
        retry_seconds: float = START_RETRY_SECONDS,
        heartbeat_seconds: float = HEARTBEAT_SECONDS,
        report: Callable[[str], None] = say,
        monotonic: Callable[[], float] = time.monotonic,
    ) -> None:
        self.servers = servers
        self.seats = list(seats)
        self.output = output
        self.settings = settings
        self.cancel = cancel or threading.Event()
        self.poll_seconds = poll_seconds
        self.retry_seconds = retry_seconds
        self.heartbeat_seconds = heartbeat_seconds
        self.report = report
        self.monotonic = monotonic
        self.queue = GameQueue([])
        self.guests: list[Guest] = []
        self.lobby_id: str | None = None
        self.snapshot: dict[str, Any] = {}

    @property
    def host(self) -> Guest:
        return self.guests[0]

    def play(
        self,
        games: Sequence[Game] | GameQueue,
        on_result: Callable[[dict[str, Any]], None] = lambda _: None,
    ) -> list[dict[str, Any]]:
        """Play games in one lobby, taking each from ``games`` until none is
        left, and return their results, in order; ``on_result`` gets each one
        as its game ends. The guests leave the lobby at the end, also when a
        game fails or ``cancel`` is set."""
        queue = games if isinstance(games, GameQueue) else GameQueue(games)
        self.queue = queue
        game = queue.next()
        if game is None:
            return []
        self.output.mkdir(parents=True, exist_ok=True)
        results = []
        try:
            for seat in self.seats:
                self._check_cancel()
                self.guests.append(self._session(seat))
            self._open(game)
            assert self.lobby_id is not None
            with Heartbeats(
                self.servers,
                self.guests,
                self.lobby_id,
                self.heartbeat_seconds,
                self.report,
            ):
                self._choose_bots()
                first = True
                while game is not None:
                    result = self._play_game(game, first=first)
                    if result is None:
                        break
                    on_result(result)
                    results.append(result)
                    first = False
                    game = queue.next()
            return results
        finally:
            self._leave()

    def _check_cancel(self) -> None:
        if self.cancel.is_set():
            raise MatchCancelled

    def _pause(self, seconds: float) -> None:
        """Wait, unless the command is stopped meanwhile."""
        if self.cancel.wait(seconds):
            raise MatchCancelled

    def _session(self, seat: Seat) -> Guest:
        session = self.servers.lobby(
            "POST", "/lobby/v1/sessions", body={"display_name": seat.name}
        ).json()
        return Guest(
            seat=seat,
            credential=session["access_token"],
            player_id=session["player_id"],
        )

    def _settings(self, game: Game) -> dict[str, Any]:
        return {**self.settings, "map": game.map}

    def _open(self, first: Game) -> None:
        """The host creates the lobby on the first game's map, and the other
        guests join it."""
        host = self.host
        self.snapshot = self.servers.lobby(
            "POST",
            "/lobby/v1/lobbies",
            host,
            {
                "request_id": str(uuid.uuid4()),
                "game_key": GAME_KEY,
                "name": "Bot match",
                "teams": team_names(len(self.seats)),
                "game_settings": self._settings(first),
                "client_instance_id": host.client_instance_id,
            },
        ).json()
        lobby = self.snapshot["lobby"]
        self.lobby_id = lobby["id"]
        host.membership_generation = self.snapshot["membership_generation"]
        self.report(f"lobby {lobby['id']} (join code {lobby['join_code']})")
        for guest in self.guests[1:]:
            joined = self.servers.lobby(
                "POST",
                "/lobby/v1/memberships",
                guest,
                {
                    "game_key": GAME_KEY,
                    "join_code": lobby["join_code"],
                    "client_instance_id": guest.client_instance_id,
                },
            ).json()
            guest.membership_generation = joined["membership_generation"]
            self.snapshot = joined

    def _choose_bots(self) -> None:
        """Each guest uploads its package, if it plays one, and chooses its
        seat's bot, which stays for every game in the lobby."""
        assert self.lobby_id is not None
        for guest in self.guests:
            if guest.seat.kind == "package":
                guest.package_id = self._upload(guest)
            self.snapshot = self.servers.lobby(
                "PUT",
                self.servers.lobby_path(self.lobby_id, "/member-settings"),
                guest,
                {"member_settings": {"bot": guest.seat.member_bot(guest.package_id)}},
            ).json()

    def _upload(self, guest: Guest) -> str:
        """Upload the guest's package zip to the lobby, as ``upload`` does,
        with an upload token for the guest."""
        response = self.servers.game(
            "POST", f"/game/lobbies/{self.lobby_id}/bot-packages/upload-token", guest
        )
        if response.status_code != 200:
            raise MatchError(f"the game gave no upload token: {_problem(response)}")
        path = Path(guest.seat.value)
        try:
            package = upload(
                self.servers.game_url,
                response.json()["token"],
                path,
                self.servers.client,
            )
        except UploadError as error:
            raise MatchError(f"{guest.seat.name}: {error}") from error
        self.report(f"{guest.seat.name}: uploaded {path}: {describe(package)}")
        return str(package.package_id)

    def _play_game(self, game: Game, first: bool) -> dict[str, Any] | None:
        """Play one game and save it, or return None, starting nothing, when
        another lobby's game failed while this one was getting ready."""
        assert self.lobby_id is not None
        self._check_cancel()
        lobby_path = self.servers.lobby_path
        if not first:
            self.snapshot = self.servers.lobby(
                "POST", lobby_path(self.lobby_id, "/reopen"), self.host, {}
            ).json()
            self.snapshot = self.servers.lobby(
                "PUT",
                lobby_path(self.lobby_id, "/settings"),
                self.host,
                {
                    "teams": team_names(len(self.seats)),
                    "game_settings": self._settings(game),
                },
            ).json()
        teams = [
            team["id"]
            for team in sorted(self.snapshot["teams"], key=lambda team: team["order"])
        ]
        players = [self.guests[index] for index in game.seat_order]
        wanted = {
            guest.player_id: teams[colour] for colour, guest in enumerate(players)
        }
        current = {
            member["player_id"]: member["team_id"]
            for member in self.snapshot["members"]
        }
        moving = [
            guest
            for guest in self.guests
            if current.get(guest.player_id) != wanted[guest.player_id]
        ]
        # A team holds one member, so everyone who moves leaves their team first.
        for guest in moving:
            if current.get(guest.player_id) is not None:
                self._team(guest, None)
        for guest in moving:
            self._team(guest, wanted[guest.player_id])
        for guest in self.guests:
            self.snapshot = self.servers.lobby(
                "PUT", lobby_path(self.lobby_id, "/ready"), guest, {"ready": True}
            ).json()
        # A lobby that took its game before another lobby's game failed
        # still starts nothing after that.
        if self.queue.stopped.is_set():
            return None
        self._start()
        lobby_round = self.snapshot["lobby"]["round"]
        names = [guest.seat.name for guest in players]
        self.report(
            f"{game.id}: {' vs '.join(names)} on {game.map_path}, round {lobby_round}"
        )
        state = self._wait(lobby_round)
        demo_path = self.output / f"{game.id}.demo.json"
        self._save_demo(demo_path)
        outcome = state["outcome"]
        winner_colour = outcome.get("winner_team_color")
        result = {
            "id": game.id,
            "map": str(game.map_path),
            "players": names,
            "outcome": outcome,
            "winner": names[winner_colour - 1] if winner_colour else None,
            "generations": state["turn_number"],
            "lobby_id": self.lobby_id,
            "lobby_round": lobby_round,
            "game_id": state["game_id"],
            "demo": str(demo_path),
        }
        (self.output / f"{game.id}.json").write_text(
            json.dumps(result, indent=2) + "\n"
        )
        self.report(f"{game.id}: {outcome['summary']}")
        return result

    def _team(self, guest: Guest, team_id: str | None) -> None:
        assert self.lobby_id is not None
        self.snapshot = self.servers.lobby(
            "PUT",
            self.servers.lobby_path(self.lobby_id, "/team"),
            guest,
            {"team_id": team_id},
        ).json()

    def _start(self) -> None:
        assert self.lobby_id is not None
        for attempt in range(START_ATTEMPTS):
            try:
                self.snapshot = self.servers.lobby(
                    "POST",
                    self.servers.lobby_path(self.lobby_id, "/start"),
                    self.host,
                    {},
                ).json()
                return
            except LobbyRefusal as refusal:
                if refusal.code != "game_unavailable" or attempt == START_ATTEMPTS - 1:
                    raise
                self._pause(self.retry_seconds)

    def _wait(self, lobby_round: int) -> dict[str, Any]:
        """Read the host's game until it ends. Every game request first ends
        a turn whose deadline has passed, so reading also ends a turn that a
        bot didn't answer in time."""
        timeout = self.settings.get("turn_timeout_seconds") or (
            DEFAULT_TURN_TIMEOUT_SECONDS
        )
        stall_limit = 2 * timeout + STALL_MARGIN_SECONDS
        last_turn, last_advance = None, self.monotonic()
        while True:
            response = self.servers.game(
                "GET",
                f"/game/from-lobby/{self.lobby_id}/generation",
                self.host,
                retry=True,
            )
            if response.status_code != 200:
                raise MatchError(f"could not read the game: {_problem(response)}")
            state = response.json()
            if (
                state.get("lobby_round") == lobby_round
                and state.get("outcome") is not None
            ):
                return state
            now = self.monotonic()
            if state.get("turn_number") != last_turn:
                last_turn, last_advance = state.get("turn_number"), now
            elif now - last_advance > stall_limit:
                raise MatchError(
                    f"the game in lobby {self.lobby_id} stalled: turn {last_turn} "
                    f"hasn't ended in {round(now - last_advance)} s, although its "
                    f"{timeout} s deadline should have ended it"
                )
            self._pause(self.poll_seconds)

    def _save_demo(self, path: Path) -> None:
        """The game's demo, with every team's log lines, as Save demo saves it."""
        response = self.servers.game(
            "GET", f"/game/from-lobby/{self.lobby_id}/demo", self.host, retry=True
        )
        if response.status_code != 200:
            raise MatchError(f"could not save the demo: {_problem(response)}")
        path.write_bytes(response.content)

    def _leave(self) -> None:
        """Every guest leaves the lobby, which the lobby then deletes."""
        if self.lobby_id is None:
            return
        for guest in self.guests:
            try:
                self.servers.lobby(
                    "DELETE",
                    self.servers.lobby_path(self.lobby_id, "/membership"),
                    guest,
                    expected=(200, 204, 404),
                )
            except MatchError as error:
                self.report(f"{guest.seat.name} could not leave the lobby: {error}")


def play_parallel(
    make_match: Callable[[threading.Event], Match],
    games: Sequence[Game],
    parallel: int,
    on_result: Callable[[dict[str, Any]], None],
    report: Callable[[str], None],
) -> list[MatchError]:
    """Play the games in up to ``parallel`` lobbies at once, each with its own
    guests, which take the next game from one queue as they finish one.

    A failed game stops the schedule: no lobby starts another game, but the
    games that already run play to their end and are saved, since nothing can
    stop a started game early. Ctrl+C stops at once: every lobby is left, and
    a game that had started plays on at the server without anyone watching.
    Returns the failures, empty when every game was played; raises
    KeyboardInterrupt after cleaning up on Ctrl+C."""
    queue = GameQueue(games)
    cancel = threading.Event()
    failures: list[MatchError] = []
    lock = threading.Lock()

    def whole_line(result: dict[str, Any]) -> None:
        with lock:
            on_result(result)

    def worker() -> None:
        try:
            make_match(cancel).play(queue, whole_line)
        except MatchCancelled:
            pass
        except MatchError as error:
            queue.stop()
            with lock:
                failures.append(error)
            report(f"a game failed, so no lobby starts another: {error}")
        except Exception as error:  # noqa: BLE001 - reported like a refusal
            queue.stop()
            with lock:
                failures.append(MatchError(f"{type(error).__name__}: {error}"))

    started: list[threading.Thread] = []
    try:
        for index in range(max(1, min(parallel, len(games)))):
            thread = threading.Thread(target=worker, name=f"match-{index + 1}")
            # Listed before it starts, so an interrupt during start() still
            # joins it; one that never started has no ident.
            started.append(thread)
            thread.start()
        for thread in started:
            while thread.is_alive():
                thread.join(JOIN_SECONDS)
    except KeyboardInterrupt:
        report("stopping: leaving every lobby")
        cancel.set()
        queue.stop()
        # Every lobby is left before the HTTP client closes, however often
        # Ctrl+C is pressed meanwhile.
        for thread in started:
            while thread.ident is not None and thread.is_alive():
                try:
                    thread.join(JOIN_SECONDS)
                except KeyboardInterrupt:
                    report("still leaving the lobbies; wait a moment")
        raise
    return failures


def run(args: argparse.Namespace, game_url: str, lobby_url: str) -> int:
    """The ``match`` command, on the game and the lobby at these addresses."""
    try:
        if args.games < 1:
            raise MatchError("--games must be at least 1")
        if args.parallel < 1:
            raise MatchError("--parallel must be at least 1")
        seats = unique_names([parse_seat(text) for text in args.seat])
        if len(seats) < 2:
            raise MatchError("give at least two --seat, one per team of the maps")
        games = schedule([Path(path) for path in args.map], len(seats), args.games)
        settings: dict[str, Any] = {}
        if args.turn_timeout is not None:
            settings["turn_timeout_seconds"] = args.turn_timeout
        if args.max_generations is not None:
            settings["max_generations"] = args.max_generations
        output = Path(args.output)
        limits = httpx.Limits(max_connections=max(100, 10 * args.parallel))
        with httpx.Client(
            timeout=30.0, follow_redirects=False, limits=limits
        ) as client:
            servers = Servers(lobby_url, game_url, client)
            failures = play_parallel(
                lambda cancel: Match(
                    servers, seats, output, settings, cancel=cancel, report=say
                ),
                games,
                args.parallel,
                lambda result: print(json.dumps(result), flush=True),
                say,
            )
    except MatchError as error:
        say(f"FAIL: {error}")
        return 1
    except KeyboardInterrupt:
        say("stopped")
        return 130
    for failure in failures:
        say(f"FAIL: {failure}")
    return 1 if failures else 0
