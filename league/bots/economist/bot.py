"""Economist: raise production first, expand with the surplus, attack when far ahead.

A founder of the league (iteration 0). Every castle buys generation_amount
levels while it can keep a guard home, then spends like the expander. The bot
keeps nothing between turns except its memory.
"""

from math import ceil

from island_battles_client import (
    NEUTRAL_TEAM_COLOR,
    BotTurn,
    BotTurnResult,
    Castle,
    PublicGenerationCommand,
    SoldierGroup,
    UpgradeType,
    logger,
    next_upgrade_cost,
    production,
    send_soldier_group,
    turn_result,
    upgrade_castle,
)


class Board:
    """The turn's board, and what this turn's commands have spent so far."""

    def __init__(self, turn: BotTurn) -> None:
        """Read the board from the turn."""
        self.turn = turn
        self.castles = sorted(turn.generation.castles, key=lambda castle: castle.id)
        self.groups = sorted(turn.generation.soldier_groups, key=lambda group: group.id)
        self.own = [castle for castle in self.castles if castle.team_color == turn.team_color]
        self.others = [castle for castle in self.castles if castle.team_color != turn.team_color]
        self.enemies = [castle for castle in self.others if castle.team_color != NEUTRAL_TEAM_COLOR]
        self.neutrals = [castle for castle in self.others if castle.team_color == NEUTRAL_TEAM_COLOR]
        self.commands: list[PublicGenerationCommand] = []
        # Soldiers each castle has spent this turn. The server rejects the
        # whole result when a castle spends more than it has.
        self.spent: dict[int, int] = {}
        # Speed levels bought this turn, which the groups sent after them have.
        self.speed_bought: dict[int, int] = {}
        self.bought: set[tuple[int, UpgradeType]] = set()

    def level_value(self, upgrade_type: UpgradeType, level: int) -> float:
        """The value of an upgrade type's level, from the turn's table."""
        for upgrade in self.turn.upgrades:
            if upgrade.type == upgrade_type:
                for entry in upgrade.levels:
                    if entry.level == level:
                        return entry.value
        return 0.0

    def left(self, castle: Castle) -> int:
        """The soldiers the castle hasn't spent yet this turn."""
        return castle.soldiers - self.spent.get(castle.id, 0)

    def travel_turns(self, origin: Castle, target: Castle) -> int:
        """The turns a group sent now from origin takes to land on target."""
        level = origin.upgrades[UpgradeType.speed] + self.speed_bought.get(origin.id, 0)
        return max(
            1,
            ceil(origin.distance(target) / self.level_value(UpgradeType.speed, level) - 1e-9),
        )

    def arrival_turns(self, group: SoldierGroup) -> int:
        """The turns until a group lands, if nothing stops it."""
        speed = self.level_value(UpgradeType.speed, group.upgrades[UpgradeType.speed])
        return max(1, ceil(group.distance_left() / speed - 1e-9))

    def heading_to(self, castle: Castle, friendly: bool) -> int:
        """The soldiers on their way to the castle: this bot's, or everyone else's."""
        return sum(
            group.soldiers
            for group in self.groups
            if group.destination_castle_id == castle.id and (group.team_color == self.turn.team_color) == friendly
        )

    def defenders_on_arrival(self, origin: Castle, target: Castle) -> int:
        """The soldiers a group sent now from origin would meet in target.

        A castle that a team owns grows while the group walks; a neutral one
        doesn't.
        """
        growth = 0
        if target.team_color != NEUTRAL_TEAM_COLOR:
            growth = production(self.turn, target) * self.travel_turns(origin, target)
        return target.soldiers + growth

    def send(self, origin: Castle, target: Castle, amount: int) -> None:
        """Send soldiers, counting them as spent."""
        self.commands.append(send_soldier_group(origin.id, target.id, amount))
        self.spent[origin.id] = self.spent.get(origin.id, 0) + amount

    def buy(self, castle: Castle, upgrade_type: UpgradeType, reserve: int = 0) -> bool:
        """Buy the castle's next level of an upgrade if it leaves the reserve, once a turn."""
        cost = next_upgrade_cost(self.turn, castle, upgrade_type)
        if cost is None or self.left(castle) - cost < reserve or (castle.id, upgrade_type) in self.bought:
            return False
        self.bought.add((castle.id, upgrade_type))
        self.commands.append(upgrade_castle(castle.id, upgrade_type))
        self.spent[castle.id] = self.spent.get(castle.id, 0) + cost
        if upgrade_type == UpgradeType.speed:
            self.speed_bought[castle.id] = 1
        return True


# Soldiers a castle keeps home besides what is on its way to attack it.
GUARD = 4
# The bot attacks an enemy castle only with this many times its defenders.
ATTACK_MARGIN = 1.3


def play(turn: BotTurn) -> BotTurnResult:
    """Play one turn: a pure function of the turn.

    Args:
        turn: The board, this bot's team, the upgrade table and last turn's memory.

    Returns:
        The commands to run, and the memory for the next turn.
    """
    board = Board(turn)
    notes = []
    for castle in board.own:
        reserve = board.heading_to(castle, friendly=False) + GUARD
        if board.buy(castle, UpgradeType.generation_amount, reserve=reserve):
            notes.append(f"production@{castle.id}")
        available = board.left(castle) - reserve
        if available < 1:
            continue
        best = None
        for target in board.others:
            defenders = board.defenders_on_arrival(castle, target)
            if target.team_color != NEUTRAL_TEAM_COLOR:
                defenders = ceil(defenders * ATTACK_MARGIN)
            need = defenders + 1 - board.heading_to(target, friendly=True)
            if need < 1 or need > available:
                continue
            # A castle that already produces more is worth more.
            cost = need + 2 * board.travel_turns(castle, target) - 3 * production(turn, target)
            if best is None or cost < best[0]:
                best = (cost, need, target)
        if best is not None:
            board.send(castle, best[2], best[1])
            notes.append(f"{castle.id}->{best[2].id}:{best[1]}")
    bought = int((turn.memory or {}).get("bought", 0)) + sum("production@" in note for note in notes)
    logger.info(
        "turn %d: %d castles producing %d a turn; %s",
        turn.turn_number,
        len(board.own),
        sum(production(turn, castle) for castle in board.own),
        " ".join(notes) or "saving",
    )
    return turn_result(board.commands, memory={"bought": bought})
