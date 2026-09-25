"""Commands scoped to one invocation of a player's generation callback."""

from collections.abc import Awaitable, Callable, Iterable
from typing import Any
from uuid import UUID

from .errors import GameError
from .models import SoldierGroupOrder


class CommandReceiver:
    """Submit orders for the current callback; the SDK owns turn completion."""

    def __init__(
        self,
        submit: Callable[[list[dict[str, Any]], UUID | str | None], Awaitable[None]],
    ) -> None:
        self._submit = submit
        self._closed = False

    def _close(self) -> None:
        self._closed = True

    async def send_soldier_groups(
        self,
        groups: Iterable[SoldierGroupOrder],
        *,
        request_id: UUID | str | None = None,
    ) -> None:
        """Send a batch immediately, awaiting it before the callback returns."""
        if self._closed:
            raise GameError(
                "command receiver is closed; use it only during on_generation"
            )
        commands = []
        for group in groups:
            if not isinstance(group, SoldierGroupOrder):
                raise TypeError("orders must be SoldierGroupOrder models")
            commands.append(
                {
                    "command_type": "send_soldier_group",
                    "parameters": group.model_dump(),
                }
            )
        await self._submit(commands, request_id)

    async def send_soldier_group(
        self,
        origin_logical_castle_id: int,
        destination_logical_castle_id: int,
        soldier_amount: int,
        *,
        request_id: UUID | str | None = None,
    ) -> None:
        """Send one order for this callback's generation."""
        await self.send_soldier_groups(
            [
                SoldierGroupOrder(
                    origin_logical_castle_id=origin_logical_castle_id,
                    destination_logical_castle_id=destination_logical_castle_id,
                    soldier_amount=soldier_amount,
                )
            ],
            request_id=request_id,
        )
