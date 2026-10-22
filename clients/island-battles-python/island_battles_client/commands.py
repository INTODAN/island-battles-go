"""Commands scoped to one invocation of a player's generation callback."""

from collections.abc import Awaitable, Callable

from ._generated import PublicGenerationCommand, SendSoldierGroupParameters
from .errors import GameReceiverClosedError


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
        """Send one group immediately; await this before the callback returns.

        Arguments are validated against the API schema before any request, so
        invalid values raise ``pydantic.ValidationError``.
        """
        if self._closed:
            raise GameReceiverClosedError()
        parameters = SendSoldierGroupParameters(
            origin_castle_id=origin_castle_id,
            destination_castle_id=destination_castle_id,
            soldier_amount=soldier_amount,
        )
        await self._submit(
            PublicGenerationCommand(
                command_type="send_soldier_group", parameters=parameters
            )
        )
