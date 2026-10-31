"""Drive a client's conformance adapter over JSON Lines on stdin and stdout."""

from __future__ import annotations

import asyncio
import json
import os
import shlex
from collections import deque
from pathlib import Path
from typing import Any, Self

REPOSITORY = Path(__file__).resolve().parents[3]
DEFAULT_COMMAND = (
    f"uv run --project {REPOSITORY / 'clients/island-battles-python'} --locked --quiet "
    f"python {REPOSITORY / 'clients/island-battles-python/conformance/adapter.py'}"
)


def client_command() -> str:
    """The adapter under test; defaults to the in-repository Python client."""
    return os.environ.get("ISLAND_BATTLES_CLIENT_COMMAND", DEFAULT_COMMAND)


class AdapterProcess:
    """One adapter process. Replies and events are buffered until expected."""

    def __init__(self, command: str | None = None) -> None:
        self.command = command or client_command()
        self.received: list[dict[str, Any]] = []
        self._pending: deque[dict[str, Any]] = deque()
        self._arrived = asyncio.Condition()
        self._stderr: deque[str] = deque(maxlen=60)
        self._process: asyncio.subprocess.Process | None = None
        self._tasks: list[asyncio.Task] = []

    async def __aenter__(self) -> Self:
        self._process = await asyncio.create_subprocess_exec(
            *shlex.split(self.command),
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            limit=16 * 1024 * 1024,
        )
        self._tasks = [
            asyncio.create_task(self._read_stdout()),
            asyncio.create_task(self._read_stderr()),
        ]
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.close()

    async def send(self, message: dict[str, Any]) -> None:
        assert self._process is not None and self._process.stdin is not None
        self._process.stdin.write((json.dumps(message) + "\n").encode())
        await self._process.stdin.drain()

    async def send_together(self, messages: list[dict[str, Any]]) -> None:
        """Write several messages at once, so the adapter acts on them concurrently."""
        assert self._process is not None and self._process.stdin is not None
        self._process.stdin.write(
            "".join(json.dumps(message) + "\n" for message in messages).encode()
        )
        await self._process.stdin.drain()

    async def request(
        self, message: dict[str, Any], reply: str | tuple[str, ...], timeout: float = 20
    ) -> dict[str, Any]:
        await self.send(message)
        return await self.expect(reply, timeout)

    async def expect(
        self, kind: str | tuple[str, ...], timeout: float = 20
    ) -> dict[str, Any]:
        """Return the oldest unconsumed message of this type."""
        kinds = (kind,) if isinstance(kind, str) else kind
        try:
            async with asyncio.timeout(timeout):
                async with self._arrived:
                    while True:
                        for message in self._pending:
                            if message.get("type") in kinds:
                                self._pending.remove(message)
                                return message
                        if self._exited():
                            raise AssertionError(
                                f"adapter exited before {kinds}{self._diagnostics()}"
                            )
                        await self._arrived.wait()
        except TimeoutError:
            raise AssertionError(
                f"adapter did not send {kinds} within {timeout}s{self._diagnostics()}"
            ) from None

    async def assert_quiet(self, kind: str, seconds: float) -> None:
        """Fail if a message of this type arrives within the window."""
        await asyncio.sleep(seconds)
        unexpected = [
            message for message in self._pending if message.get("type") == kind
        ]
        assert not unexpected, f"unexpected {kind}: {unexpected}"

    async def close(self) -> None:
        process = self._process
        if process is None:
            return
        self._process = None
        if process.returncode is None:
            try:
                await self.send_to(process, {"type": "close"})
                await asyncio.wait_for(process.wait(), 10)
            except OSError, TimeoutError:
                process.kill()
                await process.wait()
        for task in self._tasks:
            task.cancel()
        await asyncio.gather(*self._tasks, return_exceptions=True)

    @staticmethod
    async def send_to(
        process: asyncio.subprocess.Process, message: dict[str, Any]
    ) -> None:
        assert process.stdin is not None
        process.stdin.write((json.dumps(message) + "\n").encode())
        await process.stdin.drain()

    def _exited(self) -> bool:
        return self._process is None or self._process.returncode is not None

    def _diagnostics(self) -> str:
        stderr = "\n".join(self._stderr)
        return f"\nreceived: {self.received}\nadapter stderr:\n{stderr}"

    async def _read_stdout(self) -> None:
        assert self._process is not None and self._process.stdout is not None
        stream = self._process.stdout
        while line := await stream.readline():
            try:
                message = json.loads(line)
            except ValueError:
                message = {
                    "type": "invalid_output",
                    "line": line.decode(errors="replace"),
                }
            self.received.append(message)
            async with self._arrived:
                self._pending.append(message)
                self._arrived.notify_all()
        async with self._arrived:
            self._arrived.notify_all()

    async def _read_stderr(self) -> None:
        assert self._process is not None and self._process.stderr is not None
        while line := await self._process.stderr.readline():
            self._stderr.append(line.decode(errors="replace").rstrip())
