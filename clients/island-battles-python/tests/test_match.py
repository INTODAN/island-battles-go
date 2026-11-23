"""``island-battles-bot match`` against a fake lobby and game."""

import _thread
import base64
import json
import re
import threading
import time
from pathlib import Path
from typing import Any

import httpx
import pytest

from island_battles_client import cli
from island_battles_client.match import (
    Match,
    MatchCancelled,
    MatchError,
    Seat,
    Servers,
    parse_seat,
    play_parallel,
    schedule,
    unique_names,
)

LOBBY = "https://lobby.example"
GAME = "https://game.example"
LOBBY_PATH = re.compile(r"^/lobby/v1/games/island-battles/lobbies/([^/]+)(/.*)$")
GAME_PATH = re.compile(r"^/game/(?:lobbies|from-lobby)/([^/]+)(/.*)$")


def circle_map(name: str, teams: int = 2) -> dict[str, Any]:
    return {
        "map_name": name,
        "player_amount": teams,
        "plane_size_x": 8,
        "plane_size_y": 6,
        "neutral_starting_soldier_amount": 0,
        "castles": [
            {"team_color": index + 1, "x": index, "y": 0, "starting_soldier_amount": 30}
            for index in range(teams)
        ],
    }


class FakeLobby:
    """One lobby, with its teams, members, round and game."""

    def __init__(self, number: int) -> None:
        self.id = f"6f1c2c9e-7d0a-4a43-9c55-6e3f0a1b2c{number:02d}"
        self.join_code = f"CODE{number}"
        self.members: dict[str, dict[str, Any]] = {}  # player ID: member
        self.teams: list[dict[str, Any]] = []
        self.settings: dict[str, Any] = {}
        self.phase = "waiting"
        self.round = 1
        self.polls = 0

    def snapshot(self) -> dict[str, Any]:
        return {
            "lobby": {
                "id": self.id,
                "join_code": self.join_code,
                "round": self.round,
                "phase": self.phase,
                "game_settings": self.settings,
            },
            "teams": [dict(team) for team in self.teams],
            "members": [dict(member) for member in self.members.values()],
            "membership_generation": 1,
        }

    def make_teams(self, definitions: list[dict[str, str]]) -> None:
        old = self.teams
        self.teams = [
            {
                "id": old[index]["id"]
                if index < len(old)
                else f"{self.id}-team-{index}",
                "name": item["name"],
                "order": index,
            }
            for index, item in enumerate(definitions)
        ]


class FakeServers:
    """The lobby and the game, as much of them as match uses, with their rules
    on teams, readiness and rounds, for any number of lobbies."""

    def __init__(
        self,
        unavailable_starts: int = 0,
        polls_per_game: int = 2,
        refused_map: str | None = None,
    ) -> None:
        self.lock = threading.Lock()
        self.requests: list[tuple[str, str]] = []
        self.players: dict[str, str] = {}  # credential: player ID
        self.names: dict[str, str] = {}  # player ID: display name
        self.lobbies: dict[str, FakeLobby] = {}
        self.player_lobby: dict[str, FakeLobby] = {}
        self.packages: dict[str, str] = {}  # package ID: owner
        self.uploads: list[bytes] = []
        self.unavailable_starts = unavailable_starts
        self.polls_per_game = polls_per_game
        self.refused_map = refused_map
        self.starts: list[dict[str, Any]] = []
        self.left: list[str] = []
        # The most games that ran at once.
        self.most_running = 0

    def client(self) -> httpx.Client:
        return httpx.Client(transport=httpx.MockTransport(self))

    @staticmethod
    def problem(status: int, code: str, detail: str = "") -> httpx.Response:
        return httpx.Response(
            status, json={"code": code, "detail": detail or code, "status": status}
        )

    def join(self, lobby: FakeLobby, player: str) -> None:
        lobby.members[player] = {
            "player_id": player,
            "display_name": self.names[player],
            "team_id": None,
            "ready": False,
            "member_settings": None,
            "kind": "human",
        }
        self.player_lobby[player] = lobby

    def running(self) -> int:
        return sum(lobby.phase == "started" for lobby in self.lobbies.values())

    def __call__(self, request: httpx.Request) -> httpx.Response:
        with self.lock:
            return self.handle(request)

    def handle(self, request: httpx.Request) -> httpx.Response:
        method, path = request.method, request.url.path
        self.requests.append(
            (method, f"{request.url.scheme}://{request.url.host}{path}")
        )
        body = (
            json.loads(request.content)
            if request.content
            and request.headers.get("content-type", "").startswith("application/json")
            else None
        )
        authorization = request.headers.get("authorization", "").removeprefix("Bearer ")
        if request.url.host == "storage.example":
            self.uploads.append(request.content)
            return httpx.Response(204)
        if request.url.host == "lobby.example":
            return self.handle_lobby(path, body, authorization)
        return self.handle_game(path, authorization)

    def handle_lobby(self, path: str, body: Any, authorization: str) -> httpx.Response:
        if path == "/lobby/v1/sessions":
            player = f"9a0c1e2d-3b4f-4a5b-8c6d-7e8f9a0b1c{len(self.players) + 1:02d}"
            self.players[f"credential-{player}"] = player
            self.names[player] = body["display_name"]
            return httpx.Response(
                201,
                json={
                    "player_id": player,
                    "access_token": f"credential-{player}",
                    "expires_at": "2026-11-24T00:00:00Z",
                },
            )
        player = self.players.get(authorization)
        if player is None:
            return self.problem(401, "unauthorized")
        if path == "/lobby/v1/player-tokens":
            assert body == {"game_key": "island-battles"}
            return httpx.Response(
                200,
                json={"token": f"token-{player}", "expires_at": "2099-01-01T00:00:00Z"},
            )
        if path == "/lobby/v1/lobbies":
            assert body["game_key"] == "island-battles"
            assert body["client_instance_id"]
            lobby = FakeLobby(len(self.lobbies) + 1)
            self.lobbies[lobby.id] = lobby
            lobby.make_teams(body["teams"])
            lobby.settings = body["game_settings"]
            self.join(lobby, player)
            return httpx.Response(201, json=lobby.snapshot())
        if path == "/lobby/v1/memberships":
            [lobby] = [
                lobby
                for lobby in self.lobbies.values()
                if lobby.join_code == body["join_code"]
            ]
            if len(lobby.members) >= len(lobby.teams):
                return self.problem(409, "lobby_full")
            self.join(lobby, player)
            return httpx.Response(201, json=lobby.snapshot())
        found = LOBBY_PATH.match(path)
        lobby = self.lobbies.get(found.group(1)) if found else None
        if found is None or lobby is None or player not in lobby.members:
            return self.problem(404, "not_found", path)
        rest, member = found.group(2), lobby.members[player]
        if rest == "/heartbeat":
            assert body["membership_generation"] == 1
            return httpx.Response(200, json={"version": 1, "membership_generation": 1})
        if rest == "/membership":
            self.left.append(player)
            del lobby.members[player]
            return httpx.Response(204)
        if rest == "/reopen":
            assert lobby.phase == "ended", "reopened while the game runs"
            lobby.phase, lobby.round = "waiting", lobby.round + 1
            for other in lobby.members.values():
                other["ready"] = False
            return httpx.Response(200, json=lobby.snapshot())
        if lobby.phase != "waiting":
            return self.problem(409, "phase_conflict")
        if rest == "/member-settings":
            bot = body["member_settings"]["bot"]
            if bot["kind"] == "uploaded" and self.packages.get(bot["package_id"]) != (
                f"{lobby.id}:{player}"
            ):
                return self.problem(422, "invalid_member_settings", "not your package")
            member["member_settings"], member["ready"] = body["member_settings"], False
            return httpx.Response(200, json=lobby.snapshot())
        if rest == "/team":
            team = body["team_id"]
            if team is not None and any(
                other["team_id"] == team for other in lobby.members.values()
            ):
                return self.problem(409, "team_full")
            member["team_id"], member["ready"] = team, False
            return httpx.Response(200, json=lobby.snapshot())
        if rest == "/ready":
            if member["team_id"] is None or member["member_settings"] is None:
                return self.problem(409, "not_ready")
            member["ready"] = body["ready"]
            return httpx.Response(200, json=lobby.snapshot())
        if rest == "/settings":
            assert body["game_settings"]["map"]["player_amount"] == len(body["teams"])
            lobby.make_teams(body["teams"])
            lobby.settings = body["game_settings"]
            for other in lobby.members.values():
                other["ready"] = False
            return httpx.Response(200, json=lobby.snapshot())
        if rest == "/start":
            if not all(other["ready"] for other in lobby.members.values()):
                return self.problem(409, "not_ready")
            if self.unavailable_starts:
                self.unavailable_starts -= 1
                return self.problem(502, "game_unavailable")
            if lobby.settings["map"]["map_name"] == self.refused_map:
                return self.problem(409, "game_requirement", "this map is refused")
            order = {team["id"]: team["order"] for team in lobby.teams}
            by_colour = sorted(
                lobby.members.values(), key=lambda other: order[other["team_id"]]
            )
            self.starts.append(
                {
                    "lobby": lobby.id,
                    "round": lobby.round,
                    "map": lobby.settings["map"]["map_name"],
                    "settings": {
                        key: value
                        for key, value in lobby.settings.items()
                        if key != "map"
                    },
                    "players": [other["display_name"] for other in by_colour],
                    "bots": [other["member_settings"]["bot"] for other in by_colour],
                }
            )
            lobby.phase, lobby.polls = "started", 0
            self.most_running = max(self.most_running, self.running())
            return httpx.Response(200, json=lobby.snapshot())
        return self.problem(404, "not_found", path)

    def handle_game(self, path: str, authorization: str) -> httpx.Response:
        # The game takes a player token, or an upload token for the upload.
        if authorization.startswith("ibut1."):
            player = player_of_upload(authorization)
        else:
            player = authorization.removeprefix("token-")
        found = GAME_PATH.match(path)
        lobby = self.lobbies.get(found.group(1)) if found else None
        if found is None or lobby is None:
            return httpx.Response(404, json={"detail": path})
        if player not in lobby.members:
            return httpx.Response(401, json={"detail": "unauthorized"})
        rest = found.group(2)
        if rest == "/bot-packages/upload-token":
            payload = json.dumps(
                {"lobby_id": lobby.id, "player_id": player, "exp": 2_000_000_000}
            )
            encoded = base64.urlsafe_b64encode(payload.encode()).decode().rstrip("=")
            return httpx.Response(
                200,
                json={
                    "token": f"ibut1.{encoded}.mac",
                    "expires_at": "2099-01-01T00:00:00Z",
                },
            )
        if rest == "/bot-packages":
            package = f"0b8f3f5e-3c1a-4d1e-9b7a-2f6c5d4e3a{len(self.packages):02d}"
            self.packages[package] = f"pending:{lobby.id}:{player}"
            return httpx.Response(
                201,
                json={
                    "package_id": package,
                    "upload": {
                        "url": "https://storage.example/packages",
                        "fields": {"key": "k"},
                    },
                },
            )
        if rest.startswith("/bot-packages/") and rest.endswith("/complete"):
            package = rest.split("/")[-2]
            self.packages[package] = self.packages[package].removeprefix("pending:")
            return httpx.Response(
                200,
                json={
                    "package_id": package,
                    "owner_player_id": player,
                    "owner_name": self.names[player],
                    "language": "python",
                    "entry_or_command": "bot:play",
                    "size_bytes": 4,
                    "uploaded_at": "2026-11-23T12:00:00Z",
                },
            )
        if rest == "/generation":
            assert lobby.phase in ("started", "ended")
            lobby.polls += 1
            state = {
                "game_id": 100 * len(self.lobbies) + lobby.round,
                "lobby_round": lobby.round,
                "turn_number": lobby.polls,
            }
            if lobby.polls >= self.polls_per_game:
                lobby.phase = "ended"
                # Colour 2 wins in round 1, colour 1 after; round 3 is a draw.
                winner = None if lobby.round == 3 else (2 if lobby.round == 1 else 1)
                state["outcome"] = {
                    "reason": "generation_cap",
                    "winner_team_color": winner,
                    "summary": f"round {lobby.round} ended",
                }
            return httpx.Response(200, json=state)
        if rest == "/demo":
            return httpx.Response(
                200, json={"format": "island-battles-demo", "lobby": lobby.id}
            )
        return httpx.Response(404, json={"detail": path})


def player_of_upload(token: str) -> str:
    payload = token.split(".")[1]
    return json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))[
        "player_id"
    ]


@pytest.fixture
def maps(tmp_path: Path) -> list[Path]:
    paths = []
    for name in ("strait", "crossing"):
        path = tmp_path / "maps" / f"{name}.json"
        path.parent.mkdir(exist_ok=True)
        path.write_text(json.dumps(circle_map(name)))
        paths.append(path)
    return paths


@pytest.fixture
def zip_file(tmp_path: Path) -> Path:
    path = tmp_path / "dist" / "island-battles-bot.zip"
    path.parent.mkdir()
    path.write_bytes(b"PK\x05\x06")
    return path


def make_match(
    client: httpx.Client,
    seats: list[Seat],
    output: Path,
    settings: dict | None = None,
    cancel: threading.Event | None = None,
    poll_seconds: float = 0,
) -> Match:
    return Match(
        Servers(LOBBY, GAME, client),
        seats,
        output,
        settings or {},
        cancel=cancel,
        poll_seconds=poll_seconds,
        retry_seconds=0,
        heartbeat_seconds=0.01,
        report=lambda _: None,
    )


def play(
    fake: FakeServers, seats: list[Seat], games, output: Path, settings=None
) -> list[dict]:
    with fake.client() as client:
        return make_match(client, seats, output, settings).play(games)


def test_seats_parse_with_their_names(zip_file: Path) -> None:
    assert parse_seat("hosted:https://bot.example.org/turn") == Seat(
        "bot.example.org", "hosted", "https://bot.example.org/turn"
    )
    assert parse_seat(f"package:{zip_file}") == Seat(
        "island-battles-bot.zip", "package", str(zip_file)
    )
    assert parse_seat("default:3") == Seat("level 3", "default", "3")
    assert parse_seat("rival=hosted:https://bot.example.org/turn?a=b").name == "rival"
    assert (
        parse_seat("hosted:https://bot.example.org/turn?a=b").value
        == "https://bot.example.org/turn?a=b"
    )
    for bad in (
        "bot.zip",
        "remote:https://x",
        "default:zero",
        "default:0",
        "hosted:not-a-url",
        "package:missing.zip",
    ):
        with pytest.raises(MatchError):
            parse_seat(bad)
    assert [
        seat.name
        for seat in unique_names(
            [parse_seat("default:1"), parse_seat("default:1"), parse_seat("default:2")]
        )
    ] == ["level 1 (1)", "level 1 (2)", "level 2"]


def test_the_schedule_plays_every_map_from_every_side(
    maps: list[Path], tmp_path: Path
) -> None:
    games = schedule(maps, 2, 1)
    assert [(game.id, game.seat_order) for game in games] == [
        ("strait-1", (0, 1)),
        ("strait-2", (1, 0)),
        ("crossing-1", (0, 1)),
        ("crossing-2", (1, 0)),
    ]
    assert [game.id for game in schedule(maps[:1], 2, 2)] == [
        "strait-1-1",
        "strait-2-1",
        "strait-1-2",
        "strait-2-2",
    ]
    three = tmp_path / "three.json"
    three.write_text(json.dumps(circle_map("three", 3)))
    assert [game.seat_order for game in schedule([three], 3, 1)] == [
        (0, 1, 2),
        (1, 2, 0),
        (2, 0, 1),
    ]
    with pytest.raises(MatchError, match="map for 3 teams, but there are 2 seats"):
        schedule([three], 2, 1)
    not_a_map = tmp_path / "notes.json"
    not_a_map.write_text("[]")
    with pytest.raises(MatchError, match="not a map file"):
        schedule([not_a_map], 2, 1)


def test_a_package_plays_a_hosted_bot_from_both_sides_and_each_game_is_saved(
    maps: list[Path], zip_file: Path, tmp_path: Path
) -> None:
    fake = FakeServers(unavailable_starts=1)
    seats = [
        parse_seat(f"mine=package:{zip_file}"),
        parse_seat("hosted:https://bot.example.org/turn"),
    ]
    output = tmp_path / "results"
    results = play(
        fake, seats, schedule(maps, 2, 1), output, {"turn_timeout_seconds": 10}
    )

    # One guest per seat, named after it; the package went up once, before
    # the first game, and the lobby was reopened for each later game.
    assert sorted(fake.names.values()) == ["bot.example.org", "mine"]
    [uploaded] = fake.uploads
    assert (
        b'filename="island-battles-bot.zip"' in uploaded and b"PK\x05\x06" in uploaded
    )
    assert [
        (start["round"], start["map"], start["players"]) for start in fake.starts
    ] == [
        (1, "strait", ["mine", "bot.example.org"]),
        (2, "strait", ["bot.example.org", "mine"]),
        (3, "crossing", ["mine", "bot.example.org"]),
        (4, "crossing", ["bot.example.org", "mine"]),
    ]
    assert all(
        start["settings"] == {"turn_timeout_seconds": 10} for start in fake.starts
    )
    [package] = fake.packages
    assert fake.starts[0]["bots"] == [
        {"kind": "uploaded", "package_id": package},
        {"kind": "hosted", "url": "https://bot.example.org/turn"},
    ]
    assert [
        (result["id"], result["winner"], result["lobby_round"]) for result in results
    ] == [
        ("strait-1", "bot.example.org", 1),
        ("strait-2", "bot.example.org", 2),
        ("crossing-1", None, 3),
        ("crossing-2", "bot.example.org", 4),
    ]
    first = results[0]
    assert first["players"] == ["mine", "bot.example.org"]
    assert first["outcome"]["reason"] == "generation_cap"
    assert first["generations"] == 2
    assert first["game_id"] == 101
    assert json.loads((output / "strait-1.json").read_text()) == first
    assert json.loads((output / "strait-1.demo.json").read_text()) == {
        "format": "island-battles-demo",
        "lobby": first["lobby_id"],
    }
    assert first["demo"] == str(output / "strait-1.demo.json")
    # Every guest left the lobby at the end.
    assert sorted(fake.left) == sorted(fake.players.values())
    # No request went anywhere else.
    assert {url.split("/")[2] for _, url in fake.requests} == {
        "lobby.example",
        "game.example",
        "storage.example",
    }


def test_a_refused_start_stops_the_match_and_the_guests_leave(
    maps: list[Path], tmp_path: Path
) -> None:
    fake = FakeServers(unavailable_starts=99)
    with pytest.raises(MatchError, match="game_unavailable"):
        play(
            fake,
            [parse_seat("default:1"), parse_seat("default:2")],
            schedule(maps, 2, 1),
            tmp_path,
        )
    assert len(fake.left) == 2


def test_the_command_prints_one_line_per_game(
    maps: list[Path],
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    capsys: pytest.CaptureFixture[str],
) -> None:
    fake = FakeServers(polls_per_game=1)
    real_client = httpx.Client

    def client(**arguments: Any) -> httpx.Client:
        return real_client(transport=httpx.MockTransport(fake), **arguments)

    monkeypatch.setattr("island_battles_client.match.httpx.Client", client)
    output = tmp_path / "out"
    status = cli.main(
        [
            "match",
            "--lobby-url",
            LOBBY,
            "--game-url",
            GAME,
            "--map",
            str(maps[0]),
            "--seat",
            "default:1",
            "--seat",
            "default:2",
            "--output",
            str(output),
            "--max-generations",
            "50",
        ]
    )
    captured = capsys.readouterr()
    assert status == 0, captured.err
    lines = [json.loads(line) for line in captured.out.splitlines()]
    assert [(line["id"], line["players"]) for line in lines] == [
        ("strait-1", ["level 1", "level 2"]),
        ("strait-2", ["level 2", "level 1"]),
    ]
    assert fake.starts[0]["settings"] == {"max_generations": 50}
    assert fake.starts[0]["bots"] == [
        {"kind": "default", "level": 1},
        {"kind": "default", "level": 2},
    ]
    assert sorted(path.name for path in output.iterdir()) == [
        "strait-1.demo.json",
        "strait-1.json",
        "strait-2.demo.json",
        "strait-2.json",
    ]

    assert (
        cli.main(
            [
                "match",
                "--lobby-url",
                LOBBY,
                "--game-url",
                GAME,
                "--map",
                str(maps[0]),
                "--seat",
                "default:1",
            ]
        )
        == 1
    )
    assert "at least two --seat" in capsys.readouterr().err


def play_in_parallel(
    fake: FakeServers,
    seats: list[Seat],
    games,
    output: Path,
    parallel: int,
    poll_seconds: float = 0,
) -> tuple[list[dict], list[MatchError], list[str]]:
    results: list[dict] = []
    reports: list[str] = []
    with fake.client() as client:
        failures = play_parallel(
            lambda cancel: make_match(
                client, seats, output, cancel=cancel, poll_seconds=poll_seconds
            ),
            games,
            parallel,
            results.append,
            reports.append,
        )
    return results, failures, reports


def test_parallel_games_play_in_lobbies_of_their_own(
    maps: list[Path], zip_file: Path, tmp_path: Path
) -> None:
    fake = FakeServers(polls_per_game=5)
    seats = [
        parse_seat(f"mine=package:{zip_file}"),
        parse_seat("hosted:https://bot.example.org/turn"),
    ]
    output = tmp_path / "results"
    results, failures, _ = play_in_parallel(
        fake, seats, schedule(maps, 2, 1), output, parallel=2, poll_seconds=0.01
    )
    assert failures == []
    # Two lobbies, each with its own two guests, each uploading the package
    # once; they played the four games between them, two at a time.
    assert len(fake.lobbies) == 2
    assert len(fake.players) == 4
    assert len(fake.uploads) == 2
    assert fake.most_running == 2
    assert sorted(result["id"] for result in results) == [
        "crossing-1",
        "crossing-2",
        "strait-1",
        "strait-2",
    ]
    assert len({result["lobby_id"] for result in results}) == 2
    for result in results:
        assert json.loads((output / f"{result['id']}.json").read_text()) == result
        demo = json.loads((output / f"{result['id']}.demo.json").read_text())
        assert demo["lobby"] == result["lobby_id"]
    by_id = {result["id"]: result["players"] for result in results}
    assert by_id["strait-1"] == ["mine", "bot.example.org"]
    assert by_id["strait-2"] == ["bot.example.org", "mine"]
    # Each lobby's package is its own guest's.
    for start in fake.starts:
        package = start["bots"][start["players"].index("mine")]["package_id"]
        assert fake.packages[package].startswith(start["lobby"])
    # Every guest left its lobby.
    assert sorted(fake.left) == sorted(fake.players.values())
    assert all(not lobby.members for lobby in fake.lobbies.values())


def test_a_failed_game_starts_no_more_but_lets_running_games_end(
    tmp_path: Path,
) -> None:
    good, bad = tmp_path / "good.json", tmp_path / "bad.json"
    good.write_text(json.dumps(circle_map("good")))
    bad.write_text(json.dumps(circle_map("bad")))
    fake = FakeServers(polls_per_game=5, refused_map="bad")
    # Two games on the good map start first; the bad map's are next.
    games = schedule([good, bad], 2, 1)
    results, failures, reports = play_in_parallel(
        fake,
        [parse_seat("default:1"), parse_seat("default:2")],
        games,
        tmp_path / "out",
        parallel=2,
        poll_seconds=0.01,
    )
    assert 1 <= len(failures) <= 2
    assert all("this map is refused" in str(failure) for failure in failures)
    assert any("no lobby starts another" in line for line in reports)
    # The good map's games, already running, ended and were saved.
    assert sorted(result["id"] for result in results) == ["good-1", "good-2"]
    assert (tmp_path / "out" / "good-2.demo.json").is_file()
    # No game was left running, and every guest left.
    assert all(lobby.phase != "started" for lobby in fake.lobbies.values())
    assert sorted(fake.left) == sorted(fake.players.values())


def test_a_cancelled_match_leaves_its_lobby_while_a_game_runs(
    maps: list[Path], tmp_path: Path
) -> None:
    fake = FakeServers(polls_per_game=10**9)
    cancel = threading.Event()
    threading.Timer(0.3, cancel.set).start()
    with fake.client() as client, pytest.raises(MatchCancelled):
        make_match(
            client,
            [parse_seat("default:1"), parse_seat("default:2")],
            tmp_path,
            cancel=cancel,
            poll_seconds=0.01,
        ).play(schedule(maps, 2, 1))
    assert len(fake.starts) == 1
    assert sorted(fake.left) == sorted(fake.players.values())


def test_ctrl_c_leaves_every_lobby(maps: list[Path], tmp_path: Path) -> None:
    fake = FakeServers(polls_per_game=10**9)

    def interrupt_once_both_run() -> None:
        while len(fake.starts) < 2:
            time.sleep(0.01)
        _thread.interrupt_main()

    threading.Thread(target=interrupt_once_both_run, daemon=True).start()
    with pytest.raises(KeyboardInterrupt):
        play_in_parallel(
            fake,
            [parse_seat("default:1"), parse_seat("default:2")],
            schedule(maps, 2, 1),
            tmp_path,
            parallel=3,
            poll_seconds=0.01,
        )
    # Two games had started; the third lobby, if it got that far, left too.
    assert len(fake.lobbies) >= 2
    assert sorted(fake.left) == sorted(fake.players.values())
    assert all(not lobby.members for lobby in fake.lobbies.values())


def test_the_command_takes_parallel(
    maps: list[Path],
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    capsys: pytest.CaptureFixture[str],
) -> None:
    fake = FakeServers(polls_per_game=2, refused_map="crossing")
    real_client = httpx.Client

    def client(**arguments: Any) -> httpx.Client:
        return real_client(transport=httpx.MockTransport(fake), **arguments)

    monkeypatch.setattr("island_battles_client.match.httpx.Client", client)
    arguments = [
        "match",
        "--lobby-url",
        LOBBY,
        "--game-url",
        GAME,
        "--seat",
        "default:1",
        "--seat",
        "default:2",
    ]
    status = cli.main(
        [
            *arguments,
            "--map",
            str(maps[0]),
            "--output",
            str(tmp_path),
            "--parallel",
            "2",
        ]
    )
    captured = capsys.readouterr()
    assert status == 0, captured.err
    # Whole lines, one per game, in either order.
    assert sorted(json.loads(line)["id"] for line in captured.out.splitlines()) == [
        "strait-1",
        "strait-2",
    ]
    assert len(fake.lobbies) == 2

    # A failure is reported, and the command fails.
    assert cli.main([*arguments, "--map", str(maps[1]), "--output", str(tmp_path)]) == 1
    assert "FAIL: the lobby refused" in capsys.readouterr().err

    assert cli.main([*arguments, "--map", str(maps[0]), "--parallel", "0"]) == 1
    assert "--parallel must be at least 1" in capsys.readouterr().err
