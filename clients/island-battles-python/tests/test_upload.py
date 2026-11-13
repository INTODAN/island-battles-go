"""``island-battles-bot upload``: a package zip to a lobby with an upload token."""

import base64
import json
from pathlib import Path

import httpx
import pytest

from island_battles_client import cli, upload
from island_battles_client.upload import UploadError

LOBBY = "6f1c2c9e-7d0a-4a43-9c55-6e3f0a1b2c3d"
PACKAGE = "0b8f3f5e-3c1a-4d1e-9b7a-2f6c5d4e3a21"
PLAYER = "9a0c1e2d-3b4f-4a5b-8c6d-7e8f9a0b1c2d"


def token(lobby: str = LOBBY) -> str:
    payload = json.dumps(
        {
            "lobby_id": lobby,
            "player_id": PLAYER,
            "exp": 2_000_000_000,
        }
    )
    encoded = base64.urlsafe_b64encode(payload.encode()).decode().rstrip("=")
    return f"ibut1.{encoded}.mac"


PACKAGE_VIEW = {
    "package_id": PACKAGE,
    "owner_player_id": PLAYER,
    "owner_name": "Alex",
    "language": "python",
    "entry_or_command": "bot:play",
    "size_bytes": 4,
    "uploaded_at": "2026-11-12T12:00:00Z",
}


class Game:
    """The game and the storage, as a mock transport records them."""

    def __init__(
        self,
        complete: httpx.Response | None = None,
        store: httpx.Response | None = None,
    ) -> None:
        self.requests: list[httpx.Request] = []
        self.complete = complete or httpx.Response(200, json=PACKAGE_VIEW)
        self.store = store or httpx.Response(204)

    def __call__(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        path = request.url.path
        if request.url.host == "storage.example" and path == "/packages":
            return self.store
        if path == f"/game/lobbies/{LOBBY}/bot-packages":
            return httpx.Response(
                201,
                json={
                    "package_id": PACKAGE,
                    "upload": {
                        "url": "https://storage.example/packages",
                        "fields": {"key": "lobbies/k.zip", "policy": "p"},
                    },
                },
            )
        if path == f"/game/lobbies/{LOBBY}/bot-packages/{PACKAGE}/complete":
            return self.complete
        return httpx.Response(404, json={"detail": "not found"})

    def client(self) -> httpx.Client:
        return httpx.Client(transport=httpx.MockTransport(self))


@pytest.fixture
def zip_file(tmp_path: Path) -> Path:
    path = tmp_path / "dist" / "island-battles-bot.zip"
    path.parent.mkdir()
    path.write_bytes(b"PK\x05\x06")
    return path


def test_upload_presigns_stores_and_completes(zip_file: Path) -> None:
    game = Game()
    package = upload.upload("https://game.example/", token(), zip_file, game.client())
    assert package.entry_or_command == "bot:play"
    start, store, complete = game.requests
    assert (start.method, str(start.url)) == (
        "POST",
        f"https://game.example/game/lobbies/{LOBBY}/bot-packages",
    )
    assert start.headers["Authorization"] == f"Bearer {token()}"
    assert json.loads(start.content) == {"file_name": "island-battles-bot.zip"}
    # The fields go first, unchanged, and the zip last, with its length.
    assert str(store.url) == "https://storage.example/packages"
    assert "Authorization" not in store.headers
    body = store.read()
    assert int(store.headers["Content-Length"]) == len(body)
    assert (
        body.index(b'name="key"')
        < body.index(b'name="policy"')
        < body.index(b'name="file"')
    )
    assert b"lobbies/k.zip" in body and b"PK\x05\x06" in body
    assert (
        str(complete.url)
        == f"https://game.example/game/lobbies/{LOBBY}/bot-packages/{PACKAGE}/complete"
    )
    assert complete.headers["Authorization"] == f"Bearer {token()}"


def test_the_servers_reason_for_a_bad_package_is_shown(zip_file: Path) -> None:
    refused = httpx.Response(
        422,
        json={
            "detail": 'the package was refused: the entry "bot" must be module:function.'
        },
    )
    with pytest.raises(UploadError, match='the entry "bot" must be module:function'):
        upload.upload(
            "https://game.example", token(), zip_file, Game(complete=refused).client()
        )


def test_storage_refusals_are_shown(zip_file: Path) -> None:
    too_large = httpx.Response(
        400,
        text="<Error><Code>EntityTooLarge</Code><Message>Your proposed upload exceeds the maximum allowed size</Message></Error>",
    )
    with pytest.raises(
        UploadError,
        match="storage refused the zip: EntityTooLarge: Your proposed upload",
    ):
        upload.upload(
            "https://game.example", token(), zip_file, Game(store=too_large).client()
        )


def test_files_are_checked_before_anything_is_sent(tmp_path: Path) -> None:
    game = Game()
    with pytest.raises(UploadError, match="is not a file"):
        upload.upload(
            "https://game.example", token(), tmp_path / "missing.zip", game.client()
        )
    empty = tmp_path / "empty.zip"
    empty.write_bytes(b"")
    with pytest.raises(UploadError, match="is empty"):
        upload.upload("https://game.example", token(), empty, game.client())
    with pytest.raises(UploadError, match="--game-url"):
        upload.upload("game.example", token(), empty, game.client())
    assert game.requests == []


def test_a_zip_over_250_mib_is_refused_before_sending(tmp_path: Path) -> None:
    large = tmp_path / "large.zip"
    with large.open("wb") as file:
        file.truncate(262_144_001)
    game = Game()
    with pytest.raises(UploadError, match="250.0 MiB"):
        upload.upload("https://game.example", token(), large, game.client())
    assert game.requests == []


@pytest.mark.parametrize(
    "bad",
    [
        "session-token",
        "ibst1.e30.mac",
        "ibut1.!!!.mac",
        "ibut1." + base64.urlsafe_b64encode(b"{}").decode() + ".mac",
    ],
)
def test_only_an_upload_token_names_a_lobby(bad: str) -> None:
    with pytest.raises(UploadError):
        upload.token_lobby(bad)


def test_the_token_names_its_lobby() -> None:
    assert upload.token_lobby(token("another")) == "another"


def test_an_expired_token_says_to_copy_a_new_command(zip_file: Path) -> None:
    class Expired(Game):
        def __call__(self, request: httpx.Request) -> httpx.Response:
            return httpx.Response(
                401,
                json={
                    "detail": "the upload token is not valid for this lobby, or has expired; copy a new command from the bot picker"
                },
            )

    with pytest.raises(UploadError, match="copy a new command"):
        upload.upload("https://game.example", token(), zip_file, Expired().client())


def test_the_command_line(
    zip_file: Path, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    game = Game()
    real_client = httpx.Client
    monkeypatch.setattr(
        upload.httpx,
        "Client",
        lambda **options: real_client(transport=httpx.MockTransport(game), **options),
    )
    status = cli.main(
        [
            "upload",
            "--game-url",
            "https://game.example",
            "--token",
            token(),
            str(zip_file),
        ]
    )
    assert status == 0
    assert capsys.readouterr().out.startswith(
        f"uploaded {zip_file}: python, entry bot:play, 0.0 MiB, package {PACKAGE}"
    )
    refused = Game(
        complete=httpx.Response(
            422, json={"detail": "the package was refused: no manifest."}
        )
    )
    monkeypatch.setattr(
        upload.httpx,
        "Client",
        lambda **options: real_client(
            transport=httpx.MockTransport(refused), **options
        ),
    )
    assert (
        cli.main(
            [
                "upload",
                "--game-url",
                "https://game.example",
                "--token",
                token(),
                str(zip_file),
            ]
        )
        == 1
    )
    assert capsys.readouterr().err == "FAIL: the package was refused: no manifest.\n"
