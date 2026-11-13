"""``island-battles-bot upload``: a package zip to a lobby, from the command line.

The bot picker's **Upload from the command line** gives a ready-to-copy
command with a short-lived upload token for the player and the lobby:

    island-battles-bot upload --game-url <url> --token=<token> dist/island-battles-bot.zip

The token stands in for the player's credentials. Uploading takes three requests:
the game presigns the upload, the zip goes straight to storage with that
presigned POST, and the game then checks the zip and adds it to the lobby's
packages, replacing the player's previous one.
"""

from __future__ import annotations

import argparse
import base64
import binascii
import json
import sys
from pathlib import Path
from urllib.parse import urlsplit

import httpx
from pydantic import ValidationError

from ._generated import BotPackage, BotPackageUpload
from .bundle import MAX_ZIP_BYTES, _mebibytes

TOKEN_PREFIX = "ibut1."
# The zip may take a while to send; the other requests are quick.
UPLOAD_TIMEOUT = httpx.Timeout(30.0, write=600.0, read=120.0)


class UploadError(Exception):
    """A reason the upload failed, in words for the person running it."""


def token_lobby(token: str) -> str:
    """The lobby an upload token is for, from its payload, which isn't secret."""
    parts = token.split(".")
    if not token.startswith(TOKEN_PREFIX) or len(parts) != 3:
        raise UploadError(
            "the token is not an upload token (ibut1.…); copy the command again "
            "from the bot picker's Upload from the command line"
        )
    try:
        payload = json.loads(
            base64.urlsafe_b64decode(parts[1] + "=" * (-len(parts[1]) % 4))
        )
    except (binascii.Error, ValueError) as error:
        raise UploadError(
            "the upload token is damaged; copy the command again"
        ) from error
    lobby = payload.get("lobby_id") if isinstance(payload, dict) else None
    if not isinstance(lobby, str) or not lobby:
        raise UploadError("the upload token names no lobby; copy the command again")
    return lobby


def _problem(response: httpx.Response) -> str:
    """The server's own words for a refused request."""
    try:
        body = response.json()
    except ValueError:
        body = None
    if isinstance(body, dict) and isinstance(body.get("detail"), str):
        return body["detail"]
    return f"HTTP {response.status_code}"


def _storage_problem(response: httpx.Response) -> str:
    """S3's own code and message for a refused upload, from its XML."""
    text = response.text
    found = []
    for tag in ("Code", "Message"):
        start, end = text.find(f"<{tag}>"), text.find(f"</{tag}>")
        if start != -1 and end > start:
            found.append(text[start + len(tag) + 2 : end])
    return ": ".join(found) or f"HTTP {response.status_code}"


def upload(game_url: str, token: str, path: Path, client: httpx.Client) -> BotPackage:
    """Upload the zip at ``path`` to the token's lobby and return the package."""
    parts = urlsplit(game_url)
    if parts.scheme not in ("http", "https") or not parts.netloc:
        raise UploadError(
            f"--game-url must be the game's address, such as https://island-battles.example.org, not {game_url!r}"
        )
    base = game_url.rstrip("/")
    lobby = token_lobby(token)
    if not path.is_file():
        raise UploadError(
            f"{path} is not a file; build it with `island-battles-bot package`"
        )
    size = path.stat().st_size
    if size == 0:
        raise UploadError(f"{path} is empty")
    if size > MAX_ZIP_BYTES:
        raise UploadError(
            f"{path} is {_mebibytes(size)}; the limit is {_mebibytes(MAX_ZIP_BYTES)}"
        )
    headers = {"Authorization": f"Bearer {token}", "Accept": "application/json"}
    packages = f"{base}/game/lobbies/{lobby}/bot-packages"
    try:
        started = client.post(packages, json={"file_name": path.name}, headers=headers)
        if started.status_code != 201:
            raise UploadError(f"the game refused the upload: {_problem(started)}")
        presigned = BotPackageUpload.model_validate(started.json())
        with path.open("rb") as file:
            stored = client.post(
                presigned.upload.url,
                data=presigned.upload.fields,
                files={"file": (path.name, file, "application/zip")},
                timeout=UPLOAD_TIMEOUT,
            )
        if not stored.is_success:
            raise UploadError(f"storage refused the zip: {_storage_problem(stored)}")
        completed = client.post(
            f"{packages}/{presigned.package_id}/complete", headers=headers
        )
        if completed.status_code != 200:
            raise UploadError(_problem(completed))
        return BotPackage.model_validate(completed.json())
    except httpx.HTTPError as error:
        raise UploadError(f"the upload failed: {error}") from error
    except (ValidationError, ValueError) as error:
        raise UploadError(f"the game answered something unexpected: {error}") from error


def describe(package: BotPackage) -> str:
    """One line about an uploaded package."""
    what = "entry" if package.language == "python" else "command"
    return (
        f"{package.language}, {what} {package.entry_or_command}, "
        f"{_mebibytes(package.size_bytes)}, package {package.package_id}"
    )


def run(args: argparse.Namespace) -> int:
    """The ``upload`` command."""
    path = Path(args.zip)
    try:
        with httpx.Client(timeout=30.0, follow_redirects=False) as client:
            package = upload(args.game_url, args.token, path, client)
    except UploadError as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1
    print(
        f"uploaded {path}: {describe(package)}. Everyone in the lobby sees it in the bot picker."
    )
    return 0
