"""The Python bot runner (#223): one turn of one uploaded Python bot.

A Lambda function on the ``python3.14`` runtime, handler ``runner.handler``,
standard library only. Each invocation gets a ``BotRunnerEvent``
(``backend/models/bot_runner.go``)::

    {"package": {"id": "…", "url": "<presigned GET>", "size_bytes": 123},
     "delivery": <TurnDelivery>,
     "limits": {"load_seconds": 10, "play_seconds": 2}}

1. An instance keeps at most one package loaded, under ``/tmp/bot/<id>``.
   Another package stops the worker, deletes ``/tmp/bot``, downloads the zip
   (at most 250 MiB), checks it as the upload does (``backend/botpackage``),
   unpacks it counting the bytes written (at most 1 GiB), and starts the
   worker of #216, ``python -m island_battles_client.worker <entry>`` with
   ``PYTHONPATH=<unpacked>/site``, waiting at most ``load_seconds`` in all
   for ``{"ready": true}``.
2. It writes ``{"turn": …}`` and waits at most ``play_seconds`` for one line.
3. It submits the answer to ``delivery.submit.url`` with the submit token.
   When the bot fails (an error, a timeout, a crash, or an answer the server
   rejects), it submits a pass instead: no commands, the turn's memory, and
   one ``error`` line from ``island_battles.bots`` that ends with the
   worker's last stderr lines, cut to 500 characters.
4. A worker that timed out or crashed is killed, and the next turn loads the
   package again.

The submit URL must be on ``GAME_SUBMIT_HOST``. The worker gets no AWS
credentials or other settings of the runner's: only a few plain variables.

``python runner.py --local …`` plays one turn of a local game instead
(``backend/README.md``, "Bot runners"). ``python runner.py --events`` is
the local server host's runner (#225): it takes one event per line on
stdin, like one Lambda instance, and answers each with its outcome.
"""

from __future__ import annotations

import argparse
import collections
import hashlib
import http.server
import json
import logging
import os
import queue
import re
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import zipfile
from pathlib import Path
from typing import Any

MANIFEST_NAME = "island-battles-bot.json"
PYTHON_VERSION = "3.14"
# The upload's limits (backend/botpackage), in binary units.
MAX_ZIP_BYTES = 250 * 1024 * 1024
MAX_UNPACKED_BYTES = 1024 * 1024 * 1024
MAX_LOG_MESSAGE = 500
BOTS_LOGGER = "island_battles.bots"
SUBMIT_USER_AGENT = "island-battles-bot-runner/1"
SUBMIT_ATTEMPTS = 3
SUBMIT_ATTEMPT_SECONDS = 5.0
# One answer line is at most 1000 commands, 100 log lines and 64 KiB of
# memory, well under this.
MAX_ANSWER_BYTES = 4 * 1024 * 1024
STDERR_TAIL_BYTES = 4096
EXIT_WAIT_SECONDS = 1.0
# The server's command rejections that pass the turn, besides 403 and 422,
# as for the SDKs' serve.
REJECTION_TYPES = {
    "urn:island-battles:problem:upgrade-max-level",
    "urn:island-battles:problem:insufficient-soldiers",
}
PACKAGE_ID = re.compile(r"[A-Za-z0-9-]{1,64}")

log = logging.getLogger("bot_runner")


class EventError(Exception):
    """An event that can't be played at all; nothing is submitted."""


class BotFailure(Exception):
    """Why the bot passes this turn, as its game log line says it."""


class Refused(Exception):
    """A package that the runner doesn't load."""


def truncate(message: str) -> str:
    """Cut a message to a game log line's 500 characters."""
    if len(message) <= MAX_LOG_MESSAGE:
        return message
    return message[: MAX_LOG_MESSAGE - 1] + "…"


def with_output(message: str, stderr: str) -> str:
    """The message with as much of the end of stderr as fits in the line."""
    stderr = stderr.strip()
    if not stderr:
        return truncate(message)
    if not message.endswith("."):
        message += "."
    separator = " Its last output: "
    room = MAX_LOG_MESSAGE - len(message) - len(separator)
    if room < 20:
        return truncate(message)
    if len(stderr) > room:
        stderr = "…" + stderr[len(stderr) - room + 1 :]
    return message + separator + stderr


def check_submit_url(submit_url: object, submit_host: str) -> None:
    """Refuse a submit URL that isn't https (or local http) on the game's host."""
    if not submit_host:
        raise EventError("GAME_SUBMIT_HOST is not set, so the runner submits nowhere")
    if not isinstance(submit_url, str):
        raise EventError("the delivery has no submit URL")
    try:
        parsed = urllib.parse.urlsplit(submit_url)
        hostname = (parsed.hostname or "").lower()
        parsed.port  # noqa: B018 - raises on a bad port
    except ValueError as error:
        raise EventError(f"the submit URL {submit_url!r} is not a URL") from error
    if "@" in parsed.netloc or not parsed.netloc or parsed.netloc.lower() != submit_host.lower():
        raise EventError(f"the submit URL {submit_url!r} is not on the game's host {submit_host}")
    if parsed.scheme == "https" or (parsed.scheme == "http" and hostname in ("localhost", "127.0.0.1")):
        return
    raise EventError(f"the submit URL {submit_url!r} must use https")


# Checking a package ------------------------------------------------------------


def check_path(original: str) -> None:
    """Refuse a name that leaves the package's folder, as backend/botpackage does."""
    name = original.removesuffix("/")
    if not name:
        raise Refused("the zip has a file without a name")
    if "\\" in name:
        raise Refused(f"{original} has a backslash; a package's paths use /")
    if name.startswith("/") or (len(name) > 1 and name[1] == ":"):
        raise Refused(f"{original} is an absolute path; every path must be inside the package")
    parts = name.split("/")
    if ".." in parts:
        raise Refused(f"{original} goes outside the package with ..; every path must be inside it")
    if any(part in ("", ".") for part in parts):
        raise Refused(f"{original} is not a plain relative path (it has an empty or . part)")


def check_entries(archive: zipfile.ZipFile) -> None:
    """Refuse unsafe paths, links and special files, and a declared size over 1 GiB."""
    declared = 0
    for info in archive.infolist():
        check_path(info.filename)
        mode = info.external_attr >> 16
        kind = stat.S_IFMT(mode)
        if kind == stat.S_IFLNK:
            raise Refused(f"{info.filename} is a symbolic link; a package may hold only files and folders")
        if kind not in (0, stat.S_IFREG, stat.S_IFDIR):
            raise Refused(f"{info.filename} is not a regular file; a package may hold only files and folders")
        declared += info.file_size
        if declared > MAX_UNPACKED_BYTES:
            raise Refused(f"the package is more than {MAX_UNPACKED_BYTES} bytes unpacked, the limit")


def check_manifest(data: bytes) -> str:
    """The entry of a valid Python manifest; refuse anything else."""
    try:
        manifest = json.loads(data)
    except ValueError as error:
        raise Refused(f"{MANIFEST_NAME} is not a valid manifest: {error}") from error
    if not isinstance(manifest, dict) or manifest.get("format") != 1:
        raise Refused(f'{MANIFEST_NAME} must have "format": 1')
    language = manifest.get("language")
    if language == "native":
        raise Refused("it is a native package, which the native runner plays, not this one")
    if language != "python":
        raise Refused(f'the language must be "python" or "native", not {language!r}')
    if manifest.get("python") != PYTHON_VERSION:
        raise Refused(f'a python manifest must have "python": "{PYTHON_VERSION}", the runners\' Python')
    entry = manifest.get("entry")
    module, separator, function = entry.partition(":") if isinstance(entry, str) else ("", "", "")
    if not separator or not all(name.isidentifier() for name in [*module.split("."), function]):
        raise Refused(f'the entry {entry!r} must be module:function, such as "bot:play"')
    return entry


def unpack(archive_path: Path, directory: Path) -> str:
    """Check the zip, unpack it counting the bytes written, and return its entry.

    The sizes in the zip come from the uploader, so the 1 GiB limit holds for
    the bytes actually written, whatever the zip declares.
    """
    try:
        archive = zipfile.ZipFile(archive_path)
    except (zipfile.BadZipFile, OSError) as error:
        raise Refused(f"the file is not a zip archive ({error})") from error
    with archive:
        check_entries(archive)
        try:
            manifest = archive.read(MANIFEST_NAME)
        except KeyError as error:
            raise Refused(f"the zip has no {MANIFEST_NAME} at its root") from error
        except (zipfile.BadZipFile, OSError, ValueError) as error:
            raise Refused(f"{MANIFEST_NAME} can't be read: {error}") from error
        entry = check_manifest(manifest)
        directory.mkdir(parents=True)
        root = directory.resolve()
        written = 0
        for info in archive.infolist():
            target = (directory / info.filename.removesuffix("/")).resolve()
            if root not in target.parents:
                raise Refused(f"{info.filename} is outside the package")
            if info.is_dir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            executable = (info.external_attr >> 16) & 0o111
            try:
                descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o755 if executable else 0o644)
                with os.fdopen(descriptor, "wb") as output, archive.open(info) as source:
                    while chunk := source.read(1024 * 1024):
                        written += len(chunk)
                        if written > MAX_UNPACKED_BYTES:
                            raise Refused(f"the package is more than {MAX_UNPACKED_BYTES} bytes unpacked, the limit")
                        output.write(chunk)
            except (zipfile.BadZipFile, OSError, ValueError, EOFError) as error:
                raise Refused(f"{info.filename} can't be unpacked: {error}") from error
    return entry


# The worker --------------------------------------------------------------------


class Worker:
    """A bot's worker process: JSON Lines over its stdin and stdout (#216)."""

    def __init__(self, package_id: str, arguments: list[str], directory: Path, environment: dict[str, str]):
        self.package_id = package_id
        self.process = subprocess.Popen(
            arguments,
            cwd=directory,
            env=environment,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            # Its own process group, so killing it kills what it started.
            start_new_session=True,
        )
        self.lines: queue.Queue[bytes | None] = queue.Queue()
        self._stderr = collections.deque[bytes]()
        self._stderr_size = 0
        self._stderr_lock = threading.Lock()
        self._stdout_reader = threading.Thread(target=self._read_stdout, daemon=True)
        self._stderr_reader = threading.Thread(target=self._read_stderr, daemon=True)
        self._stdout_reader.start()
        self._stderr_reader.start()

    def _read_stdout(self) -> None:
        assert self.process.stdout is not None
        while True:
            line = self.process.stdout.readline(MAX_ANSWER_BYTES + 1)
            if not line:
                break
            if len(line) > MAX_ANSWER_BYTES:
                self.lines.put(b"\0too long")
                break
            if line.strip():
                self.lines.put(line)
        self.lines.put(None)

    def _read_stderr(self) -> None:
        assert self.process.stderr is not None
        while chunk := self.process.stderr.read1(65536):
            with self._stderr_lock:
                self._stderr.append(chunk)
                self._stderr_size += len(chunk)
                while self._stderr_size - len(self._stderr[0]) >= STDERR_TAIL_BYTES:
                    self._stderr_size -= len(self._stderr.popleft())

    def stderr(self) -> str:
        """The end of what the worker wrote to stderr, from a whole line."""
        self._stderr_reader.join(timeout=0.2 if self.process.poll() is not None else 0)
        with self._stderr_lock:
            data = b"".join(self._stderr)
        cut = len(data) > STDERR_TAIL_BYTES
        text = data[-STDERR_TAIL_BYTES:].decode("utf-8", "replace")
        if cut and "\n" in text:
            text = text.split("\n", 1)[1]
        return text

    def running(self) -> bool:
        return self.process.poll() is None

    def kill(self) -> None:
        """Kill the worker's process group and wait for it."""
        try:
            os.killpg(self.process.pid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError):
            pass
        try:
            self.process.wait(timeout=2)
        except subprocess.TimeoutExpired:
            pass
        self._stdout_reader.join(timeout=EXIT_WAIT_SECONDS)
        self._stderr_reader.join(timeout=EXIT_WAIT_SECONDS)
        for stream in (self.process.stdin, self.process.stdout, self.process.stderr):
            try:
                if stream:
                    stream.close()
            except (OSError, ValueError):
                pass

    def ended(self) -> str:
        """How a worker whose output ended exited."""
        try:
            status = self.process.wait(timeout=EXIT_WAIT_SECONDS)
        except subprocess.TimeoutExpired:
            return "closed its output"
        self._stderr_reader.join(timeout=EXIT_WAIT_SECONDS)
        if status == 0:
            return "exited"
        if status < 0:
            return f"stopped (signal {signal.Signals(-status).name})"
        return f"stopped (exit status {status})"

    def _next_line(self, timeout: float) -> bytes | None:
        """The next output line, None at its end; raises queue.Empty on timeout."""
        return self.lines.get(timeout=max(timeout, 0))

    def wait_ready(self, deadline: float) -> None:
        """Wait for ``{"ready": true}`` until the deadline (time.monotonic())."""
        try:
            line = self._next_line(deadline - time.monotonic())
        except queue.Empty:
            raise BotFailure("it was not ready within the time to load it") from None
        if line is None:
            raise BotFailure(f"its worker {self.ended()} before it was ready")
        try:
            answer = json.loads(line)
        except ValueError:
            answer = None
        if isinstance(answer, dict) and isinstance(answer.get("error"), str):
            raise BotFailure(answer["error"])
        if not isinstance(answer, dict) or answer.get("ready") is not True:
            raise BotFailure(f'its worker wrote {first_characters(line)!r} instead of {{"ready": true}}')

    def play(self, turn: Any, limit: float) -> Any:
        """Play the turn; return the result, or raise.

        ``BotAnswerError`` is a turn the bot failed, with the worker still
        running. ``BotFailure`` is a worker that failed; the caller kills it.
        """
        request = (json.dumps({"turn": turn}, ensure_ascii=False, separators=(",", ":")) + "\n").encode()
        # The write blocks while the worker doesn't read; killing it ends it.
        threading.Thread(target=self._write, args=(request,), daemon=True).start()
        try:
            line = self._next_line(limit)
        except queue.Empty:
            raise BotFailure(f"did not answer within {limit:g} seconds") from None
        if line is None:
            raise BotFailure(self.ended())
        if line.startswith(b"\0"):
            raise BotFailure("broke the worker protocol: it wrote a line longer than 4 MiB")
        try:
            answer = json.loads(line)
        except ValueError:
            answer = None
        if isinstance(answer, dict) and isinstance(answer.get("error"), str):
            raise BotAnswerError(answer["error"])
        if not isinstance(answer, dict) or not isinstance(answer.get("result"), dict):
            raise BotFailure(f"broke the worker protocol: it wrote {first_characters(line)!r}")
        return answer["result"]

    def _write(self, request: bytes) -> None:
        try:
            assert self.process.stdin is not None
            self.process.stdin.write(request)
            self.process.stdin.flush()
        except (OSError, ValueError):
            pass


class BotAnswerError(Exception):
    """The worker answered ``{"error": …}``: the bot failed, the worker is fine."""


def first_characters(data: bytes) -> str:
    text = data.decode("utf-8", "replace").strip()
    return text if len(text) <= 80 else text[:79] + "…"


def worker_environment(site: Path) -> dict[str, str]:
    """The only environment a worker gets: none of the runner's settings.

    The runtime's library path is kept: Lambda's Python may find its own
    shared library through it.
    """
    temporary = tempfile.gettempdir()
    environment = {
        "PATH": "/usr/local/bin:/usr/bin:/bin",
        "HOME": temporary,
        "TMPDIR": temporary,
        "LANG": "C.UTF-8",
        "PYTHONPATH": str(site),
    }
    if "LD_LIBRARY_PATH" in os.environ:
        environment["LD_LIBRARY_PATH"] = os.environ["LD_LIBRARY_PATH"]
    return environment


# The runner --------------------------------------------------------------------


class Runner:
    """Plays one turn per event, keeping at most one package loaded."""

    def __init__(
        self,
        directory: Path,
        submit_host: str,
        python: str = sys.executable,
        allow_http_packages: bool = False,
    ):
        self.directory = directory
        self.submit_host = submit_host
        self.python = python
        self.allow_http_packages = allow_http_packages
        self.loaded: Worker | None = None
        self._opener = urllib.request.build_opener(NoRedirects)

    def handle(self, event: Any, deadline: float) -> dict[str, Any]:
        """Play the event's turn and submit it, or a pass; ``deadline`` is time.monotonic()."""
        if not isinstance(event, dict) or not isinstance(event.get("delivery"), dict):
            raise EventError("the event has no delivery")
        delivery = event["delivery"]
        submit = delivery.get("submit")
        if not isinstance(submit, dict):
            raise EventError("the delivery has no submit")
        check_submit_url(submit.get("url"), self.submit_host)
        turn = delivery.get("turn")
        if not isinstance(turn, dict) or not turn.get("game_id") or not (turn.get("generation") or {}).get("id"):
            raise EventError("the delivery has no turn")
        if "hosted" in event:
            raise EventError("hosted bots are delivered by the native runner")
        package, limits = event.get("package"), event.get("limits")
        if not isinstance(package, dict):
            raise EventError("the event has no package")
        if (
            not isinstance(limits, dict)
            or not _positive(limits.get("load_seconds"))
            or not _positive(limits.get("play_seconds"))
        ):
            raise EventError("a package's event needs positive limits.load_seconds and limits.play_seconds")
        try:
            result = self.play(package, limits, turn)
        except BotFailure as failure:
            return self.pass_turn(delivery, str(failure), deadline)
        status, problem = self.submit(delivery, result, deadline)
        if rejected(status, problem):
            return self.pass_turn(
                delivery,
                "The server rejected the uploaded bot's answer, so it passes this turn: "
                + str(problem.get("detail", "")),
                deadline,
            )
        return outcome("submitted", status, problem)

    def play(self, package: dict[str, Any], limits: dict[str, Any], turn: Any) -> Any:
        """The worker's result for the turn, loading the package first if needed."""
        if self.loaded is not None and (self.loaded.package_id != package.get("id") or not self.loaded.running()):
            self.unload()
        if self.loaded is None:
            load_deadline = time.monotonic() + float(limits["load_seconds"])
            try:
                self.loaded = self.load(package, load_deadline)
            except LoadFailure as failure:
                self.unload()
                raise BotFailure(
                    with_output(
                        f"The uploaded bot could not be loaded, so it passes this turn: {failure.reason}",
                        failure.stderr,
                    )
                ) from None
        try:
            return self.loaded.play(turn, float(limits["play_seconds"]))
        except BotAnswerError as error:
            raise BotFailure(f"The uploaded bot failed this turn, so it passes: {error}") from None
        except BotFailure as failure:
            stderr = self.loaded.stderr()
            self.unload()
            raise BotFailure(
                with_output(f"The uploaded bot {failure}, so it passes this turn and is restarted next turn.", stderr)
            ) from None

    def unload(self) -> None:
        """Stop the worker, if any, and delete the loaded package."""
        if self.loaded is not None:
            self.loaded.kill()
            self.loaded = None
        shutil.rmtree(self.directory, ignore_errors=True)

    def load(self, package: dict[str, Any], deadline: float) -> Worker:
        package_id = package.get("id")
        if not isinstance(package_id, str) or not PACKAGE_ID.fullmatch(package_id):
            raise LoadFailure(f"the package ID {package_id!r} is not a plain ID")
        size = package.get("size_bytes")
        if isinstance(size, int) and size > MAX_ZIP_BYTES:
            raise LoadFailure(f"the package is {size} bytes; the limit is {MAX_ZIP_BYTES}")
        archive_path = self.directory / f"{package_id}.zip"
        directory = self.directory / package_id
        try:
            # Nothing is loaded now, but an earlier runtime of this instance,
            # such as one that timed out, may have left a package in /tmp.
            shutil.rmtree(self.directory, ignore_errors=True)
            self.directory.mkdir(mode=0o700, parents=True, exist_ok=True)
            self.download(package, archive_path, deadline)
            entry = unpack(archive_path, directory)
        except Refused as refused:
            raise LoadFailure(str(refused)) from None
        except OSError as error:
            raise LoadFailure(f"it could not be unpacked: {error}") from None
        finally:
            archive_path.unlink(missing_ok=True)
        if time.monotonic() >= deadline:
            raise LoadFailure("it was not ready within the time to load it")
        # Not `python -I`: it would ignore PYTHONPATH, and the SDK's worker
        # is inside site/ (see #223). The environment is the worker's own.
        try:
            worker = Worker(
                package_id,
                [self.python, "-m", "island_battles_client.worker", entry],
                directory,
                worker_environment(directory / "site"),
            )
        except OSError as error:
            raise LoadFailure(f"its worker could not start: {error}") from None
        try:
            worker.wait_ready(deadline)
        except BotFailure as failure:
            stderr = worker.stderr()
            worker.kill()
            raise LoadFailure(str(failure), stderr) from None
        return worker

    def download(self, package: dict[str, Any], path: Path, deadline: float) -> None:
        """Write the zip to path: at most 250 MiB, and the size it was uploaded with."""
        url = package.get("url")
        scheme = urllib.parse.urlsplit(url).scheme if isinstance(url, str) else ""
        if scheme != "https" and not (self.allow_http_packages and scheme == "http"):
            raise Refused("the package's URL is not an https URL")
        timeout = deadline - time.monotonic()
        if timeout <= 0:
            raise Refused("it was not ready within the time to load it")
        try:
            with self._opener.open(urllib.request.Request(url), timeout=timeout) as response:
                if response.status != 200:
                    raise Refused(f"its download answered {response.status}")
                length = response.headers.get("Content-Length")
                if length and length.isdigit() and int(length) > MAX_ZIP_BYTES:
                    raise Refused(f"the package is {length} bytes; the limit is {MAX_ZIP_BYTES}")
                written = 0
                with path.open("wb") as output:
                    while chunk := response.read(1024 * 1024):
                        written += len(chunk)
                        if written > MAX_ZIP_BYTES:
                            raise Refused(f"the package is more than {MAX_ZIP_BYTES} bytes, the limit")
                        if time.monotonic() > deadline:
                            raise Refused("its download took longer than the time to load it")
                        output.write(chunk)
        except urllib.error.HTTPError as error:
            error.close()
            raise Refused(f"its download answered {error.code}") from None
        except (urllib.error.URLError, OSError) as error:
            # The URL is presigned: keep its signature out of the game log.
            reason = getattr(error, "reason", error)
            raise Refused(f"its download failed: {reason}") from None
        expected = package.get("size_bytes")
        if isinstance(expected, int) and expected > 0 and written != expected:
            raise Refused(f"the package's zip is {written} bytes, not the {expected} it was uploaded with")

    def pass_turn(self, delivery: dict[str, Any], reason: str, deadline: float) -> dict[str, Any]:
        """Submit a pass: no commands, the turn's memory, one error line."""
        reason = truncate(reason)
        turn = delivery["turn"]
        log.warning(
            "An uploaded bot passes its turn: game %s, generation %s: %s",
            turn.get("game_id"),
            turn.get("generation", {}).get("id"),
            reason,
        )
        result = {
            "commands": [],
            "logs": [{"level": "error", "logger": BOTS_LOGGER, "message": reason}],
            "memory": turn.get("memory"),
        }
        status, problem = self.submit(delivery, result, deadline)
        answer = outcome("passed", status, problem)
        if answer["outcome"] == "passed":
            answer["reason"] = reason
        return answer

    def submit(self, delivery: dict[str, Any], result: dict[str, Any], deadline: float) -> tuple[int, dict[str, Any]]:
        """Send the result as the turn's completing batch, again after a server error."""
        turn, submit = delivery["turn"], delivery["submit"]
        commands = result.get("commands")
        logs = result.get("logs")
        body = json.dumps(
            {
                "game_id": turn["game_id"],
                "generation_id": turn["generation"]["id"],
                "request_id": str(uuid.uuid4()),
                "complete": True,
                "commands": [] if commands is None else commands,
                "logs": [] if logs is None else logs,
                "memory": result.get("memory"),
            },
            ensure_ascii=False,
        ).encode()
        status, problem = 0, {}
        for attempt in range(SUBMIT_ATTEMPTS):
            if attempt:
                pause = 0.25 * 2**attempt
                if time.monotonic() + pause >= deadline:
                    break
                time.sleep(pause)
            status, problem = self._post(submit, body, deadline)
            if status and status < 500:
                break
        return status, problem

    def _post(self, submit: dict[str, Any], body: bytes, deadline: float) -> tuple[int, dict[str, Any]]:
        request = urllib.request.Request(
            submit["url"],
            data=body,
            method="POST",
            headers={
                "Content-Type": "application/json",
                "User-Agent": SUBMIT_USER_AGENT,
                "Authorization": f"Bearer {submit.get('token', '')}",
            },
        )
        timeout = max(min(SUBMIT_ATTEMPT_SECONDS, deadline - time.monotonic()), 0.5)
        try:
            with self._opener.open(request, timeout=timeout) as response:
                return response.status, read_problem(response.read(1 << 20))
        except urllib.error.HTTPError as error:
            with error:
                return error.code, read_problem(error.read(1 << 20))
        except (urllib.error.URLError, OSError) as error:
            return 0, {"detail": str(getattr(error, "reason", error))}


class LoadFailure(Exception):
    """A package that didn't load, with its worker's stderr if it started."""

    def __init__(self, reason: str, stderr: str = ""):
        super().__init__(reason)
        self.reason = reason
        self.stderr = stderr


class NoRedirects(urllib.request.HTTPRedirectHandler):
    """Never follow a redirect: the runner posts only where it checked."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):  # noqa: ANN001, ANN201, D102
        return None


def _positive(value: object) -> bool:
    return isinstance(value, int | float) and not isinstance(value, bool) and value > 0


def read_problem(data: bytes) -> dict[str, Any]:
    try:
        problem = json.loads(data)
    except ValueError:
        return {}
    return problem if isinstance(problem, dict) else {}


def rejected(status: int, problem: dict[str, Any]) -> bool:
    return status in (403, 422) or (status == 409 and problem.get("type") in REJECTION_TYPES)


def outcome(kind: str, status: int, problem: dict[str, Any]) -> dict[str, Any]:
    if not 200 <= status <= 299:
        return {"outcome": "unsubmitted", "status": status, "reason": str(problem.get("detail", ""))}
    return {"outcome": kind, "status": status}


# Lambda ------------------------------------------------------------------------

_runner: Runner | None = None


def handler(event: Any, context: Any) -> dict[str, Any]:
    """The Lambda handler, ``runner.handler``."""
    global _runner
    if _runner is None:
        logging.getLogger().setLevel(logging.WARNING)
        _runner = Runner(Path("/tmp/bot"), os.environ.get("GAME_SUBMIT_HOST", ""))
    deadline = time.monotonic() + context.get_remaining_time_in_millis() / 1000 - 0.5
    return _runner.handle(event, deadline)


# Local runs --------------------------------------------------------------------


def read_delivery(game_url: str, lobby_id: str, token: str) -> Any:
    """The player's turn of the lobby's game, as the backend delivers it."""
    request = urllib.request.Request(
        f"{game_url}/game/from-lobby/{urllib.parse.quote(lobby_id, safe='')}/turn-delivery",
        headers={"Authorization": f"Bearer {token}"},
    )
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        raise SystemExit(
            f"FAIL: the turn delivery answered {error.code}: {error.read(4096).decode(errors='replace')}"
        ) from None


def serve_zip(data: bytes) -> http.server.ThreadingHTTPServer:
    """Serve the zip on 127.0.0.1, as S3 serves it to the runner on Lambda."""

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self) -> None:  # noqa: N802
            self.send_response(200)
            self.send_header("Content-Type", "application/zip")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, *arguments: object) -> None:
            pass

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server


def serve_events(lines: Any, output: Any, submit_host: str, timeout: float = 20) -> None:
    """Handle one JSON event per line, as one Lambda instance handles its
    invocations, keeping the loaded package between them, and write one JSON
    line for each: the outcome, or ``{"error": …}`` for an event that can't be
    played. The local server host runs this when no runner functions are
    configured, with packages it serves itself over http."""
    directory = Path(tempfile.mkdtemp(prefix="bot-runner-python-"))
    events_runner = Runner(directory, submit_host, allow_http_packages=True)
    try:
        for line in lines:
            if not line.strip():
                continue
            try:
                answer = events_runner.handle(json.loads(line), time.monotonic() + timeout)
            except (EventError, ValueError) as error:
                answer = {"error": str(error)}
            output.write(json.dumps(answer) + "\n")
            output.flush()
    finally:
        events_runner.unload()


def main(arguments: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="runner.py",
        description="Plays the player's current turn of a local game with a package, as the "
        "runner Lambda does, and submits it. The package must run on this machine. With --events, "
        "plays the events on stdin instead, one per line, for the local server host.",
    )
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--local", action="store_true", help="play one turn of a local game")
    mode.add_argument(
        "--events", action="store_true", help="play the events on stdin, one per line; GAME_SUBMIT_HOST is required"
    )
    parser.add_argument("--package", type=Path, help="the package's zip")
    parser.add_argument("--game-url", default="http://localhost:8888", help="the game's URL")
    parser.add_argument("--lobby-id", help="the lobby whose game to play")
    parser.add_argument("--token", help="a player token of a player in the game")
    parser.add_argument("--load-seconds", type=float, default=10)
    parser.add_argument("--play-seconds", type=float, default=2)
    options = parser.parse_args(arguments)
    if options.events:
        logging.basicConfig(level=logging.WARNING, format="%(levelname)s %(message)s")
        serve_events(sys.stdin, sys.stdout, os.environ.get("GAME_SUBMIT_HOST", ""))
        return 0
    if options.package is None or options.lobby_id is None or options.token is None:
        parser.error("--local needs --package, --lobby-id and --token")
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(message)s")
    game_url = options.game_url.rstrip("/")
    data = options.package.read_bytes()
    delivery = read_delivery(game_url, options.lobby_id, options.token)
    server = serve_zip(data)
    directory = Path(tempfile.mkdtemp(prefix="bot-runner-python-"))
    runner = Runner(
        directory,
        os.environ.get("GAME_SUBMIT_HOST") or urllib.parse.urlsplit(game_url).netloc,
        allow_http_packages=True,
    )
    event = {
        "package": {
            "id": "local-" + hashlib.sha256(data).hexdigest()[:16],
            "url": f"http://127.0.0.1:{server.server_address[1]}/package.zip",
            "size_bytes": len(data),
        },
        "delivery": delivery,
        "limits": {"load_seconds": options.load_seconds, "play_seconds": options.play_seconds},
    }
    try:
        print(json.dumps(runner.handle(event, time.monotonic() + 20)))
    except EventError as error:
        print(f"FAIL: {error}", file=sys.stderr)
        return 1
    finally:
        runner.unload()
        server.shutdown()
    return 0


if __name__ == "__main__":
    sys.exit(main())
