"""Example bot: buy speed where a castle can afford it, then send one soldier.

Try it from ``clients/island-battles-python``:

    uv run island-battles-bot check examples.bot:play
    uv run island-battles-bot serve examples.bot:play
"""

from island_battles_client import (
    NEUTRAL_TEAM_COLOR,
    BotTurn,
    BotTurnResult,
    UpgradeType,
    log_line,
    logger,
    send_soldier_group,
    turn_result,
    upgrade_castle,
)

# What reaching speed level 2, 3 and 4 costs; GET /game/upgrades serves the table.
SPEED_COSTS = {2: 10, 3: 20, 4: 30}


def play(turn: BotTurn) -> BotTurnResult:
    """Choose a legal turn from the board; the bot keeps nothing but its memory."""
    castles = sorted(turn.generation.castles, key=lambda castle: castle.id)
    commands = []
    # Soldiers each castle spends this turn. The server rejects a result that
    # spends more than a castle has, so the bot counts what it has spent.
    spent: dict[int, int] = {}
    for castle in castles:
        if castle.team_color != turn.team_color:
            continue
        cost = SPEED_COSTS.get(castle.upgrades[UpgradeType.speed] + 1)
        # Keep one soldier to send.
        if cost is None or cost >= castle.soldiers:
            continue
        commands.append(upgrade_castle(castle.id, UpgradeType.speed))
        spent[castle.id] = cost
    own = next(
        (
            castle
            for castle in castles
            if castle.team_color == turn.team_color
            and castle.soldiers > spent.get(castle.id, 0)
        ),
        None,
    )
    target = next(
        (
            castle
            for castle in castles
            if castle.team_color not in (NEUTRAL_TEAM_COLOR, turn.team_color)
        ),
        None,
    )
    # Memory comes back as the next turn's ``turn.memory``: any JSON object.
    sent = (turn.memory or {}).get("sent", 0)
    if own is not None and target is not None:
        commands.append(send_soldier_group(own.id, target.id, 1))
        sent += 1
        logger.info("sending 1 soldier from castle %d to castle %d", own.id, target.id)
    return turn_result(
        commands,
        # Lines returned here are stored like those logged with ``logger``.
        logs=[
            log_line(f"generation {turn.generation.id}: {sent} soldiers sent so far")
        ],
        memory={"sent": sent},
    )
