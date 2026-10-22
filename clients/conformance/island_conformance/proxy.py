"""HTTP proxy between the client under test and the backend.

It records every exchange and injects the faults real networks produce: lost
responses, server errors and responses that arrive late.
"""

from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass, field
from typing import Any, Self

import aiohttp
from aiohttp import web

FORWARDED_HEADERS = ("Authorization", "Content-Type", "Accept")


@dataclass
class Exchange:
    method: str
    path: str
    body: Any
    status: int | None = None
    injected: str | None = None


@dataclass
class _Rule:
    method: str
    path: str
    action: str
    remaining: int
    status: int = 503
    release: asyncio.Event = field(default_factory=asyncio.Event)
    forwarded: asyncio.Event = field(default_factory=asyncio.Event)


class FaultProxy:
    def __init__(self, upstream: str) -> None:
        self.upstream = upstream.rstrip("/")
        self.exchanges: list[Exchange] = []
        self.url = ""
        self._rules: list[_Rule] = []
        self._runner: web.AppRunner | None = None
        self._session: aiohttp.ClientSession | None = None

    async def __aenter__(self) -> Self:
        application = web.Application()
        application.router.add_route("*", "/{tail:.*}", self._handle)
        self._runner = web.AppRunner(application, access_log=None, handle_signals=False)
        await self._runner.setup()
        site = web.TCPSite(self._runner, "127.0.0.1", 0)
        await site.start()
        host, port = self._runner.addresses[0][:2]
        self.url = f"http://{host}:{port}"
        self._session = aiohttp.ClientSession()
        return self

    async def __aexit__(self, *_: object) -> None:
        for rule in self._rules:
            rule.release.set()
        if self._session is not None:
            await self._session.close()
        if self._runner is not None:
            await self._runner.cleanup()

    def drop_responses(self, method: str, path: str, count: int = 1) -> None:
        """Forward the request, then close the connection without a response."""
        self._rules.append(_Rule(method, path, "drop", count))

    def fail_requests(
        self, method: str, path: str, status: int, count: int = 1
    ) -> None:
        """Answer with an error status without forwarding the request."""
        self._rules.append(_Rule(method, path, "fail", count, status=status))

    def hold_response(
        self, method: str, path: str
    ) -> tuple[asyncio.Event, asyncio.Event]:
        """Forward the next request at once but delay its response until released.

        Returns (forwarded, release) events.
        """
        rule = _Rule(method, path, "hold", 1)
        self._rules.append(rule)
        return rule.forwarded, rule.release

    def requests(
        self, method: str | None = None, path: str | None = None
    ) -> list[Exchange]:
        return [
            exchange
            for exchange in self.exchanges
            if (method is None or exchange.method == method)
            and (path is None or exchange.path == path)
        ]

    def _take(self, method: str, path: str) -> _Rule | None:
        for rule in self._rules:
            if rule.remaining > 0 and rule.method == method and rule.path == path:
                rule.remaining -= 1
                return rule
        return None

    async def _handle(self, request: web.Request) -> web.StreamResponse:
        raw = await request.read()
        try:
            body = json.loads(raw) if raw else None
        except ValueError:
            body = raw.decode(errors="replace")
        exchange = Exchange(request.method, request.path, body)
        self.exchanges.append(exchange)
        rule = self._take(request.method, request.path)
        if rule is not None and rule.action == "fail":
            exchange.status, exchange.injected = rule.status, "fail"
            return web.json_response(
                {
                    "title": "Injected failure",
                    "status": rule.status,
                    "detail": "conformance proxy failure",
                },
                status=rule.status,
                content_type="application/problem+json",
            )
        assert self._session is not None
        headers = {
            name: request.headers[name]
            for name in FORWARDED_HEADERS
            if name in request.headers
        }
        async with self._session.request(
            request.method,
            self.upstream + request.path_qs,
            data=raw or None,
            headers=headers,
        ) as upstream:
            content = await upstream.read()
            exchange.status = upstream.status
            content_type = upstream.headers.get("Content-Type", "application/json")
        if rule is not None and rule.action == "drop":
            exchange.injected = "drop"
            if request.transport is not None:
                request.transport.abort()
            raise ConnectionResetError("conformance proxy dropped the response")
        if rule is not None and rule.action == "hold":
            exchange.injected = "hold"
            rule.forwarded.set()
            await rule.release.wait()
        return web.Response(
            body=content, status=exchange.status, headers={"Content-Type": content_type}
        )
