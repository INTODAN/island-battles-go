"""Conformance adapter: exposes the SDK through clients/conformance's JSON Lines protocol."""

import asyncio
import contextlib
import json
import logging
import sys
from typing import Any

from pydantic import ValidationError

from island_battles_client import (
    BotMemoryTooLargeError,
    BotTurn,
    BotTurnResult,
    CastleNotFoundError,
    CastleNotOwnedError,
    GameAuthenticationError,
    GameClientClosedError,
    GameError,
    GameOutcome,
    GameProtocolError,
    GameTransportError,
    Generation,
    InsufficientSoldiersError,
    IslandBattlesClient,
    UpgradeMaxLevelError,
    log_line,
    logger,
    send_soldier_group,
    turn_result,
    upgrade_castle,
)

LOG_LEVELS = {
    "debug": logging.DEBUG,
    "info": logging.INFO,
    "warning": logging.WARNING,
    "error": logging.ERROR,
}


def describe(error: BaseException) -> dict[str, Any]:
    """Map an SDK failure onto the protocol's error kinds."""
    status = getattr(error, "status", None)
    kinds = (
        (ValidationError, "invalid_argument"),
        (BotMemoryTooLargeError, "memory_too_large"),
        (GameClientClosedError, "closed"),
        (GameAuthenticationError, "authentication"),
        (GameTransportError, "transport"),
        (GameProtocolError, "protocol"),
        (CastleNotFoundError, "castle_not_found"),
        (CastleNotOwnedError, "not_owned"),
        (UpgradeMaxLevelError, "max_level"),
        (InsufficientSoldiersError, "insufficient_soldiers"),
        (GameError, "rejected"),
    )
    kind = next((kind for type_, kind in kinds if isinstance(error, type_)), "player")
    return {"kind": kind, "status": status, "detail": str(error)}


class Adapter:
    """Translate protocol messages into SDK calls."""

    def __init__(self) -> None:
        """Start with no client; the runner sends start or evaluate."""
        self.decisions: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
        self.client: IslandBattlesClient | None = None
        self.closing = False
        self.tasks: set[asyncio.Task[None]] = set()

    def order(self, order: dict[str, Any]) -> Any:
        """Build a command from the runner's order, with the values as given."""
        if order["type"] == "send_soldier_group":
            return send_soldier_group(
                order.get("origin_castle_id"),  # ty: ignore[invalid-argument-type]
                order.get("destination_castle_id"),  # ty: ignore[invalid-argument-type]
                order.get("soldier_amount"),  # ty: ignore[invalid-argument-type]
            )
        return upgrade_castle(
            order.get("castle_id"),  # ty: ignore[invalid-argument-type]
            order.get("upgrade_type"),  # ty: ignore[invalid-argument-type]
        )

    async def play(self, turn: BotTurn) -> BotTurnResult:
        """Report the turn, then act on the runner's orders: finish or fail."""
        self.emit(
            {
                "type": "generation",
                "generation": turn.generation.model_dump(mode="json"),
                "memory": turn.memory,
                "team_color": turn.team_color,
                "game_id": turn.game_id,
                "upgrades": [
                    upgrade.model_dump(mode="json") for upgrade in turn.upgrades
                ],
                "turn_deadline_at": turn.turn_deadline_at.isoformat()
                if turn.turn_deadline_at
                else None,
            }
        )
        while (decision := await self.decisions.get())["type"] == "log":
            # Logged inside the turn, so the line goes to the game log.
            logger.log(LOG_LEVELS[decision["level"]], decision["message"])
            self.emit({"type": "logged"})
        if decision["type"] == "fail":
            raise RuntimeError("scripted callback failure")
        return turn_result(
            [self.order(order) for order in decision.get("commands", [])],
            [
                log_line(entry["message"], entry["level"])
                for entry in decision.get("logs", [])
            ],
            memory=decision.get("memory"),
        )

    async def on_game_ended(self, outcome: GameOutcome, generation: Generation) -> None:
        """Report the outcome and the final board."""
        self.emit(
            {
                "type": "ended",
                "outcome": outcome.model_dump(mode="json"),
                "generation": generation.model_dump(mode="json"),
            }
        )

    def emit(self, message: dict[str, Any]) -> None:
        """Write one protocol message."""
        sys.stdout.write(json.dumps(message) + "\n")
        sys.stdout.flush()

    def spawn(self, coroutine: Any) -> None:
        """Keep a reference so the task is not garbage collected."""
        task = asyncio.create_task(coroutine)
        self.tasks.add(task)
        task.add_done_callback(self.tasks.discard)

    def evaluate(self, message: dict[str, Any]) -> None:
        """Parse a wire generation and report the public model's utilities."""
        try:
            generation = Generation.model_validate(message["generation"])
        except ValidationError:
            self.emit({"type": "evaluation", "id": message["id"], "ok": False})
            return
        entities = [*generation.castles, *generation.soldier_groups]
        self.emit(
            {
                "type": "evaluation",
                "id": message["id"],
                "ok": True,
                "generation": generation.model_dump(mode="json"),
                "distances": [[a.distance(b) for b in entities] for a in entities],
                "relations": [
                    [a.compare_team(b).value for b in entities] for a in entities
                ],
                "distance_left": [
                    group.distance_left() for group in generation.soldier_groups
                ],
            }
        )

    async def run(self, message: dict[str, Any]) -> None:
        """Run the client until it is closed or fails."""
        self.client = IslandBattlesClient(
            message["base_url"],
            message["credential"],
            message["lobby_id"],
            play=self.play,
            on_game_ended=self.on_game_ended,
            # Stdout carries the protocol; the adapter logs to stderr instead.
            log_level=logging.DEBUG,
            log_to_stdout=False,
            log_file=message.get("log_file"),
        )
        try:
            await self.client.start()
            self.emit({"type": "started", "team_color": self.client.team_color})
            await self.client.wait()
        except Exception as error:  # noqa: BLE001 - every failure is reported
            if not self.closing:
                self.emit({"type": "stopped", "error": describe(error)})
            await self.client.close()

    async def close(self) -> None:
        """Close without completing an open turn."""
        self.closing = True
        if self.client is not None:
            await self.client.close()
        self.emit({"type": "closed"})

    async def main(self) -> None:
        """Process stdin until close or end of input."""
        loop = asyncio.get_running_loop()
        reader = asyncio.StreamReader(limit=16 * 1024 * 1024)
        await loop.connect_read_pipe(
            lambda: asyncio.StreamReaderProtocol(reader), sys.stdin
        )
        while line := await reader.readline():
            message = json.loads(line)
            match message["type"]:
                case "evaluate":
                    self.evaluate(message)
                case "start":
                    self.spawn(self.run(message))
                case "finish" | "fail" | "log":
                    self.decisions.put_nowait(message)
                case "close":
                    break
        await self.close()
        for task in self.tasks:
            task.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await asyncio.gather(*self.tasks, return_exceptions=True)


if __name__ == "__main__":
    # The runner shows stderr when a test fails.
    logging.getLogger("island_battles").addHandler(logging.StreamHandler(sys.stderr))
    asyncio.run(Adapter().main())
