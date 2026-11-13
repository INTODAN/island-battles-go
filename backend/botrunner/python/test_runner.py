"""Unit tests for the Python bot runner, with fake packages.

Run from the repository root, with Python 3.14 like the runtime:

    uv run --no-project --python 3.14 python -I -m unittest discover -s backend/botrunner/python

The fake packages hold a fake ``island_battles_client.worker`` that speaks the
worker protocol of #216 with the standard library only; the module part of
its entry says how it behaves. ``test_end_to_end.py`` runs a real package.
"""

from __future__ import annotations

import copy
import io
import json
import os
import re
import stat
import tempfile
import threading
import time
import unittest
import zipfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from unittest import mock

import runner

EXAMPLE_EVENT = Path(__file__).resolve().parent.parent / "testdata" / "event.json"

FAKE_WORKER = r"""
import json, os, sys, time

mode = sys.argv[1].partition(":")[0]
print(f"pid={os.getpid()} {mode}", file=sys.stderr, flush=True)
if mode == "slowload":
    time.sleep(10)
if mode == "noimport":
    print(json.dumps({"error": "could not load noimport:play:\nModuleNotFoundError: No module named 'noimport'"}), flush=True)
    sys.exit(1)
print('{"ready": true}', flush=True)
for line in sys.stdin:
    request = json.loads(line)
    if mode == "slowplay":
        time.sleep(10)
    if mode == "crash":
        print("Traceback (most recent call last):\nZeroDivisionError: division by zero", file=sys.stderr, flush=True)
        sys.exit(3)
    if mode == "noisycrash":
        for number in range(200):
            print(f"line {number} of the bot's output", file=sys.stderr)
        sys.exit(1)
    if mode == "failing":
        print(json.dumps({"error": "ValueError: no castles"}), flush=True)
        continue
    memory = {
        "pid": os.getpid(),
        "pythonpath": os.environ.get("PYTHONPATH"),
        "secret": os.environ.get("AWS_SECRET_ACCESS_KEY"),
        "generation": request["turn"]["generation"]["id"],
    }
    command = {"command_type": "send_soldier_group", "parameters": {"origin_castle_id": 0, "destination_castle_id": 1, "soldier_amount": 1}}
    log = {"level": "info", "logger": "island_battles.player", "message": "sent one"}
    print(json.dumps({"result": {"commands": [command], "logs": [log], "memory": memory}}), flush=True)
"""


def zip_bytes(entries: list[tuple[str, int, bytes]]) -> bytes:
    """A zip of (name, mode, data) entries; mode carries the file type."""
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as archive:
        for name, mode, data in entries:
            info = zipfile.ZipInfo(name)
            info.external_attr = mode << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(info, data)
    return buffer.getvalue()


def manifest(entry: str) -> bytes:
    return json.dumps({"format": 1, "language": "python", "python": "3.14", "entry": entry}).encode()


def fake_package(mode: str, *extra: tuple[str, int, bytes]) -> bytes:
    return zip_bytes(
        [
            (runner.MANIFEST_NAME, stat.S_IFREG | 0o644, manifest(f"{mode}:play")),
            ("site/island_battles_client/__init__.py", stat.S_IFREG | 0o644, b""),
            ("site/island_battles_client/worker.py", stat.S_IFREG | 0o644, FAKE_WORKER.encode()),
            *extra,
        ]
    )


class Server:
    """A local HTTP server in a thread, answering with handle(method, path, headers, body)."""

    def __init__(self, handle: Any):
        class Handler(BaseHTTPRequestHandler):
            def answer(self) -> None:
                body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
                status, headers, data = handle(self.command, self.path, self.headers, body)
                self.send_response(status)
                for name, value in headers.items():
                    self.send_header(name, value)
                if "Content-Length" not in headers:
                    self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                if data:
                    self.wfile.write(data)

            do_GET = do_POST = answer  # noqa: N815

            def log_message(self, *arguments: object) -> None:
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=self.server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        self.host = f"127.0.0.1:{self.server.server_address[1]}"

    def close(self) -> None:
        self.server.shutdown()
        self.server.server_close()


class Packages:
    """Serves zips by path and counts their downloads."""

    def __init__(self) -> None:
        self.zips: dict[str, bytes] = {}
        self.downloads: dict[str, int] = {}
        self.server = Server(self.handle)

    def handle(self, method: str, path: str, headers: Any, body: bytes) -> tuple[int, dict[str, str], bytes]:
        self.downloads[path] = self.downloads.get(path, 0) + 1
        if path not in self.zips:
            return 404, {}, b""
        return 200, {"Content-Type": "application/zip"}, self.zips[path]

    def add(self, package_id: str, data: bytes) -> dict[str, Any]:
        self.zips[f"/{package_id}.zip"] = data
        return {"id": package_id, "url": f"{self.server.url}/{package_id}.zip", "size_bytes": len(data)}

    def downloads_of(self, package_id: str) -> int:
        return self.downloads.get(f"/{package_id}.zip", 0)


class Game:
    """A fake game that takes POST /generation/commands."""

    def __init__(self) -> None:
        self.submissions: list[dict[str, Any]] = []
        self.answers: list[int] = []
        self.server = Server(self.handle)

    def handle(self, method: str, path: str, headers: Any, body: bytes) -> tuple[int, dict[str, str], bytes]:
        if method != "POST" or path != "/generation/commands":
            return 404, {}, b""
        self.submissions.append(
            {
                "authorization": headers.get("Authorization"),
                "user_agent": headers.get("User-Agent"),
                "batch": json.loads(body),
            }
        )
        status = self.answers.pop(0) if self.answers else 200
        if status == 422:
            return (
                status,
                {"Content-Type": "application/problem+json"},
                b'{"type": "about:blank", "detail": "castle 0 has no soldiers to send"}',
            )
        return status, {}, b""

    @property
    def submit_url(self) -> str:
        return f"{self.server.url}/generation/commands"


def example_event(
    package: dict[str, Any], submit_url: str, load_seconds: float = 5, play_seconds: float = 2
) -> dict[str, Any]:
    """backend/botrunner/testdata/event.json, which the Go runner's tests use too."""
    event = json.loads(EXAMPLE_EVENT.read_text())
    event["package"] = package
    event["delivery"]["submit"]["url"] = submit_url
    event["limits"] = {"load_seconds": load_seconds, "play_seconds": play_seconds}
    return event


class RunnerTest(unittest.TestCase):
    def setUp(self) -> None:
        self.packages, self.game = Packages(), Game()
        self.addCleanup(self.packages.server.close)
        self.addCleanup(self.game.server.close)
        self.directory = Path(tempfile.mkdtemp(prefix="bot-runner-test-"))
        self.runner = runner.Runner(self.directory / "bot", self.game.server.host, allow_http_packages=True)
        self.addCleanup(self.runner.unload)
        logger = mock.patch.object(runner.log, "disabled", True)
        logger.start()
        self.addCleanup(logger.stop)

    def handle(self, event: dict[str, Any]) -> dict[str, Any]:
        return self.runner.handle(copy.deepcopy(event), time.monotonic() + 20)

    def pass_line(self, submission: dict[str, Any]) -> str:
        """Check that the submission is a pass, as the default bots pass, and return its line."""
        batch = submission["batch"]
        self.assertEqual(batch["commands"], [])
        self.assertTrue(batch["complete"])
        self.assertEqual(batch["memory"], {"sent_total": 4}, "a pass keeps the turn's memory")
        self.assertEqual(len(batch["logs"]), 1)
        line = batch["logs"][0]
        self.assertEqual((line["level"], line["logger"]), ("error", "island_battles.bots"))
        self.assertTrue(0 < len(line["message"]) <= 500)
        return line["message"]

    def test_plays_and_submits_the_answer_with_the_token(self) -> None:
        event = example_event(self.packages.add("one", fake_package("answering")), self.game.submit_url)

        outcome = self.handle(event)

        self.assertEqual(outcome, {"outcome": "submitted", "status": 200})
        [submission] = self.game.submissions
        self.assertEqual(submission["authorization"], "Bearer " + event["delivery"]["submit"]["token"])
        batch = submission["batch"]
        self.assertEqual((batch["game_id"], batch["generation_id"], batch["complete"]), (1, 12, True))
        self.assertTrue(batch["request_id"])
        self.assertEqual(len(batch["commands"]), 1)
        self.assertEqual(batch["logs"], [{"level": "info", "logger": "island_battles.player", "message": "sent one"}])
        self.assertEqual(batch["memory"]["generation"], 12)

    def test_the_worker_gets_site_on_its_path_and_none_of_the_runners_settings(self) -> None:
        event = example_event(self.packages.add("one", fake_package("answering")), self.game.submit_url)

        with mock.patch.dict(os.environ, {"AWS_SECRET_ACCESS_KEY": "not-for-bots"}):
            self.handle(event)

        memory = self.game.submissions[0]["batch"]["memory"]
        self.assertEqual(memory["pythonpath"], str(self.directory / "bot" / "one" / "site"))
        self.assertIsNone(memory["secret"])

    def test_loads_once_and_reuses_the_same_package(self) -> None:
        event = example_event(self.packages.add("one", fake_package("answering")), self.game.submit_url)

        self.handle(event)
        self.handle(event)

        first, second = (submission["batch"]["memory"]["pid"] for submission in self.game.submissions)
        self.assertEqual(first, second, "the same package keeps its worker")
        self.assertEqual(self.packages.downloads_of("one"), 1)

    def test_loads_over_a_package_an_earlier_runtime_left(self) -> None:
        # A runtime that timed out leaves its package in /tmp.
        stale = self.directory / "bot" / "one" / "site"
        stale.mkdir(parents=True)
        (stale / "bot.py").write_text("stale")
        event = example_event(self.packages.add("one", fake_package("answering")), self.game.submit_url)

        self.assertEqual(self.handle(event), {"outcome": "submitted", "status": 200})

    def test_switching_packages_reloads(self) -> None:
        one = self.packages.add("one", fake_package("answering"))
        two = self.packages.add("two", fake_package("answering"))

        self.handle(example_event(one, self.game.submit_url))
        self.handle(example_event(two, self.game.submit_url))
        self.handle(example_event(one, self.game.submit_url))

        pids = [submission["batch"]["memory"]["pid"] for submission in self.game.submissions]
        self.assertEqual(len(set(pids)), 3, "each switch starts another worker")
        with self.assertRaises(ProcessLookupError):
            os.kill(pids[0], 0)
        self.assertEqual((self.packages.downloads_of("one"), self.packages.downloads_of("two")), (2, 1))
        self.assertEqual(sorted(path.name for path in (self.directory / "bot").iterdir()), ["one"])

    def test_a_load_timeout_passes(self) -> None:
        event = example_event(
            self.packages.add("slow", fake_package("slowload")), self.game.submit_url, load_seconds=0.5
        )

        started = time.monotonic()
        outcome = self.handle(event)

        self.assertLess(time.monotonic() - started, 5)
        self.assertEqual(outcome["outcome"], "passed")
        message = self.pass_line(self.game.submissions[0])
        self.assertIn("could not be loaded", message)
        self.assertIn("not ready within the time to load it", message)
        self.assertIn("slowload", message, "the line ends with the worker's stderr")
        self.assertIsNone(self.runner.loaded)

    def test_a_bot_that_fails_to_import_passes_with_the_workers_error(self) -> None:
        event = example_event(self.packages.add("broken", fake_package("noimport")), self.game.submit_url)

        self.handle(event)

        message = self.pass_line(self.game.submissions[0])
        self.assertIn("ModuleNotFoundError: No module named 'noimport'", message)

    def test_a_play_timeout_passes_and_kills_the_worker(self) -> None:
        event = example_event(
            self.packages.add("slow", fake_package("slowplay")), self.game.submit_url, play_seconds=0.3
        )

        outcome = self.handle(event)

        self.assertEqual(outcome["outcome"], "passed")
        message = self.pass_line(self.game.submissions[0])
        self.assertIn("did not answer within 0.3 seconds", message)
        pid = int(re.search(r"pid=(\d+)", message).group(1))
        with self.assertRaises(ProcessLookupError):
            os.kill(pid, 0)
        self.assertIsNone(self.runner.loaded)
        self.handle(event)
        self.assertEqual(self.packages.downloads_of("slow"), 2, "the next turn loads it again")

    def test_a_crash_includes_stderr_in_the_log_line(self) -> None:
        event = example_event(self.packages.add("crash", fake_package("crash")), self.game.submit_url)

        self.handle(event)

        message = self.pass_line(self.game.submissions[0])
        self.assertIn("stopped (exit status 3)", message)
        self.assertIn("ZeroDivisionError: division by zero", message)
        self.assertIsNone(self.runner.loaded)

    def test_a_long_stderr_is_cut_to_the_line_limit(self) -> None:
        event = example_event(self.packages.add("noisy", fake_package("noisycrash")), self.game.submit_url)

        self.handle(event)

        message = self.pass_line(self.game.submissions[0])
        self.assertEqual(len(message), 500)
        self.assertTrue(message.endswith("line 199 of the bot's output"), message)

    def test_an_error_answer_passes_and_keeps_the_worker(self) -> None:
        event = example_event(self.packages.add("failing", fake_package("failing")), self.game.submit_url)

        self.handle(event)
        self.handle(event)

        self.assertEqual(
            self.pass_line(self.game.submissions[0]),
            "The uploaded bot failed this turn, so it passes: ValueError: no castles",
        )
        self.assertEqual(self.packages.downloads_of("failing"), 1)

    def test_a_rejected_answer_passes(self) -> None:
        self.game.answers = [422]
        event = example_event(self.packages.add("one", fake_package("answering")), self.game.submit_url)

        outcome = self.handle(event)

        self.assertEqual(outcome["outcome"], "passed")
        self.assertEqual(len(self.game.submissions), 2)
        self.assertIn("castle 0 has no soldiers to send", self.pass_line(self.game.submissions[1]))

    def test_retries_a_server_error_with_the_same_batch(self) -> None:
        self.game.answers = [502]
        event = example_event(self.packages.add("one", fake_package("answering")), self.game.submit_url)

        outcome = self.handle(event)

        self.assertEqual(outcome, {"outcome": "submitted", "status": 200})
        first, second = (submission["batch"] for submission in self.game.submissions)
        self.assertEqual(first, second)

    def test_refuses_paths_that_leave_the_package(self) -> None:
        for name, entry in {
            "parent": ("../escaped", stat.S_IFREG | 0o644, b"x"),
            "nested parent": ("site/../../escaped", stat.S_IFREG | 0o644, b"x"),
            "absolute": ("/tmp/escaped", stat.S_IFREG | 0o644, b"x"),
            "backslash": ("..\\escaped", stat.S_IFREG | 0o644, b"x"),
            "symbolic link": ("site/link", stat.S_IFLNK | 0o777, b"/etc/passwd"),
            "device": ("site/device", stat.S_IFCHR | 0o644, b""),
        }.items():
            with self.subTest(name):
                self.game.submissions.clear()
                event = example_event(self.packages.add("bad", fake_package("answering", entry)), self.game.submit_url)

                self.handle(event)

                message = self.pass_line(self.game.submissions[0])
                self.assertRegex(
                    message, "outside the package|absolute path|backslash|symbolic link|not a regular file"
                )
                self.assertFalse((self.directory / "escaped").exists())
                self.assertFalse((self.directory / "bot" / "bad").exists())

    def test_refuses_a_native_package(self) -> None:
        native = zip_bytes(
            [
                (
                    runner.MANIFEST_NAME,
                    stat.S_IFREG | 0o644,
                    b'{"format": 1, "language": "native", "command": ["./bot", "worker"]}',
                ),
                ("bot", stat.S_IFREG | 0o755, b"#!/bin/sh\n"),
            ]
        )
        self.handle(example_event(self.packages.add("native", native), self.game.submit_url))

        self.assertIn("the native runner plays", self.pass_line(self.game.submissions[0]))

    def test_refuses_an_oversized_zip_before_downloading_it(self) -> None:
        package = self.packages.add("big", fake_package("answering"))
        package["size_bytes"] = runner.MAX_ZIP_BYTES + 1

        self.handle(example_event(package, self.game.submit_url))

        self.assertIn("the limit is 262144000", self.pass_line(self.game.submissions[0]))
        self.assertEqual(self.packages.downloads_of("big"), 0)

    def test_refuses_a_download_over_the_limit(self) -> None:
        huge = Server(lambda *arguments: (200, {"Content-Length": str(runner.MAX_ZIP_BYTES + 1)}, b""))
        self.addCleanup(huge.close)

        self.handle(example_event({"id": "huge", "url": f"{huge.url}/huge.zip", "size_bytes": 0}, self.game.submit_url))

        self.assertIn("the limit is 262144000", self.pass_line(self.game.submissions[0]))

    def test_refuses_a_zip_whose_size_differs_from_the_upload(self) -> None:
        package = self.packages.add("changed", fake_package("answering"))
        package["size_bytes"] -= 1

        self.handle(example_event(package, self.game.submit_url))

        self.assertIn("it was uploaded with", self.pass_line(self.game.submissions[0]))

    def test_refuses_a_declared_unpacked_size_over_the_limit(self) -> None:
        with mock.patch.object(runner, "MAX_UNPACKED_BYTES", 1000):
            package = self.packages.add(
                "big", fake_package("answering", ("site/big", stat.S_IFREG | 0o644, b"x" * 2000))
            )
            self.handle(example_event(package, self.game.submit_url))

        self.assertIn("more than 1000 bytes unpacked", self.pass_line(self.game.submissions[0]))
        self.assertFalse((self.directory / "bot" / "big").exists(), "nothing is unpacked")

    def test_stops_unpacking_past_the_limit_whatever_the_zip_declares(self) -> None:
        # The zip's sizes come from the uploader. With the declared check out
        # of the way, as if they lied, the bytes written still count.
        archive = self.directory / "big.zip"
        archive.write_bytes(fake_package("answering", ("site/big", stat.S_IFREG | 0o644, b"x" * 2000)))
        with (
            mock.patch.object(runner, "MAX_UNPACKED_BYTES", 1500),
            mock.patch.object(runner, "check_entries", lambda archive: None),
            self.assertRaisesRegex(runner.Refused, "more than 1500 bytes unpacked"),
        ):
            runner.unpack(archive, self.directory / "unpacked")

    def test_submits_only_to_the_configured_host(self) -> None:
        elsewhere = Game()
        self.addCleanup(elsewhere.server.close)
        package = self.packages.add("one", fake_package("answering"))
        host = self.game.server.host
        for name, submit_url in {
            "another host": elsewhere.submit_url,
            "another port": "http://" + host.replace(":", ":1", 1) + "/generation/commands",
            "user information": f"http://user@{host}/generation/commands",
            "not a URL": "http://[::1/generation/commands",
            "missing": None,
        }.items():
            with self.subTest(name), self.assertRaises(runner.EventError):
                self.handle(example_event(package, submit_url))
        self.assertEqual((self.game.submissions, elsewhere.submissions), ([], []))
        self.assertEqual(self.packages.downloads_of("one"), 0)

    def test_submit_url_rules(self) -> None:
        for url, host in [
            ("https://island-battles.example.org/generation/commands", "island-battles.example.org"),
            ("https://Island-Battles.example.org/generation/commands", "island-battles.example.org"),
            ("http://localhost:8888/generation/commands", "localhost:8888"),
            ("http://127.0.0.1:8888/generation/commands", "127.0.0.1:8888"),
        ]:
            runner.check_submit_url(url, host)
        for url, host in [
            ("http://island-battles.example.org/generation/commands", "island-battles.example.org"),
            ("https://island-battles.example.org.evil.example/generation/commands", "island-battles.example.org"),
            ("https://island-battles.example.org:8443/generation/commands", "island-battles.example.org"),
            ("https://island-battles.example.org/generation/commands", ""),
        ]:
            with self.subTest(url=url, host=host), self.assertRaises(runner.EventError):
                runner.check_submit_url(url, host)

    def test_leaves_hosted_bots_to_the_native_runner(self) -> None:
        event = example_event({}, self.game.submit_url)
        del event["package"], event["limits"]
        event["hosted"] = {"url": "https://bot.example.org/turn"}

        with self.assertRaisesRegex(runner.EventError, "native runner"):
            self.handle(event)

    def test_refuses_malformed_events(self) -> None:
        package = {"id": "one", "url": "https://example.org/one.zip", "size_bytes": 1}
        no_limits = example_event(package, self.game.submit_url)
        del no_limits["limits"]
        no_package = example_event(package, self.game.submit_url)
        del no_package["package"]
        zero_play = example_event(package, self.game.submit_url, play_seconds=0)
        for name, event in {
            "no limits": no_limits,
            "no package": no_package,
            "zero play seconds": zero_play,
            "not an object": [],
        }.items():
            with self.subTest(name), self.assertRaises(runner.EventError):
                self.handle(event)
        self.assertEqual(self.game.submissions, [])

    def test_the_lambda_handler_keeps_one_runner(self) -> None:
        class Context:
            def get_remaining_time_in_millis(self) -> int:
                return 20_000

        event = example_event(self.packages.add("one", fake_package("answering")), self.game.submit_url)
        with (
            mock.patch.object(runner, "_runner", None),
            mock.patch.dict(os.environ, {"GAME_SUBMIT_HOST": self.game.server.host}),
            mock.patch.object(
                runner, "Path", lambda path: self.directory / "lambda" if path == "/tmp/bot" else Path(path)
            ),
        ):
            with self.assertRaises(runner.EventError):
                runner.handler(example_event({}, "https://elsewhere.example/generation/commands"), Context())
            first = runner._runner
            first.allow_http_packages = True
            self.addCleanup(first.unload)
            self.assertEqual(runner.handler(event, Context()), {"outcome": "submitted", "status": 200})
            self.assertIs(runner._runner, first)
            self.assertEqual(first.directory, self.directory / "lambda")


class EventsTest(unittest.TestCase):
    """``--events``, the local server host's runner: one event per line."""

    def test_plays_each_line_and_keeps_the_package_loaded(self) -> None:
        packages, game = Packages(), Game()
        self.addCleanup(packages.server.close)
        self.addCleanup(game.server.close)
        event = example_event(packages.add("one", fake_package("answering")), game.submit_url)
        lines = [json.dumps(event), "", json.dumps(event), "not json", json.dumps(example_event({}, "https://x/y"))]
        output = io.StringIO()

        with mock.patch.object(runner.log, "disabled", True):
            runner.serve_events(lines, output, game.server.host)

        answers = [json.loads(line) for line in output.getvalue().splitlines()]
        self.assertEqual(answers[:2], [{"outcome": "submitted", "status": 200}] * 2)
        self.assertIn("error", answers[2])
        self.assertIn("not on the game's host", answers[3]["error"])
        self.assertEqual(len(answers), 4)
        first, second = (submission["batch"]["memory"]["pid"] for submission in game.submissions)
        self.assertEqual(first, second, "the same worker plays both turns")


class LogLineTest(unittest.TestCase):
    def test_keeps_the_end_of_the_output(self) -> None:
        message = runner.with_output("The uploaded bot stopped", "x" * 1000 + "\nthe last line")
        self.assertEqual(len(message), 500)
        self.assertTrue(message.startswith("The uploaded bot stopped. Its last output: …"))
        self.assertTrue(message.endswith("the last line"))

    def test_without_output_cuts_the_message_alone(self) -> None:
        self.assertEqual(runner.with_output("short", "  \n"), "short")
        self.assertEqual(len(runner.with_output("y" * 600, "output")), 500)


if __name__ == "__main__":
    unittest.main()
