"""The castle upgrade table, which the client reads once from GET /game/upgrades."""

from collections.abc import Iterable, Mapping
from types import MappingProxyType

from ._generated import Upgrade, UpgradeType
from .errors import GameProtocolError


class UpgradeTable:
    """Every upgrade type's levels, effects and soldier costs, as the server serves them.

    The table is static for a server and read-only here. Levels run from 1, where
    every castle starts, to ``max_level``.
    """

    def __init__(self, upgrades: Iterable[Upgrade]) -> None:
        table = {upgrade.type: upgrade for upgrade in upgrades}
        for upgrade in table.values():
            levels = [level.level for level in upgrade.levels]
            if levels != list(range(1, upgrade.max_level + 1)):
                raise GameProtocolError(
                    f"upgrade table lists {upgrade.type} levels {levels}"
                )
        if set(table) != set(UpgradeType):
            raise GameProtocolError("upgrade table does not list every upgrade type")
        self._upgrades: Mapping[UpgradeType, Upgrade] = MappingProxyType(table)

    @property
    def upgrades(self) -> Mapping[UpgradeType, Upgrade]:
        """Each type's description, ``max_level`` and ``levels``."""
        return self._upgrades

    def max_level(self, upgrade_type: UpgradeType | str) -> int:
        """The highest level of a type. Raises ``ValueError`` for an unknown type."""
        return self._upgrades[UpgradeType(upgrade_type)].max_level

    def cost(self, upgrade_type: UpgradeType | str, level: int) -> int:
        """Soldiers a castle pays to reach ``level`` from the level below.

        Level 1 is free. Raises ``ValueError`` for an unknown type or a level
        outside 1 to ``max_level``.
        """
        upgrade = self._upgrades[UpgradeType(upgrade_type)]
        if not 1 <= level <= upgrade.max_level:
            raise ValueError(
                f"{upgrade.type} levels run from 1 to {upgrade.max_level}, not {level}"
            )
        return upgrade.levels[level - 1].soldier_cost
