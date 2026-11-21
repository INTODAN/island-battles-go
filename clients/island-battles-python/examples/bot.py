"""Example bot: buy speed where a castle can afford it, then send one soldier.

Try it from ``clients/island-battles-python``:

    uv run island-battles-bot check examples.bot:play
    uv run island-battles-bot serve examples.bot:play

When a game ends, ``game_ended`` says how and where to download its demo.
"""

from island_battles_client import (
    NEUTRAL_TEAM_COLOR,
    BotTurn,
    BotTurnResult,
    GameEndedDelivery,
    UpgradeType,
    log_line,
    logger,
    next_upgrade_cost,
    send_soldier_group,
    turn_result,
    upgrade_castle,
)


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
        # The cost table comes with the turn: turn.upgrades.
        cost = next_upgrade_cost(turn, castle, UpgradeType.speed)
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
            log_line(
                f"turn {turn.turn_number} of {turn.max_generations - 1}: "
                f"{sent} soldiers sent so far"
            )
        ],
        memory={"sent": sent},
    )


def game_ended(delivery: GameEndedDelivery) -> None:
    """Say how the game ended; optional, called once when it has its outcome.

    ``download_demo(delivery, "demo.json")`` would save the game's demo, which
    the link in ``delivery.demo`` serves until ``delivery.demo.expires_at``.
    """
    won = delivery.outcome.winner_team_color == delivery.team_color
    logger.info(
        "game %d %s at turn %d: %s",
        delivery.game_id,
        "won" if won else "over",
        delivery.turn_number,
        delivery.outcome.summary,
    )
