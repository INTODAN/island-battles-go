"""Minimal generation player; keep its separate LobbyClient joined for heartbeat."""

import argparse
import asyncio

from island_battles_client import IslandBattlesClient, SoldierGroupOrder


class ExamplePlayer(IslandBattlesClient):
    async def next_generation(self, generation):
        own = next((castle for castle in generation.generation.castles
                    if castle.team_color == generation.team_color and castle.soldiers >= 2), None)
        target = next((castle for castle in generation.generation.castles
                       if castle.team_color != generation.team_color), None)
        if own and target:
            await self.send_soldier_groups([SoldierGroupOrder(own.id, target.id, 1)])
        await self.end_turn()


async def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url")
    parser.add_argument("credential")
    parser.add_argument("lobby_id")
    args = parser.parse_args()
    await ExamplePlayer(args.base_url, args.credential, args.lobby_id).run()


if __name__ == "__main__":
    asyncio.run(main())
