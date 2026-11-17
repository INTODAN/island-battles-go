"""Smoke test for a deployment: the game, the lobby and the VM together.

Run it with `scripts/smoke.sh`. The game is at its URL, which CloudFront serves
from the backend's Function URL (#75), and the lobby at its own, which
INTODAN/mpl deploys (#232). It checks, in order:

1. The game's `/healthz` returns 204, and with `--expected-version` its
   `X-Island-Battles-Version` header names that release.
2. The game's `/` redirects (307) to `/play/`, the 3D client.
3. The lobby's old routes on the game's URL say where it went: the lobby page
   redirects (301) to the same path and query on the lobby's URL, and the
   lobby's API answers 410 with a problem that names the lobby's URL.
4. A guest credential works at the lobby. The credential is kept in a session
   file, so a later run reuses it, which shows the lobby's database was
   carried over. A missing or expired credential is replaced by a new guest
   session.
5. The lobby issues a connection token, so it reaches its Postgres.
6. An authenticated GET reaches the lobby with its `Authorization` header.
   CloudFront strips the header from GET requests unless its origin request
   policy forwards it.
7. The lobby calls the game's hooks: it refuses a lobby whose map doesn't
   exist with the game's own answer, `invalid_game_settings`.
8. The game takes the lobby's player token on an authenticated GET, so the
   header reaches the backend, the backend reads the lobby's keys, and it
   reaches its own Postgres.
9. Centrifugo accepts the token on its public WebSocket URL, with a valid
   certificate and the game's URL as the browser origin.
10. Centrifugo subscribes the guest to its personal channel, so Centrifugo
    reaches the lobby's subscribe proxy.

With `--expected-web-bundle` (the entry script of the web client just
uploaded, such as `assets/index-1a2b3c4d.js`), it also checks that `/play/`
returns 200, that its `index.html` loads that script, that the script is
served (#114), and that `/play/config.json` names the lobby's URL. Without it,
the web client is not checked: nothing may be uploaded yet.
"""
from __future__ import annotations

import argparse
import asyncio
import datetime as dt
import json
import os
import re
import sys
import tempfile
import uuid
from pathlib import Path
from urllib.parse import urlsplit

import httpx
from websockets.asyncio.client import connect
from websockets.exceptions import WebSocketException

from web_client_page import web_bundle_referenced

# Guest sessions are kept for 24 hours. A credential this close to expiring is
# replaced, so it can't expire between the run before a move and the one after.
MINIMUM_REMAINING_SESSION = dt.timedelta(hours=2)
# Covers a Lambda cold start, and Centrifugo's subscribe proxy call to the
# lobby's.
HTTP_TIMEOUT_SECONDS = 30
# CloudFront finishes an invalidation a few seconds after the upload script
# creates it; a page cached before it can be served until then.
WEB_CLIENT_ATTEMPTS = 18
WEB_CLIENT_RETRY_SECONDS = 5
WEBSOCKET_TIMEOUT_SECONDS = 30


# What /healthz reports the binary's version in (backend/app/version.go).
VERSION_HEADER = "X-Island-Battles-Version"
# The game's key in the lobby.
GAME_KEY = "island-battles"
# Where the lobby serves its page, on its own URL.
LOBBY_PAGE = "/lobby/ui/"


class SmokeTestFailure(Exception):
    """A check failed. The message says which one and what to look at."""


def report(message: str) -> None:
    print(f"ok: {message}", flush=True)


def origin_of(url: str) -> str:
    parts = urlsplit(url)
    return f"{parts.scheme}://{parts.netloc}"


def load_session(session_file: Path) -> dict[str, str] | None:
    if not session_file.exists():
        return None
    session = json.loads(session_file.read_text(encoding="utf-8"))
    expires_at = dt.datetime.fromisoformat(session["expires_at"])
    if expires_at - dt.datetime.now(dt.UTC) < MINIMUM_REMAINING_SESSION:
        print(f"note: the saved guest credential expires at {session['expires_at']}; creating a new one", flush=True)
        return None
    return session


def save_session(session_file: Path, session: dict[str, str]) -> None:
    """Writes the credential readable only by its owner, replacing the old one atomically."""
    session_file.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_path = tempfile.mkstemp(dir=session_file.parent, prefix=".smoke-session-")
    with os.fdopen(descriptor, "w", encoding="utf-8") as temporary_file:
        json.dump(session, temporary_file)
    os.replace(temporary_path, session_file)


async def check_game(game: httpx.AsyncClient, lobby_origin: str, expected_version: str | None = None) -> None:
    """Checks the game's own name: its health, and where the lobby's old routes went."""
    health = await game.get("/healthz")
    if health.status_code != 204:
        raise SmokeTestFailure(f"/healthz returned {health.status_code}, expected 204")
    report("the backend answers /healthz")
    if expected_version:
        version = health.headers.get(VERSION_HEADER)
        if version != expected_version:
            raise SmokeTestFailure(f"/healthz reports version {version!r}, expected {expected_version!r}. The backend "
                                   "running is not the release's build.")
        report(f"the backend reports version {version}")

    root = await game.get("/")
    if root.status_code != 307 or root.headers.get("location") != "/play/":
        raise SmokeTestFailure(f"/ answered {root.status_code} to {root.headers.get('location')!r}, expected 307 to "
                               "/play/ from the backend (the web client itself is checked with "
                               "ISLAND_BATTLES_EXPECTED_WEB_BUNDLE).")
    report("/ redirects to /play/")

    moved = await game.get(f"{LOBBY_PAGE}?join=SMOKE")
    if moved.status_code != 301 or moved.headers.get("location") != f"{lobby_origin}{LOBBY_PAGE}?join=SMOKE":
        raise SmokeTestFailure(f"the old lobby page answered {moved.status_code} to {moved.headers.get('location')!r}, "
                               f"expected 301 to {lobby_origin}{LOBBY_PAGE}?join=SMOKE. Check the distribution's "
                               "lobby_redirect function.")
    report(f"the old lobby page redirects to {lobby_origin}{LOBBY_PAGE}")

    gone = await game.post("/lobby/v1/sessions", json={"display_name": "Smoke test"})
    if gone.status_code != 410 or gone.headers.get("content-type") != "application/problem+json" or \
            gone.json().get("lobby_url") != lobby_origin:
        raise SmokeTestFailure(f"the lobby's old API on the game's URL answered {gone.status_code}: {gone.text}. "
                               f"Expected 410 with a problem naming {lobby_origin}.")
    report("the lobby's old API on the game's URL answers 410 and names the lobby")


async def check_lobby(lobby: httpx.AsyncClient, session_file: Path) -> tuple[dict[str, str], dict[str, str]]:
    """Returns the guest session and a connection token for it, from the lobby."""
    session = load_session(session_file)
    reused_session = session is not None
    if session is None:
        response = await lobby.post("/lobby/v1/sessions", json={"display_name": "Smoke test"})
        if response.status_code != 201:
            raise SmokeTestFailure(f"creating a guest session returned {response.status_code}: {response.text}")
        session = {key: response.json()[key] for key in ("player_id", "access_token", "expires_at")}
        save_session(session_file, session)
        report(f"created guest {session['player_id']} and saved its credential in {session_file}")
    authorization = {"Authorization": f"Bearer {session['access_token']}"}

    response = await lobby.post("/lobby/v1/connection-token", json={}, headers=authorization)
    if response.status_code == 401 and reused_session:
        raise SmokeTestFailure(
            f"the lobby rejected the saved credential of guest {session['player_id']}. After moving the lobby or "
            "the VM, that means its database wasn't carried over. Otherwise delete the session file and run again.")
    if response.status_code != 200:
        raise SmokeTestFailure(f"getting a connection token returned {response.status_code}: {response.text}")
    if reused_session:
        report(f"the saved credential of guest {session['player_id']} still works")
    report("the lobby issued a connection token, so it reaches Postgres")
    connection_token = response.json()

    # A lobby that doesn't exist: the lobby answers 404 once it has
    # authenticated the guest, and 401 without the header.
    response = await lobby.get(f"/lobby/v1/games/{GAME_KEY}/lobbies/{uuid.uuid4()}", headers=authorization)
    if response.status_code == 401:
        raise SmokeTestFailure("an authenticated GET answered 401, so its Authorization header didn't reach the "
                               "lobby. Check its distribution's origin request policy.")
    if response.status_code != 404:
        raise SmokeTestFailure(f"getting a missing lobby returned {response.status_code}, expected 404: {response.text}")
    report("an authenticated GET reaches the lobby with its Authorization header")

    # A map that doesn't exist: the game refuses it in its validate-settings
    # hook, so the lobby creates nothing.
    response = await lobby.post("/lobby/v1/lobbies", headers=authorization, json={
        "request_id": str(uuid.uuid4()), "game_key": GAME_KEY, "name": "Smoke test",
        "teams": [{"name": "Red"}, {"name": "Blue"}], "client_instance_id": str(uuid.uuid4()),
        "game_settings": {"map_template_id": 999999999}})
    if response.status_code != 422 or "invalid_game_settings" not in response.text:
        raise SmokeTestFailure(f"creating a lobby with a missing map returned {response.status_code}, expected 422 "
                               f"invalid_game_settings from the game's hook: {response.text}. Check the lobby's "
                               "LOBBY_GAMES entry for the game and the hooks' shared secret.")
    report("the lobby calls the game's hooks, signed with their shared secret")
    return session, connection_token


async def check_player_token(lobby: httpx.AsyncClient, game: httpx.AsyncClient, session: dict[str, str]) -> None:
    """Checks that the game takes the lobby's player token."""
    response = await lobby.post("/lobby/v1/player-tokens", json={"game_key": GAME_KEY},
                                headers={"Authorization": f"Bearer {session['access_token']}"})
    if response.status_code != 200:
        raise SmokeTestFailure(f"getting a player token returned {response.status_code}: {response.text}")
    token = response.json()["token"]
    # A lobby without a game: the game answers 404 once it has verified the
    # token and looked for the game, and 401 for a token it can't verify.
    response = await game.get(f"/game/from-lobby/{uuid.uuid4()}/generation",
                              headers={"Authorization": f"Bearer {token}"})
    if response.status_code == 401:
        raise SmokeTestFailure("the game refused the lobby's player token. Check that its ISLAND_BATTLES_LOBBY_URL "
                               "is the lobby's URL, where it reads the lobby's keys, and that the distribution "
                               "forwards the Authorization header.")
    if response.status_code != 404:
        raise SmokeTestFailure(f"reading a lobby without a game returned {response.status_code}, expected 404: "
                               f"{response.text}")
    report("the game takes the lobby's player token and reaches its Postgres")


async def check_web_client(http: httpx.AsyncClient, expected_bundle: str, lobby_origin: str,
                           retry_seconds: float = WEB_CLIENT_RETRY_SECONDS) -> None:
    """Checks that /play/ serves the build whose entry script is expected_bundle, with the lobby's URL."""
    problem = ""
    for attempt in range(WEB_CLIENT_ATTEMPTS):
        if attempt:
            await asyncio.sleep(retry_seconds)
        page = await http.get("/play/")
        if page.status_code != 200:
            problem = f"/play/ returned {page.status_code}, expected 200"
        elif not web_bundle_referenced(page.text, expected_bundle):
            problem = (f"/play/ loads a different build than {expected_bundle}. CloudFront may still serve the "
                       "previous index.html from its cache, or the upload didn't happen")
        else:
            break
    else:
        raise SmokeTestFailure(problem)
    report(f"/play/ loads {expected_bundle}")

    bundle = await http.get(f"/play/{expected_bundle}")
    if bundle.status_code != 200:
        raise SmokeTestFailure(f"/play/{expected_bundle} returned {bundle.status_code}, expected 200")
    report("the web client's script is served")

    config = await http.get("/play/config.json")
    if config.status_code != 200 or config.json().get("lobby_url", "").rstrip("/") != lobby_origin:
        raise SmokeTestFailure(f"/play/config.json returned {config.status_code}: {config.text}. Expected its "
                               f"lobby_url to be {lobby_origin} (scripts/deploy-web-client.sh writes it).")
    report(f"the web client reaches the lobby at {lobby_origin}")


async def check_centrifugo(game_origin: str, session: dict[str, str], connection_token: dict[str, str]) -> None:
    websocket_url = connection_token["websocket_url"]
    if game_origin.startswith("https://") and not websocket_url.startswith("wss://"):
        raise SmokeTestFailure(f"browsers on {game_origin} can't open {websocket_url}; it must use wss://")
    channel = f"player:{session['player_id']}"
    # Centrifugo's JSON protocol: one command per message, and each reply
    # carries its command's id. An empty message is a ping, answered with one.
    async with connect(websocket_url, origin=game_origin, open_timeout=WEBSOCKET_TIMEOUT_SECONDS) as websocket:
        async def call(command_id: int, command: dict[str, object]) -> dict[str, object]:
            await websocket.send(json.dumps({"id": command_id, **command}))
            while True:
                message = await websocket.recv()
                for line in str(message).splitlines():
                    reply = json.loads(line)
                    if not reply:
                        await websocket.send("{}")
                    elif reply.get("id") == command_id:
                        return reply

        reply = await asyncio.wait_for(call(1, {"connect": {"token": connection_token["token"], "name": "smoke-test"}}),
                                       WEBSOCKET_TIMEOUT_SECONDS)
        if "error" in reply:
            raise SmokeTestFailure(f"Centrifugo refused the connection token: {reply['error']}. Check that the lobby "
                                   "and Centrifugo use the same connection token secret.")
        report(f"connected to {websocket_url} from origin {game_origin}")

        reply = await asyncio.wait_for(call(2, {"subscribe": {"channel": channel}}), WEBSOCKET_TIMEOUT_SECONDS)
        if "error" in reply:
            raise SmokeTestFailure(f"Centrifugo refused the subscription to {channel}: {reply['error']}. Check that "
                                   "its proxy endpoints call the lobby (INTODAN/mpl's centrifugo_proxy_url), with the "
                                   "lobby's proxy secret.")
        report(f"subscribed to {channel} through the lobby's subscribe proxy")


async def run(game_url: str, lobby_url: str, session_file: Path, expected_version: str | None = None,
              expected_web_bundle: str | None = None) -> None:
    game_origin, lobby_origin = origin_of(game_url), origin_of(lobby_url)
    async with httpx.AsyncClient(base_url=game_origin, timeout=HTTP_TIMEOUT_SECONDS) as game, \
            httpx.AsyncClient(base_url=lobby_origin, timeout=HTTP_TIMEOUT_SECONDS) as lobby:
        await check_game(game, lobby_origin, expected_version)
        session, connection_token = await check_lobby(lobby, session_file)
        await check_player_token(lobby, game, session)
        if expected_web_bundle:
            await check_web_client(game, expected_web_bundle, lobby_origin)
    await check_centrifugo(game_origin, session, connection_token)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--game-url", required=True, help="where players open the game, through CloudFront")
    parser.add_argument("--lobby-url", required=True, help="the lobby's URL, which INTODAN/mpl deploys")
    parser.add_argument("--session-file", type=Path, required=True,
                        help="where the guest credential is kept between runs")
    parser.add_argument("--expected-version", default=None,
                        help="fail unless /healthz reports this version, such as v0.1.0")
    parser.add_argument("--expected-web-bundle", default=None,
                        help="also fail unless /play/ loads this script, such as assets/index-1a2b3c4d.js")
    arguments = parser.parse_args()
    if arguments.expected_web_bundle and not re.fullmatch(r"assets/[\w.-]+\.js", arguments.expected_web_bundle):
        parser.error("--expected-web-bundle must look like assets/index-1a2b3c4d.js")
    try:
        asyncio.run(run(arguments.game_url, arguments.lobby_url, arguments.session_file, arguments.expected_version,
                        arguments.expected_web_bundle))
    except SmokeTestFailure as failure:
        print(f"FAILED: {failure}", file=sys.stderr)
        return 1
    except (httpx.HTTPError, WebSocketException, OSError, TimeoutError) as error:
        print(f"FAILED: {type(error).__name__}: {error}", file=sys.stderr)
        return 1
    print("Smoke test passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
