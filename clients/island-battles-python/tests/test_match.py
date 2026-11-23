"""``island-battles-bot match`` against a fake lobby and game."""

import base64
import json
import threading
from pathlib import Path
from typing import Any

import httpx
import pytest

from island_battles_client import cli
from island_battles_client.match import (
    Match,
    MatchError,
    Seat,
    Servers,
    parse_seat,
    schedule,
    unique_names,
)

LOBBY = "https://lobby.example"
GAME = "https://game.example"
LOBBY_ID = "6f1c2c9e-7d0a-4a43-9c55-6e3f0a1b2c3d"
PREFIX = f"/lobby/v1/games/island-battles/lobbies/{LOBBY_ID}"


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


class FakeServers:
    """The lobby and the game, as much of them as match uses, with their rules
    on teams, readiness and rounds."""

    def __init__(self, unavailable_starts: int = 0, polls_per_game: int = 2) -> None:
        self.lock = threading.Lock()
        self.requests: list[tuple[str, str]] = []
        self.players: dict[str, str] = {}  # credential: player ID
        self.names: dict[str, str] = {}  # player ID: display name
        self.members: dict[str, dict[str, Any]] = {}  # player ID: member
        self.teams: list[dict[str, Any]] = []
        self.settings: dict[str, Any] = {}
        self.phase = "waiting"
        self.round = 1
        self.packages: dict[str, str] = {}  # package ID: owner
        self.uploads: list[bytes] = []
        self.unavailable_starts = unavailable_starts
        self.polls_per_game = polls_per_game
        self.polls = 0
        self.starts: list[dict[str, Any]] = []
        self.heartbeats = 0
        self.left: list[str] = []

    def client(self) -> httpx.Client:
        return httpx.Client(transport=httpx.MockTransport(self))

    def snapshot(self, player: str | None = None) -> dict[str, Any]:
        return {
            "lobby": {
                "id": LOBBY_ID,
                "join_code": "ABCD",
                "round": self.round,
                "phase": self.phase,
                "game_settings": self.settings,
            },
            "teams": [dict(team) for team in self.teams],
            "members": [dict(member) for member in self.members.values()],
            "membership_generation": 1,
        }

    @staticmethod
    def problem(status: int, code: str, detail: str = "") -> httpx.Response:
        return httpx.Response(
            status, json={"code": code, "detail": detail or code, "status": status}
        )

    def make_teams(self, definitions: list[dict[str, str]]) -> None:
        old = self.teams
        self.teams = [
            {
                "id": old[index]["id"] if index < len(old) else f"team-{index}",
                "name": item["name"],
                "order": index,
            }
            for index, item in enumerate(definitions)
        ]

    def join(self, player: str) -> None:
        self.members[player] = {
            "player_id": player,
            "display_name": self.names[player],
            "team_id": None,
            "ready": False,
            "member_settings": None,
            "kind": "human",
        }

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
            if path == "/lobby/v1/sessions":
                player = (
                    f"9a0c1e2d-3b4f-4a5b-8c6d-7e8f9a0b1c{len(self.players) + 1:02d}"
                )
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
                    json={
                        "token": f"token-{player}",
                        "expires_at": "2099-01-01T00:00:00Z",
                    },
                )
            if path == "/lobby/v1/lobbies":
                assert body["game_key"] == "island-battles"
                assert body["client_instance_id"]
                self.make_teams(body["teams"])
                self.settings = body["game_settings"]
                self.join(player)
                return httpx.Response(201, json=self.snapshot())
            if path == "/lobby/v1/memberships":
                assert body["join_code"] == "ABCD"
                if len(self.members) >= len(self.teams):
                    return self.problem(409, "lobby_full")
                self.join(player)
                return httpx.Response(201, json=self.snapshot())
            member = self.members.get(player)
            if member is None:
                return self.problem(404, "not_found")
            if path == f"{PREFIX}/heartbeat":
                assert body["membership_generation"] == 1
                self.heartbeats += 1
                return httpx.Response(
                    200, json={"version": 1, "membership_generation": 1}
                )
            if path == f"{PREFIX}/membership":
                self.left.append(player)
                del self.members[player]
                return httpx.Response(204)
            if path == f"{PREFIX}/reopen":
                assert self.phase == "ended", "reopened while the game runs"
                self.phase, self.round = "waiting", self.round + 1
                for other in self.members.values():
                    other["ready"] = False
                return httpx.Response(200, json=self.snapshot())
            if self.phase != "waiting":
                return self.problem(409, "phase_conflict")
            if path == f"{PREFIX}/member-settings":
                bot = body["member_settings"]["bot"]
                if (
                    bot["kind"] == "uploaded"
                    and self.packages.get(bot["package_id"]) != player
                ):
                    return self.problem(
                        422, "invalid_member_settings", "not your package"
                    )
                member["member_settings"], member["ready"] = (
                    body["member_settings"],
                    False,
                )
                return httpx.Response(200, json=self.snapshot())
            if path == f"{PREFIX}/team":
                team = body["team_id"]
                if team is not None and any(
                    other["team_id"] == team for other in self.members.values()
                ):
                    return self.problem(409, "team_full")
                member["team_id"], member["ready"] = team, False
                return httpx.Response(200, json=self.snapshot())
            if path == f"{PREFIX}/ready":
                if member["team_id"] is None or member["member_settings"] is None:
                    return self.problem(409, "not_ready")
                member["ready"] = body["ready"]
                return httpx.Response(200, json=self.snapshot())
            if path == f"{PREFIX}/settings":
                assert body["game_settings"]["map"]["player_amount"] == len(
                    body["teams"]
                )
                self.make_teams(body["teams"])
                self.settings = body["game_settings"]
                for other in self.members.values():
                    other["ready"] = False
                return httpx.Response(200, json=self.snapshot())
            if path == f"{PREFIX}/start":
                if not all(other["ready"] for other in self.members.values()):
                    return self.problem(409, "not_ready")
                if self.unavailable_starts:
                    self.unavailable_starts -= 1
                    return self.problem(502, "game_unavailable")
                order = {team["id"]: team["order"] for team in self.teams}
                by_colour = sorted(
                    self.members.values(), key=lambda other: order[other["team_id"]]
                )
                self.starts.append(
                    {
                        "round": self.round,
                        "map": self.settings["map"]["map_name"],
                        "settings": {
                            key: value
                            for key, value in self.settings.items()
                            if key != "map"
                        },
                        "players": [other["display_name"] for other in by_colour],
                        "bots": [
                            other["member_settings"]["bot"] for other in by_colour
                        ],
                    }
                )
                self.phase, self.polls = "started", 0
                return httpx.Response(200, json=self.snapshot())
            return self.problem(404, "not_found", path)
        # The game takes a player token, or an upload token for the upload.
        if authorization.startswith("ibut1."):
            player = player_of_upload(authorization)
        else:
            player = authorization.removeprefix("token-")
        if player not in self.members:
            return httpx.Response(401, json={"detail": "unauthorized"})
        packages = f"/game/lobbies/{LOBBY_ID}/bot-packages"
        if path == f"{packages}/upload-token":
            payload = json.dumps(
                {"lobby_id": LOBBY_ID, "player_id": player, "exp": 2_000_000_000}
            )
            encoded = base64.urlsafe_b64encode(payload.encode()).decode().rstrip("=")
            return httpx.Response(
                200,
                json={
                    "token": f"ibut1.{encoded}.mac",
                    "expires_at": "2099-01-01T00:00:00Z",
                },
            )
        if path == packages:
            package = f"0b8f3f5e-3c1a-4d1e-9b7a-2f6c5d4e3a2{len(self.packages)}"
            self.packages[package] = "pending:" + player
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
        if path.startswith(packages) and path.endswith("/complete"):
            package = path.split("/")[-2]
            owner = self.packages[package].removeprefix("pending:")
            self.packages[package] = owner
            return httpx.Response(
                200,
                json={
                    "package_id": package,
                    "owner_player_id": owner,
                    "owner_name": self.names[owner],
                    "language": "python",
                    "entry_or_command": "bot:play",
                    "size_bytes": 4,
                    "uploaded_at": "2026-11-23T12:00:00Z",
                },
            )
        if path == f"/game/from-lobby/{LOBBY_ID}/generation":
            assert self.phase in ("started", "ended")
            self.polls += 1
            state = {
                "game_id": 100 + self.round,
                "lobby_round": self.round,
                "turn_number": self.polls,
            }
            if self.polls >= self.polls_per_game:
                self.phase = "ended"
                # Colour 2 wins in round 1, colour 1 after; round 3 is a draw.
                winner = None if self.round == 3 else (2 if self.round == 1 else 1)
                state["outcome"] = {
                    "reason": "generation_cap",
                    "winner_team_color": winner,
                    "summary": f"round {self.round} ended",
                }
            return httpx.Response(200, json=state)
        if path == f"/game/from-lobby/{LOBBY_ID}/demo":
            return httpx.Response(
                200, json={"format": "island-battles-demo", "game_id": 100 + self.round}
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


def play(
    fake: FakeServers, seats: list[Seat], games, output: Path, settings=None
) -> list[dict]:
    with fake.client() as client:
        match = Match(
            Servers(LOBBY, GAME, client),
            seats,
            output,
            settings or {},
            sleep=lambda _: None,
            poll_seconds=0,
            heartbeat_seconds=0.01,
            report=lambda _: None,
        )
        return match.play(games)


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
    assert json.loads((output / "strait-1.demo.json").read_text())["game_id"] == 101
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
