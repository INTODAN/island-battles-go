"""Minimal player; keep its separate LobbyClient joined for heartbeat."""

import argparse
import asyncio

from island_battles_client import (
    NEUTRAL_TEAM_COLOR,
    CommandReceiver,
    Generation,
    IslandBattlesClient,
    Player,
    SoldierGroupOrder,
)


class ExamplePlayer(Player):
    """Send one soldier from the first available castle to the first enemy."""

    async def on_generation(
        self, generation: Generation, commands_receiver: CommandReceiver
    ) -> None:
        """Choose one legal order; returning completes the turn."""
        castles = sorted(generation.castles, key=lambda castle: castle.id)
        own = next(
            (
                castle
                for castle in castles
                if castle.team_color == self.team_color and castle.soldiers > 0
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
            await commands_receiver.send_soldier_groups(
                [
                    SoldierGroupOrder(
                        origin_logical_castle_id=own.id,
                        destination_logical_castle_id=target.id,
                        soldier_amount=1,
                    )
                ]
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
