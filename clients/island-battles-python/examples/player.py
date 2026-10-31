"""Minimal player; keep its separate LobbyClient joined for heartbeat."""

import argparse
import asyncio

from island_battles_client import (
    NEUTRAL_TEAM_COLOR,
    CommandReceiver,
    GameCommandError,
    Generation,
    IslandBattlesClient,
    Player,
    UpgradeMaxLevelError,
    UpgradeType,
)


class ExamplePlayer(Player):
    """Buy speed where a castle can afford it, then send one soldier to an enemy."""

    async def on_generation(
        self, generation: Generation, commands_receiver: CommandReceiver
    ) -> None:
        """Choose legal orders; returning completes the turn."""
        castles = sorted(generation.castles, key=lambda castle: castle.id)
        spent: dict[int, int] = {}
        for castle in castles:
            if castle.team_color != self.team_color:
                continue
            try:
                cost = commands_receiver.upgrade_cost(castle.id, UpgradeType.speed)
            except UpgradeMaxLevelError:
                continue
            # Keep one soldier to send.
            if cost >= castle.soldiers:
                continue
            try:
                await commands_receiver.upgrade_castle(castle.id, UpgradeType.speed)
            except GameCommandError:
                # A teammate's orders this turn may already have spent the
                # castle's soldiers or bought the level; skip the upgrade.
                continue
            spent[castle.id] = cost
        own = next(
            (
                castle
                for castle in castles
                if castle.team_color == self.team_color
                and castle.soldiers > spent.get(castle.id, 0)
            ),
            None,
        )
        target = next(
            (
                castle
                for castle in castles
                if castle.team_color not in (NEUTRAL_TEAM_COLOR, self.team_color)
            ),
            None,
        )
        if own is not None and target is not None:
            await commands_receiver.send_soldier_group(
                origin_castle_id=own.id,
                destination_castle_id=target.id,
                soldier_amount=1,
            )


async def main() -> None:
    """Run the example for an existing, actively joined lobby player."""
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url")
    parser.add_argument("credential")
    parser.add_argument("lobby_id")
    args = parser.parse_args()
    client = IslandBattlesClient(
        args.base_url,
        args.credential,
        args.lobby_id,
        player_factory=ExamplePlayer,
    )
    await client.run()


if __name__ == "__main__":
    asyncio.run(main())
