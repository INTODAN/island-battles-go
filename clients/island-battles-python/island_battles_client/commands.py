"""Commands scoped to one invocation of a player's generation callback."""

from collections.abc import Awaitable, Callable

from island_game_api_client.models import (
    PublicGenerationCommand,
    PublicGenerationCommandCommandType,
    SendSoldierGroupParameters,
)

from .errors import GameError


class CommandReceiver:
    """Submit commands for the current callback; the SDK completes the turn."""

    def __init__(
        self, submit: Callable[[PublicGenerationCommand], Awaitable[None]]
    ) -> None:
        self._submit = submit
        self._closed = False

    def _close(self) -> None:
        self._closed = True

    async def send_soldier_group(
        self,
        origin_castle_id: int,
        destination_castle_id: int,
        soldier_amount: int,
    ) -> None:
        """Send one group immediately; await this before the callback returns."""
        if self._closed:
            raise GameError(
                "command receiver is closed; use it only during on_generation"
            )
        parameters = {
            "origin_castle_id": origin_castle_id,
            "destination_castle_id": destination_castle_id,
            "soldier_amount": soldier_amount,
        }
        for name, value in parameters.items():
            if type(value) is not int:
                raise TypeError(f"{name} must be an integer")
            minimum = 1 if name == "soldier_amount" else 0
            if value < minimum:
                raise ValueError(f"{name} must be at least {minimum}")
        await self._submit(
            PublicGenerationCommand(
                command_type=PublicGenerationCommandCommandType.SEND_SOLDIER_GROUP,
                parameters=SendSoldierGroupParameters(
                    origin_persistent_castle_id=origin_castle_id,
                    destination_persistent_castle_id=destination_castle_id,
                    soldier_amount=soldier_amount,
                ),
            )
        )
