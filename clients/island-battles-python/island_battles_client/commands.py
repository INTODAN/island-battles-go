"""Commands scoped to one invocation of a player's generation callback."""

from collections import Counter
from collections.abc import Awaitable, Callable

from ._entities import NEUTRAL_TEAM_COLOR
from ._generated import (
    Castle,
    Generation,
    PublicGenerationCommand,
    SendSoldierGroupCommand,
    SendSoldierGroupParameters,
    UpgradeCastleCommand,
    UpgradeCastleParameters,
    UpgradeType,
)
from .errors import (
    CastleNotFoundError,
    CastleNotOwnedError,
    GameReceiverClosedError,
    InsufficientSoldiersError,
    UpgradeMaxLevelError,
)
from .upgrades import UpgradeTable


class CommandReceiver:
    """Submit commands for the current callback; the SDK completes the turn.

    The receiver remembers what this callback's commands reserved: the soldiers
    each castle sends, and the upgrade levels it buys, which pay their costs.
    """

    def __init__(
        self,
        submit: Callable[[PublicGenerationCommand], Awaitable[None]],
        *,
        generation: Generation,
        team_color: int,
        upgrades: UpgradeTable,
    ) -> None:
        self._submit = submit
        self._castles = {castle.id: castle for castle in generation.castles}
        self._team_color = team_color
        self._upgrades = upgrades
        self._sent_soldiers: Counter[int] = Counter()
        self._bought_levels: Counter[tuple[int, UpgradeType]] = Counter()
        self._closed = False

    def _close(self) -> None:
        self._closed = True

    def _check_open(self) -> None:
        if self._closed:
            raise GameReceiverClosedError()

    def _castle(self, castle_id: int) -> Castle:
        castle = self._castles.get(castle_id)
        if castle is None:
            raise CastleNotFoundError(f"castle {castle_id} is not in this generation")
        return castle

    def _reserved(self, castle: Castle) -> int:
        """Soldiers this callback's sends and upgrades take from the castle.

        Upgrade costs follow from the number of levels bought, not from what
        each call expected to pay, so they stay right when concurrent calls
        reach the server in another order or one of them fails.
        """
        reserved = self._sent_soldiers[castle.id]
        for upgrade_type in UpgradeType:
            level = castle.upgrades.get(upgrade_type, 1)
            bought = self._bought_levels[(castle.id, upgrade_type)]
            for bought_level in range(level + 1, level + bought + 1):
                reserved += self._upgrades.cost(upgrade_type, bought_level)
        return reserved

    def _next_level(self, castle: Castle, upgrade_type: UpgradeType) -> int:
        level = castle.upgrades.get(upgrade_type, 1)
        level += self._bought_levels[(castle.id, upgrade_type)]
        if level >= self._upgrades.max_level(upgrade_type):
            raise UpgradeMaxLevelError(
                f"castle {castle.id} already reaches {upgrade_type} level {level}, "
                "the maximum, counting this turn's upgrades"
            )
        return level + 1

    async def send_soldier_group(
        self,
        origin_castle_id: int,
        destination_castle_id: int,
        soldier_amount: int,
    ) -> None:
        """Send one group immediately; await this before the callback returns.

        Arguments are validated against the API schema before any request, so
        invalid values raise ``pydantic.ValidationError``. A missing castle, an
        origin your team does not own and too few soldiers raise the matching
        ``GameCommandError`` from the server's answer.
        """
        self._check_open()
        parameters = SendSoldierGroupParameters(
            origin_castle_id=origin_castle_id,
            destination_castle_id=destination_castle_id,
            soldier_amount=soldier_amount,
        )
        # Reserved before the request, so a concurrent call counts it too.
        self._sent_soldiers[parameters.origin_castle_id] += parameters.soldier_amount
        try:
            await self._submit(
                PublicGenerationCommand(
                    SendSoldierGroupCommand(
                        command_type="send_soldier_group", parameters=parameters
                    )
                )
            )
        except BaseException:
            self._sent_soldiers[parameters.origin_castle_id] -= (
                parameters.soldier_amount
            )
            raise

    def upgrade_cost(self, castle_id: int, upgrade_type: UpgradeType | str) -> int:
        """Soldiers the next ``upgrade_castle`` of this castle and type costs.

        Counts the levels this callback already bought, and sends no request.
        Raises ``pydantic.ValidationError`` for invalid arguments,
        ``CastleNotFoundError`` for a castle that is not on the board, and
        ``UpgradeMaxLevelError`` when there is no next level.
        """
        self._check_open()
        parameters = UpgradeCastleParameters(
            castle_id=castle_id,
            upgrade_type=upgrade_type,
        )
        castle = self._castle(parameters.castle_id)
        level = self._next_level(castle, parameters.upgrade_type)
        return self._upgrades.cost(parameters.upgrade_type, level)

    async def upgrade_castle(
        self, castle_id: int, upgrade_type: UpgradeType | str
    ) -> None:
        """Buy the next level of one upgrade type for a castle your team owns.

        The castle pays the level's cost, ``upgrade_cost`` beforehand, from its
        soldiers. Each call is its own request. Before any request it raises
        ``pydantic.ValidationError`` for invalid arguments, ``CastleNotFoundError``,
        ``CastleNotOwnedError``, ``UpgradeMaxLevelError`` and
        ``InsufficientSoldiersError``, counting what this callback already sent and
        bought. The server raises the same errors for what only it can see, such as
        a teammate's reservations.
        """
        self._check_open()
        parameters = UpgradeCastleParameters(
            castle_id=castle_id,
            upgrade_type=upgrade_type,
        )
        castle = self._castle(parameters.castle_id)
        if castle.team_color != self._team_color:
            owner = (
                "no team"
                if castle.team_color == NEUTRAL_TEAM_COLOR
                else f"team {castle.team_color}"
            )
            raise CastleNotOwnedError(
                f"castle {castle.id} belongs to {owner}, not to your team {self._team_color}"
            )
        level = self._next_level(castle, parameters.upgrade_type)
        cost = self._upgrades.cost(parameters.upgrade_type, level)
        unreserved = castle.soldiers - self._reserved(castle)
        if cost > unreserved:
            raise InsufficientSoldiersError(
                f"castle {castle.id} has {unreserved} unreserved soldiers; "
                f"{parameters.upgrade_type} level {level} costs {cost}"
            )
        bought = (castle.id, parameters.upgrade_type)
        self._bought_levels[bought] += 1
        try:
            await self._submit(
                PublicGenerationCommand(
                    UpgradeCastleCommand(
                        command_type="upgrade_castle", parameters=parameters
                    )
                )
            )
        except BaseException:
            self._bought_levels[bought] -= 1
            raise
