# Island Battles Python player client

An asyncio client for authenticated generation turns. It uses the existing lobby guest bearer credential and the lobby connection-token endpoint. Install with `uv sync --locked` (Python 3.14 or later), then subclass `IslandBattlesClient`:

```python
import asyncio
from island_battles_client import IslandBattlesClient, SoldierGroupOrder


class MyPlayer(IslandBattlesClient):
    async def next_generation(self, generation):
        own = next((castle for castle in generation.generation.castles
                    if castle.team_color == generation.team_color and castle.soldiers > 1), None)
        other = next((castle for castle in generation.generation.castles
                      if castle.team_color != generation.team_color), None)
        if own and other:
            await self.send_soldier_groups([SoldierGroupOrder(own.id, other.id, 1)])
        await self.end_turn()  # Empty final batch; seals this generation's turn.


async def main():
    player = MyPlayer("http://localhost:8888", "guest-bearer-token", "lobby-uuid")
    await player.run()


asyncio.run(main())
```

The full example is in [examples/player.py](examples/player.py). Obtain the guest credential and lobby ID through the generic lobby flow before starting this client. **Keep the authenticated `LobbyClient` joined and running while the game client plays:** it owns the lobby heartbeat/lease. Closing it can revoke game access after the lobby lease expires. The game is initialized when that lobby starts; every player in its frozen roster must complete the turn before the next generation appears.

`start()` subscribes to the authenticated `player:<player_uuid>` channel and synchronizes the current state; it returns a `GenerationState`. A first authorized GET discovers the channel, then a second GET after subscription acknowledgment closes the delivery gap. `state` holds the latest authorized state. `next_generation(self, generation)` receives that `GenerationState` once per generation ID, on a separate serial task. Commands inside the callback stay pinned to its generation even if the client refreshes a newer state during the callback. Calls outside the callback use the latest `state`. `run()` waits for callbacks or reconciliation errors and closes the client; `close()` and `async with` are also available.

`send_soldier_groups()` immediately posts a batch with `complete: false`. It accepts `SoldierGroupOrder` values or dictionaries with `origin_logical_castle_id`, `destination_logical_castle_id`, and `soldier_amount`. Castle ID zero is valid. `send_soldier_group()` is a single-order convenience method. `end_turn()` immediately posts an empty batch with `complete: true`; you may call it without sending soldiers. The server rejects forbidden commands and ownership, insufficient soldiers including teammate reservations, stale generations, and commands after completion without applying the rejected batch.

Each request carries a UUID. Supply `request_id=` when an application needs to retry explicitly. The client reuses the same UUID and body through automatic transport retries, and repeated calls with a supplied UUID in one client instance reuse the original generation and payload. A different batch under that UUID raises `GameError` with status 409. You can also supply `generation_id=` for a retry after process restart. `GameError.status` and `.detail` retain the server's HTTP status and Huma problem detail; 401 raises `GameAuthenticationError`. Background authentication and callback failures surface from `run()` or `wait()`.

Realtime publications trigger an authorized HTTP refresh; the push body is only used to select relevant events. The SDK also refreshes after reconnect and every `refresh_interval` seconds (default 15), so a missed publication does not strand the client. `refresh()` permits an explicit read.

Run tests with `uv run --locked pytest -q`.
