# Island Battles Python player client

An asyncio client for authenticated generation turns. It uses the existing lobby guest bearer credential and connection-token endpoint. Install with `uv sync --locked` (Python 3.14 or later). Keep the authenticated generic `LobbyClient` joined and running while the game client plays: it owns the lobby heartbeat and lease. Closing it can revoke game access after the lease expires.

Subclass `Player` and implement `async on_generation(generation, commands) -> None`. The shared `Player.__init__(team_color)` assigns `self.team_color` directly, so a strategy needs no constructor or property unless it has additional setup. `Player` is an abstract base class: subclasses must implement `on_generation` before they can be instantiated. It contains no networking or command implementation. The client reads the first authorized generation state, obtains the frozen team color, and calls `player_factory(team_color)` once to build the strategy:

```python
import asyncio

from island_battles_client import (
    NEUTRAL_TEAM_COLOR,
    CommandReceiver,
    Generation,
    IslandBattlesClient,
    Player,
)


class MyPlayer(Player):
    async def on_generation(
        self, generation: Generation, commands_receiver: CommandReceiver
    ) -> None:
        own = next(
            (castle for castle in generation.castles
             if castle.team_color == self.team_color and castle.soldiers > 0),
            None,
        )
        enemy = next(
            (castle for castle in generation.castles
             if castle.team_color not in (NEUTRAL_TEAM_COLOR, self.team_color)),
            None,
        )
        if own is not None and enemy is not None:
            await commands_receiver.send_soldier_group(
                origin_castle_id=own.id,
                destination_castle_id=enemy.id,
                soldier_amount=1,
            )


async def main() -> None:
    client = IslandBattlesClient(
        "http://localhost:8888",
        "guest-bearer-token",
        "lobby-uuid",
        player_factory=MyPlayer,
    )
    await client.run()


asyncio.run(main())
```

The full example is in [examples/player.py](examples/player.py). Obtain the guest credential and lobby ID through the generic lobby flow before starting this client. `player_factory` is invoked once after authentication and the first authorized state read, with the team color bound to that player's frozen roster entry. The constructed strategy is available as `client.player`.

If a strategy needs its own `__init__`, call `super().__init__(team_color)` before initializing its additional state.

`Generation` exposes `id`, `castles`, `soldier_groups`, and `clashes`. These public snapshot models are frozen Pydantic models; nested collections are mutable Python lists. Each `Clash` has a generation-local ID, a time fraction within the turn, and `ClashParticipant` records containing persistent group IDs, teams, contact positions, radii, and soldier counts before and after combat. Older states without clash events expose an empty list. Castle and group values are read directly from `generation.castles` and `generation.soldier_groups`. Transport identity and submission status stay inside the client rather than leaking into the strategy API. `client.generation`, `start()`, and `refresh()` expose the current `Generation` snapshot.

`Castle` and `SoldierGroup` provide `distance(other)` for straight-line distance in map units and `compare_team(other)` returning `TeamType.friendly`, `TeamType.neutral`, or `TeamType.enemy` (import `TeamType` from `island_battles_client`). A target with team color `0` is always neutral, including when both entities are neutral. Otherwise, equal team colors are friendly and different colors are enemies.

`SoldierGroup` requires a `destination_castle: Castle` at construction and provides `distance_left()` with no arguments. The client looks up the destination by its persistent ID in the server response and passes that castle when constructing each group. Distances are floating-point map units, not travel times or generation counts; speed and clash radius do not affect them. All public numeric fields use plain `int` or `float` annotations and are required, including group coordinates, `origin_castle_id`, `destination_castle_id`, and clash radius.

```python
origin, target = generation.castles[:2]
distance = origin.distance(target)
relationship = origin.compare_team(target)
for group in generation.soldier_groups:
    remaining = group.distance_left()
```

The client owns authentication, the personal player-channel subscription, reconnect recovery, state reconciliation, and turn completion. It calls `on_generation` serially with a fresh `CommandReceiver` scoped to that callback. Call `await commands_receiver.send_soldier_group(origin_castle_id, destination_castle_id, soldier_amount)` to send one group; it returns `None`. To send multiple groups, await one call per group. Castle IDs must be non-negative integers and the soldier amount must be a positive integer. The receiver expires when the callback returns or raises. Returning successfully completes the turn automatically, even without sending any soldiers; an exception or cancellation leaves it open.

Each call is pinned to its callback's generation. The SDK creates the request UUID internally and reuses the exact request when retrying a transport failure. A subsequent call, even with identical parameters, sends a new group. Player code only supplies the two castle IDs and soldier amount; authentication, request IDs, and completion belong to the SDK. The SDK also deduplicates repeated events and reconciles authorized HTTP state after subscription or reconnect. The existing `game.next_generation` wire event is unchanged. No periodic polling is enabled by default; `refresh()` can request an explicit read.

The full HTTP contract, authorization rules, reservation checks, retry behavior and status codes are documented in the [backend README](../../backend/README.md). The browser's **New demo lobby** uses its own JavaScript player strategy. See the [demo walkthrough](../../lobby/frontend/README.md).

Run SDK tests with `uv run --locked pytest -q`. The example is checked with Ruff and ty:

```sh
uv run --locked ruff check island_battles_client examples
uv run --locked ruff format --check island_battles_client examples
uv run --locked ty check island_battles_client examples --error-on-warning
```
