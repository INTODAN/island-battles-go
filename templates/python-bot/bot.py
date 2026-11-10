"""Starter bot: buy speed where a castle can afford it, then attack the nearest castle.

The bot keeps nothing between turns except its memory, which the server
stores. See `.agents/skills/writing-a-bot/SKILL.md`.
"""

from island_battles_client import (
    BotTurn,
    BotTurnResult,
    UpgradeType,
    logger,
    next_upgrade_cost,
    send_soldier_group,
    turn_result,
    upgrade_castle,
)


def play(turn: BotTurn) -> BotTurnResult:
    """Play one turn: a pure function of the turn, with the memory as its only state.

    Args:
        turn: The board, this bot's team, the upgrade table and last turn's memory.

    Returns:
        The commands to run, and the memory for the next turn.
    """
    castles = sorted(turn.generation.castles, key=lambda castle: castle.id)
    own_castles = [castle for castle in castles if castle.team_color == turn.team_color]
    targets = [castle for castle in castles if castle.team_color != turn.team_color]
    commands = []
    # Soldiers each castle spends this turn. The server rejects the whole result
    # when a castle spends more than it has, so the bot counts what it spends.
    spent: dict[int, int] = {}
    for castle in own_castles:
        cost = next_upgrade_cost(turn, castle, UpgradeType.speed)
        # Keep at least one soldier to send.
        if cost is None or cost >= castle.soldiers:
            continue
        commands.append(upgrade_castle(castle.id, UpgradeType.speed))
        spent[castle.id] = cost

    # Memory comes back as the next turn's `turn.memory`: a small JSON object.
    sent_total = int((turn.memory or {}).get("sent_total", 0))
    for castle in own_castles:
        available = castle.soldiers - spent.get(castle.id, 0)
        if available < 2 or not targets:
            continue
        # Neutral castles count as targets too.
        target = min(targets, key=castle.distance)
        amount = available // 2
        commands.append(send_soldier_group(castle.id, target.id, amount))
        sent_total += amount

    logger.info(
        "generation %d: %d commands, %d soldiers sent so far",
        turn.generation.id,
        len(commands),
        sent_total,
    )
    return turn_result(commands, memory={"sent_total": sent_total})
