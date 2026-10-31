"""Conformance adapter: exposes the SDK through clients/conformance's JSON Lines protocol."""

import asyncio
import contextlib
import json
import logging
import sys
from typing import Any

from pydantic import ValidationError

from island_battles_client import (
    CastleNotFoundError,
    CastleNotOwnedError,
    CommandReceiver,
    GameAuthenticationError,
    GameClientClosedError,
    GameError,
    GameOutcome,
    GameProtocolError,
    GameReceiverClosedError,
    GameTransportError,
    Generation,
    InsufficientSoldiersError,
    IslandBattlesClient,
    Player,
    UpgradeMaxLevelError,
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
        (GameReceiverClosedError, "receiver_closed"),
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


class ScriptedPlayer(Player):
    """Report each generation, then act on the runner's orders."""

    def __init__(self, team_color: int, adapter: Adapter) -> None:
        """Remember the adapter that relays orders."""
        super().__init__(team_color)
        self.adapter = adapter

    async def on_generation(
        self, generation: Generation, commands_receiver: CommandReceiver
    ) -> None:
        """Wait for finish (return) or fail (raise)."""
        self.adapter.receiver = commands_receiver
        self.adapter.emit(
            {"type": "generation", "generation": generation.model_dump(mode="json")}
        )
        while (decision := await self.adapter.decisions.get())["type"] == "log":
            # Logged inside the callback, so the line goes to the game log.
            self.logger.log(LOG_LEVELS[decision["level"]], decision["message"])
            self.adapter.emit({"type": "logged"})
        if decision["type"] == "fail":
            raise RuntimeError("scripted callback failure")

    async def on_game_ended(self, outcome: GameOutcome, generation: Generation) -> None:
        """Report the outcome and the final board."""
        self.adapter.emit(
            {
                "type": "ended",
                "outcome": outcome.model_dump(mode="json"),
                "generation": generation.model_dump(mode="json"),
            }
        )


class Adapter:
    """Translate protocol messages into SDK calls."""

    def __init__(self) -> None:
        """Start with no client; the runner sends start or evaluate."""
        self.decisions: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
        self.receiver: CommandReceiver | None = None
        self.client: IslandBattlesClient | None = None
        self.closing = False
        self.tasks: set[asyncio.Task[None]] = set()

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
            player_factory=lambda team_color: ScriptedPlayer(team_color, self),
            # Stdout carries the protocol; the adapter logs to stderr instead.
            log_level=logging.DEBUG,
            log_to_stdout=False,
            log_file=message.get("log_file"),
        )
        try:
            await self.client.start()
            assert self.client.player is not None
            self.emit({"type": "started", "team_color": self.client.player.team_color})
            await self.client.wait()
        except Exception as error:  # noqa: BLE001 - every failure is reported
            if not self.closing:
                self.emit({"type": "stopped", "error": describe(error)})
            await self.client.close()

    async def send(self, message: dict[str, Any]) -> None:
        """Send one group with the current or most recent receiver."""
        if self.receiver is None:
            self.emit(
                {"type": "send_failed", "error": describe(GameReceiverClosedError())}
            )
            return
        try:
            await self.receiver.send_soldier_group(
                message.get("origin_castle_id"),  # ty: ignore[invalid-argument-type]
                message.get("destination_castle_id"),  # ty: ignore[invalid-argument-type]
                message.get("soldier_amount"),  # ty: ignore[invalid-argument-type]
            )
        except Exception as error:  # noqa: BLE001 - every failure is reported
            self.emit({"type": "send_failed", "error": describe(error)})
        else:
            self.emit({"type": "sent"})

    async def upgrade(self, message: dict[str, Any]) -> None:
        """Upgrade one castle with the current or most recent receiver."""
        if self.receiver is None:
            self.emit(
                {"type": "send_failed", "error": describe(GameReceiverClosedError())}
            )
            return
        try:
            await self.receiver.upgrade_castle(
                message.get("castle_id"),  # ty: ignore[invalid-argument-type]
                message.get("upgrade_type"),  # ty: ignore[invalid-argument-type]
            )
        except Exception as error:  # noqa: BLE001 - every failure is reported
            self.emit({"type": "send_failed", "error": describe(error)})
        else:
            self.emit({"type": "sent"})

    def cost(self, message: dict[str, Any]) -> None:
        """Report what the next upgrade of a castle costs."""
        try:
            if self.receiver is None:
                raise GameReceiverClosedError()
            cost = self.receiver.upgrade_cost(
                message.get("castle_id"),  # ty: ignore[invalid-argument-type]
                message.get("upgrade_type"),  # ty: ignore[invalid-argument-type]
            )
        except Exception as error:  # noqa: BLE001 - every failure is reported
            self.emit({"type": "cost_failed", "error": describe(error)})
        else:
            self.emit({"type": "cost", "cost": cost})

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
                case "send_soldier_group":
                    self.spawn(self.send(message))
                case "upgrade_castle":
                    self.spawn(self.upgrade(message))
                case "upgrade_cost":
                    self.cost(message)
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
