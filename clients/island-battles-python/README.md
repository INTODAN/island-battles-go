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

To start a lobby on a chosen map, pass Island Battles' `game_settings` to the lobby client's `create_lobby`: `{"map_template_id": id}` for a default map from `GET /map/templates`, or `{"map": map_file}` for a map file, with exactly the map's number of teams. The player OpenAPI document publishes them as `LobbySettings`; the [backend README](../../backend/README.md#choosing-a-lobbys-map) has the rules.

If a strategy needs its own `__init__`, call `super().__init__(team_color)` before initializing its additional state.

`Generation` exposes `id`, `castles`, `soldier_groups`, and `clashes`. These models are generated from the backend's [player OpenAPI document](../../backend/api/openapi.json) into `island_battles_client/_generated.py`, so their fields, types and constraints match the server. They are frozen Pydantic models, and nested collections are tuples. Each `Clash` has a generation-local ID, a time fraction within the turn, and `ClashParticipant` records containing group IDs, teams, contact positions, radii, and soldier counts before and after combat. Castle and group IDs are never reassigned within a game, so a group ID in a clash never refers to a later group. Transport identity and submission status stay inside the client rather than leaking into the strategy API. `client.generation`, `start()`, and `refresh()` expose the current `Generation` snapshot.

`Castle` and `SoldierGroup` provide `distance(other)` for straight-line distance in map units and `compare_team(other)` returning `TeamType.friendly`, `TeamType.neutral`, or `TeamType.enemy` (import `TeamType` from `island_battles_client`). A target with team color `0` is always neutral, including when both entities are neutral. Otherwise, equal team colors are friendly and different colors are enemies. These utilities are the only model code written by hand; they live in `_entities.py`, which the generated classes extend.

`SoldierGroup.destination_castle` is the castle with `destination_castle_id` in the same generation, linked when the generation is validated; a generation whose group points at a missing castle is rejected. `distance_left()` takes no arguments and measures to that castle. Distances are floating-point map units, not travel times or generation counts; speed and clash radius do not affect them.

```python
origin, target = generation.castles[:2]
distance = origin.distance(target)
relationship = origin.compare_team(target)
for group in generation.soldier_groups:
    remaining = group.distance_left()
```

The client owns authentication, the personal player-channel subscription, reconnect recovery, state reconciliation, and turn completion. It calls `on_generation` serially with a fresh `CommandReceiver` scoped to that callback. Call `await commands_receiver.send_soldier_group(origin_castle_id, destination_castle_id, soldier_amount)` to send one group; it returns `None`. To send multiple groups, await one call per group. Arguments are validated against the generated `SendSoldierGroupParameters` before any request: castle IDs must be non-negative integers and the soldier amount a positive integer, otherwise `pydantic.ValidationError` is raised. The receiver expires when the callback returns or raises; later calls raise `GameReceiverClosedError`. Returning successfully completes the turn automatically, even without sending any soldiers; an exception or cancellation leaves it open.

Castles and soldier groups carry `upgrades`, which maps every `UpgradeType` (`UpgradeType.speed`, `UpgradeType.clash_stop_generations`) to its level; a group copies its origin castle's levels when it leaves. `UpgradeType` is a `StrEnum`, so `castle.upgrades["speed"]` works too. The costs live on the server: `start()` reads `GET /game/upgrades` once, before the first callback, and keeps it as `client.upgrades`, a read-only `UpgradeTable` with `max_level(upgrade_type)`, `cost(upgrade_type, level)` (the soldiers paid to reach that level) and each type's `Upgrade` in `upgrades`. If that read fails, `start()` fails like any other start request.

```python
cost = commands_receiver.upgrade_cost(castle.id, UpgradeType.speed)
if cost <= castle.soldiers:
    await commands_receiver.upgrade_castle(castle.id, UpgradeType.speed)
```

`commands_receiver.upgrade_cost(castle_id, upgrade_type)` returns what the next `upgrade_castle` of that castle and type costs. It sends no request and counts the levels this callback already bought. `await commands_receiver.upgrade_castle(castle_id, upgrade_type)` buys that level as its own request; a castle may be upgraded several times in one turn, and each call buys the next level. Before any request, both raise `pydantic.ValidationError` for invalid arguments, including an unknown upgrade type, `CastleNotFoundError` for a castle that is not on the callback's board, and `UpgradeMaxLevelError` when the castle's level plus the levels this callback bought is already the maximum. `upgrade_castle` also raises `CastleNotOwnedError` for a castle your team does not own, and `InsufficientSoldiersError` when the cost exceeds the castle's soldiers minus everything this callback already sent or paid from it; spending the exact balance is allowed.

These four errors subclass `GameCommandError`, a `GameError` with `status` and `detail`. Raised locally, before any request, their `status` is `None`. The server can still reject what the callback's board doesn't show, such as a teammate's reservations this turn; the SDK then raises the same classes from the server's problem type, with its HTTP status. `send_soldier_group` raises them for the same problems: a missing castle, an origin your team doesn't own and too few soldiers. Other rejections raise a plain `GameError`. In team games, expect these errors from the server: teammates share castles, and the callback's board doesn't show what they reserved this turn. A strategy that lets one escape its callback leaves the turn open and stops the client, so catch `GameCommandError` around commands that may compete with a teammate's, as [examples/player.py](examples/player.py) does.

Castle and group upgrades, the `upgrade_castle` command and the problem types are a breaking change to the generated models: the `PublicGenerationCommand` model became a union of `SendSoldierGroupCommand` and `UpgradeCastleCommand`, and `Castle` and `SoldierGroup` require `upgrades`. Earlier versions of this client fail to read new boards. Upgrade.

When the game ends, the client receives the final generation with its outcome, from the `game.ended` event or an HTTP read. It does not call `on_generation` for that generation. It calls the optional `async on_game_ended(outcome, generation)` once with the `GameOutcome` (`reason`, `winner_team_color`, which is `None` for a draw, and a `summary` sentence) and the final board, and then `wait()` and `run()` return. `client.outcome` holds the outcome afterwards. A client started on a finished game ends the same way. This is a breaking change: earlier versions of this client ignore `game.ended`, so at the end of a game they either wait forever or play the final generation and fail with a 409. Upgrade.

Each call is pinned to its callback's generation. The SDK creates the request UUID internally and reuses the exact request when retrying a transport failure. A subsequent call, even with identical parameters, sends a new group. Player code only supplies the castle IDs and the soldier amount or upgrade type; authentication, request IDs, and completion belong to the SDK. The SDK also deduplicates repeated events and reconciles authorized HTTP state after subscription or reconnect. No periodic polling is enabled by default; `refresh()` can request an explicit read.

The full HTTP contract, authorization rules, reservation checks, retry behavior and status codes are documented in the [backend README](../../backend/README.md). The browser's **New demo lobby** uses its own JavaScript player strategy. See the [demo walkthrough](../../backend/README.md#browser-demo).

## Regenerating the models

Change the Go API types in `backend/models`, then export the document and regenerate:

```sh
go run ./backend/cmd/export-openapi --output backend/api/openapi.json
uv run --project clients/island-battles-python/tools --locked python clients/island-battles-python/scripts/generate.py
```

Pass `--check` to `generate.py` to fail when either file is stale.

## Verification

Behavior is verified by the language-agnostic [client conformance suite](../conformance/README.md), through [conformance/adapter.py](conformance/adapter.py); there are no Python-only tests. From the repository root:

```sh
uv run --project clients/conformance --locked pytest clients/conformance/tests/test_models.py
./scripts/test-client-conformance.sh
```

The SDK, adapter and example are checked with Ruff and ty from this directory:

```sh
uv run --locked ruff check island_battles_client examples scripts conformance
uv run --locked ruff format --check island_battles_client examples scripts conformance
uv run --locked ty check island_battles_client examples conformance --error-on-warning
```
