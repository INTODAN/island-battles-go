# Island Battles Go SDK

Write a bot for [Island Battles](../../README.md) in Go. Island Battles is a real-time strategy game for teams: every team starts with a castle on an island map, castles grow one soldier a turn, and you win by sending soldier groups to take every other castle. Each turn, every player's bot looks at the board and answers with orders.

With this SDK, a bot is **one Go function**: it gets the turn (the board, your team, the upgrade cost table and what it remembered last turn) and returns its commands, a few game log lines and what it wants to remember. Your program's `main` hands that function to `islandbattles.Main`, which gives the program three subcommands:

- `serve` runs the bot on your machine, at `http://127.0.0.1:41414`. The game in your browser sends it each turn and submits its answer.
- `check` plays the bot once on a small fixture turn and tells you what is wrong with it.
- `fixture` prints that fixture turn as JSON, to try `serve` with curl.

The SDK uses only Go's standard library. It needs Go 1.25 or newer. There is a [Python SDK](../island-battles-python/README.md) too, with the same ideas and names; the [table below](#python-names-and-go-names) maps one to the other.

## Install

```sh
mkdir mybot && cd mybot
go mod init mybot
go get github.com/INTODAN/island-battles-go/clients/island-battles-go@latest
```

Each release of the game tags the SDK as `clients/island-battles-go/vX.Y.Z`, so `@vX.Y.Z` gets the SDK of that release. The package name is `islandbattles`; the examples import it with that name spelled out:

```go
import islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
```

To use a checkout of this repository instead of a release, point your `go.mod` at it: `go mod edit -replace github.com/INTODAN/island-battles-go/clients/island-battles-go=../island-battles-go/clients/island-battles-go`.

## Your first bot

Save this as `main.go` in your module:

```go
package main

import (
	"context"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

func play(_ context.Context, turn islandbattles.Turn) (islandbattles.Result, error) {
	for _, own := range turn.Generation.Castles {
		for _, other := range turn.Generation.Castles {
			if own.TeamColor == turn.TeamColor && own.Soldiers > 0 && own.CompareTeam(other) == islandbattles.Enemy {
				return islandbattles.Result{Commands: []islandbattles.Command{islandbattles.SendSoldierGroup(own.ID, other.ID, 1)}}, nil
			}
		}
	}
	return islandbattles.Result{}, nil
}

func main() {
	islandbattles.Main(play)
}
```

Each turn it sends one soldier from its first castle that has any to the first enemy castle. Check it, then serve it:

```sh
go run . check
go run . serve
```

`check` prints a line such as `ok: 1 commands, 0 log lines, memory 4 of 65536 bytes, 0.00 s`. `serve` keeps running until you press Ctrl+C. **Go doesn't reload code:** after you change the bot, stop `serve` and start it again. A slightly longer version that also logs and remembers its turns is [examples/firstbot](examples/firstbot/main.go).

### Play it in a game

1. Start `go run . serve` and leave it running.
2. Open the game's lobby page in Chrome, Edge or Firefox (Safari can't reach a bot on `127.0.0.1` from an HTTPS page), join a lobby and, in the bot picker, choose **your localhost bot** before you get ready. The [backend README](../../backend/README.md#choosing-a-bot) explains the choice; a player who picks a default level is played by the server instead.
3. When the game starts, your browser tab sends each turn to your bot and submits its answer. Keep the tab open: it relays the turns. If your bot fails, the tab shows the error.

A turn has a deadline (30 seconds unless the lobby's host chose another): a bot that doesn't answer in time makes its whole team lose.

## The turn

`play` has the type `islandbattles.Play`:

```go
type Play func(ctx context.Context, turn islandbattles.Turn) (islandbattles.Result, error)
```

- `turn.Generation` is the board: `Castles`, `SoldierGroups` and the `Clashes` of the last turn, and its `ID`, which grows by one every turn.
- `turn.TeamColor` is your team; `islandbattles.NeutralTeamColor` (0) is no team. Teammates share castles.
- `turn.Upgrades` is the upgrade cost table: every upgrade type with each level's `Value` and `SoldierCost`. It is the same for the whole game.
- `turn.Memory` is what your bot remembered last turn ([memory](#memory)), and `turn.GameID` the game.
- `turn.TurnDeadlineAt` is when the turn times out, or `nil` when there is no limit.
- `ctx` is cancelled when `serve` or `check` stops waiting for the turn. A bot that may think for long should watch it.

Return an `error` to pass the turn: `serve` answers it as an error, and the game shows it to you. A panic is reported the same way, with its stack.

A **castle** has an `ID` that never changes, its `TeamColor`, `Soldiers`, a position `X`, `Y` in map units, and `Upgrades`, the level of every upgrade type (`castle.Upgrades.Level(islandbattles.SpeedUpgrade)` is 1 for a type the map doesn't list). A **soldier group** has the same, plus `OriginCastleID`, `DestinationCastleID` and `ClashRadius`; its upgrades are its origin castle's levels when it left. Every turn each castle grows one soldier, then the groups move (a group of speed level 1 moves 0.5 map units a turn) and land in ID order: a friendly group reinforces the castle, and a hostile one takes it when it has more soldiers than the castle.

### Helpers

| Helper | What it does |
|---|---|
| `castle.Distance(other)`, `group.Distance(other)` | Straight-line distance to a castle or group, in map units, not turns. |
| `castle.CompareTeam(other)`, `group.CompareTeam(other)` | `islandbattles.Friendly`, `Neutral` or `Enemy`. A neutral `other` is always `Neutral`. |
| `group.DestinationCastle(turn.Generation)` | The castle the group is marching to, and `false` if the board doesn't have it. |
| `group.DistanceLeft(turn.Generation)` | The distance still to go to that castle. |
| `turn.Generation.Castle(id)` | A castle by its ID. |
| `islandbattles.NextUpgradeCost(turn, castle, upgradeType)` | What the castle's next level of a type costs in soldiers, from `turn.Upgrades`, and `false` at the maximum level. Levels bought earlier in the same result are not counted. |
| `turn.UpgradeLevel(upgradeType, level)` | One level of the table: its `Value` (for speed, map units per turn) and `SoldierCost`. |

## The result

`islandbattles.Result` has three fields, and every one may stay empty:

```go
result := islandbattles.Result{
	Commands: []islandbattles.Command{
		islandbattles.SendSoldierGroup(originCastleID, destinationCastleID, 12),
		islandbattles.UpgradeCastle(castleID, islandbattles.SpeedUpgrade),
	},
	Logs: []islandbattles.LogLine{islandbattles.Logf("attacking castle %d", destinationCastleID)},
}
result.Memory, err = islandbattles.NewMemory(state)
```

- `SendSoldierGroup(originCastleID, destinationCastleID, soldierAmount)` sends soldiers from a castle your team owns to any other castle. `soldierAmount` must be at least 1.
- `UpgradeCastle(castleID, upgradeType)` buys a castle the next level of `islandbattles.SpeedUpgrade` (how far its groups move per turn) or `islandbattles.ClashStopGenerationsUpgrade` (how many turns its groups stay stopped after a clash). The castle pays the level's cost in soldiers.
- **Commands run in order, and the server checks them against the board**, counting what your teammates already sent this turn. If any command is invalid, such as sending more soldiers than a castle has, the server rejects the **whole result**. Count what your commands spend, upgrades included. At most `islandbattles.MaxCommands` (1,000) commands.

## Memory

The bot keeps nothing between turns itself: `serve` may be restarted, and the server may play your seat on another machine. What it needs next turn goes into its memory, a JSON object of at most `islandbattles.MemoryLimitBytes` (64 KiB, as compact JSON) that the server stores and gives back as the next turn's `turn.Memory`. Keep it in a struct of your own:

```go
type plan struct {
	Turn   int  `json:"turn"`
	Target uint `json:"target"`
}

func play(_ context.Context, turn islandbattles.Turn) (islandbattles.Result, error) {
	var remembered plan
	if err := turn.Memory.Decode(&remembered); err != nil {
		remembered = plan{} // a memory from an older version of the bot: start over
	}
	remembered.Turn++
	memory, err := islandbattles.NewMemory(remembered)
	return islandbattles.Result{Memory: memory}, err
}
```

`Decode` leaves your struct as it is on the first turn, when there is no memory. `NewMemory` fails for a value that isn't a JSON object, or is over 64 KiB. A result without memory clears it: the next turn starts with none.

## Logging

The lines in `Result.Logs` are stored in the game log, which the replay in the lobby page shows with each turn: your team's lines while the game runs, and every team's once it has ended.

- `islandbattles.Log(message)` and `islandbattles.Logf(format, arguments...)` build an `info` line; `islandbattles.LogAt(level, message)` a line of `islandbattles.LogDebug`, `LogInfo`, `LogWarning` or `LogError`.
- Their logger name is `islandbattles.PlayerLogger`, `island_battles.player`. Set the line's `Logger` field to name another, of at most 100 characters.
- The server stores at most **100 lines per player per turn** (`islandbattles.MaxLogLines`), each of **1 to 500 characters** (`islandbattles.MaxLogMessageLength`). The helpers cut a longer message so that, ending in `… (cut at 500 characters)`, it is 500 characters, and replace a NUL character or invalid UTF-8 with `�`. A result with more than 100 lines, or an empty message, is invalid.

Anything your program prints, with `fmt` or `log`, goes to the terminal that runs `serve`, not to the game log.

## check

```sh
go run . check
go run . check --turn myturn.json
```

plays the bot once on the fixture turn (or on a `BotTurn` JSON file of your own) and exits with status 1 and a `FAIL:` message if the bot returns an error, panics, returns an invalid result (`islandbattles.ValidateResult` says why), returns memory over 64 KiB, or takes longer than 2 seconds. Otherwise it prints how many commands and log lines the bot returned, the size of its memory and how long it took. Use it before you serve, and in your own tests: `islandbattles.FixtureTurn()` is the same turn, and `islandbattles.ValidateResult(result)` the same check.

## serve

```sh
go run . serve [--port 41414] [--turn-timeout 8s] [--allow-origin https://game.example.org]
```

serves the bot on `http://127.0.0.1:41414`, the port the game looks for a localhost bot on. Build the program once (`go build -o mybot .`) to start it faster.

- `POST /turn` takes a `BotTurn` as JSON (`Content-Type: application/json`, at most 2 MiB) and answers a `BotTurnResult`. A body that isn't a turn answers `422`, a body over 2 MiB `413`, one without a length `411`, and a POST that isn't JSON `415`. A bot that returns an error, panics or returns an invalid result answers `500` with the error in the body, so the game can show it. A turn that takes longer than `--turn-timeout` (default 8 s, under the lobby's shortest turn timeout of 10 s) answers `504`, and the server keeps serving; a bot stuck in a loop keeps its goroutine until you stop the server.
- `GET /health` answers `{"name": "mybot", "ok": true}`, with your program's name, so the game can show that your bot is connected.
- **Browsers:** the server answers CORS preflights, with `Access-Control-Allow-Private-Network: true` for Chrome's Private Network Access, only for the game's origins: `http://localhost:8888` (a local backend), `http://localhost:3000` (the web client's dev server), `https://island-battles.elidan.org` (the deployed game), and `tauri://localhost` and `http://tauri.localhost` (the desktop app). Add another with `--allow-origin` (repeatable). A request from another origin, or with a `Host` other than `127.0.0.1:<port>` or `localhost:<port>` (against DNS rebinding), gets `403`, and the bot doesn't run. The server closes the connection after every answer.

Try it with curl:

```sh
go run . fixture > turn.json
curl -X POST localhost:41414/turn -H 'Content-Type: application/json' -d @turn.json
curl localhost:41414/health
```

## Examples

| Example | |
|---|---|
| [examples/firstbot](examples/firstbot/main.go) | The first bot above, with a log line and a turn counter in its memory. |
| [examples/level1](examples/level1/main.go) | The default bot of level 1: one soldier at a time from its first castle with soldiers to the first enemy castle. |
| [examples/level2](examples/level2/main.go) | Level 2: each castle attacks the nearest castle it can take, forecasting the defenders when its group lands. |
| [examples/level3](examples/level3/main.go) | Level 3: level 2's attacks, plus a garrison against incoming groups and nearby enemies, help for threatened castles, attacks from several castles timed to land together (remembered in its memory), and speed upgrades. |

The three levels are the **default bots** that the game's server plays when a player picks a level in the lobby: the server runs exactly this code, from [examples/defaultbots](examples/defaultbots). They are deterministic, log one or two lines a turn with the logger `island_battles.bots.level1` to `level3`, and have tests in [defaultbots_test.go](examples/defaultbots/defaultbots_test.go). Play against them, or copy one as the start of your own:

```sh
go run ./examples/level3 check
go run ./examples/level3 serve
```

## Python names and Go names

| Python (`island_battles_client`) | Go (`islandbattles`) |
|---|---|
| `def play(turn): ...` | `func play(ctx context.Context, turn islandbattles.Turn) (islandbattles.Result, error)`, of type `islandbattles.Play` |
| `BotTurn`, `BotTurnResult` | `Turn`, `Result` |
| `Generation`, `Castle`, `SoldierGroup`, `Clash`, `ClashParticipant` | the same names |
| `Upgrade`, `UpgradeLevel`, `UpgradeType.speed`, `UpgradeType.clash_stop_generations` | `Upgrade`, `UpgradeLevel`, `SpeedUpgrade`, `ClashStopGenerationsUpgrade` |
| `PublicGenerationCommand` | `Command` |
| `GenerationLogLine` | `LogLine` |
| `send_soldier_group(origin, destination, amount)` | `SendSoldierGroup(origin, destination, amount)` |
| `upgrade_castle(castle_id, upgrade_type)` | `UpgradeCastle(castleID, upgradeType)` |
| `next_upgrade_cost(turn, castle, upgrade_type)` (`None` at the maximum) | `NextUpgradeCost(turn, castle, upgradeType)` (`false` at the maximum) |
| `log_line(message, level="info")`, `logger.info(...)` | `Log(message)`, `Logf(format, ...)`, `LogAt(level, message)` |
| `turn_result(commands, logs, memory)` | `Result{Commands: ..., Logs: ..., Memory: ...}` |
| `turn.memory` (a dict), `memory={...}` | `turn.Memory.Decode(&state)`, `NewMemory(state)` |
| `MEMORY_LIMIT_BYTES` | `MemoryLimitBytes` |
| `NEUTRAL_TEAM_COLOR` | `NeutralTeamColor` |
| `TeamType.friendly`, `.neutral`, `.enemy` | `Friendly`, `Neutral`, `Enemy` |
| `entity.distance(other)`, `entity.compare_team(other)` | `entity.Distance(other)`, `entity.CompareTeam(other)` |
| `group.destination_castle`, `group.distance_left()` | `group.DestinationCastle(generation)`, `group.DistanceLeft(generation)` |
| `fixture_turn()` | `FixtureTurn()` |
| `BotMemoryTooLargeError`, `InvalidBotResultError` | `ErrMemoryTooLarge`, `ErrInvalidResult`, from `ValidateResult` |
| `island-battles-bot serve mybot:play` | `go run . serve` |
| `island-battles-bot check mybot:play` | `go run . check` |
| `island-battles-bot fixture` | `go run . fixture` |

Two differences: Go has no hot reload, so restart `serve` after a change; and logging goes through `Result.Logs` only, while Python also stores what the bot logs with its `logger`.

## Developing the SDK

The SDK is its own Go module in this repository, connected to the others by the root `go.work`. From the repository root:

```sh
go vet ./clients/island-battles-go/...
golangci-lint run --timeout=5m ./clients/island-battles-go/...
go test ./clients/island-battles-go/...
```

The types are written by hand. Tests keep them equal to the contract the server publishes: [contract_test.go](contract_test.go) checks every property of `BotTurn`, `BotTurnResult` and the schemas they reference in the [player API document](../../backend/api/openapi.json) against the Go types' JSON names, both ways, with the enums and limits; the backend's [bots tests](../../backend/bots/bots_test.go) send a full turn and result of the server through these types and back, unchanged. The fixture turn, [fixture_turn.json](fixture_turn.json), is the Python SDK's, which a [Python test](../island-battles-python/tests/test_fixture.py) checks. The SDK never imports the backend or the lobby; the backend imports the SDK, to play the default bots.
