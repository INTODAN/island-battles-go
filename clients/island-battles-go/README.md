# Island Battles Go SDK

Write a bot for [Island Battles](../../README.md) in Go. Island Battles is a real-time strategy game for teams: every team starts with a castle on an island map, a castle that a team owns grows its production every turn (a neutral castle keeps its soldiers until a team captures it), and you win by sending soldier groups to take every other castle. Each turn, every player's bot looks at the board and answers with orders.

**Rated bots are hosted bots (#400), a breaking change of the API.** In a rated lobby a named bot must be a hosted bot, `{"bot": {"kind": "hosted", "name": "Kraken"}}` in the member settings, which plays at the URL that its owner registered with the name in the 3D client's account page; a named localhost or uploaded bot is refused with 409 `game_requirement` of type `urn:island-battles:problem:rated-hosted-bot-required`. A listed bot name's URL is public on the leaderboard, so anyone may play against it as a hosted bot seat of an unrated lobby. The generated models change with it: `MemberBot.url` may be left out of a named hosted bot, and `Competitor`, `LeaderboardEntry` and `LeaderboardCompetitor` have a `url`. What to do: upgrade the SDK. `serve` and `run` are unchanged, and lobbies that aren't rated take every bot as before.

**Rated lobbies, a breaking change of the state (#365).** A lobby's host can make it rated (`{"rated": true}` in its `game_settings`). `GenerationState` has a required `rated`, and `GenerationState.bot` a `name`: the bot name that a localhost, hosted or uploaded bot plays as in a rated game, one of its player's bot names (`{"bot": {"kind": "localhost", "name": "Kraken"}}` in the member settings). Only players signed in with Google in the 3D client play in a rated lobby, so `run`, which plays with a guest credential, can't: a guest who gets ready there is refused with 409 `game_requirement` of type `urn:island-battles:problem:rated-sign-in-required` and the detail "This lobby is rated. Sign in with Google in the 3D client to play in it.", which the lobby page shows. What to do: upgrade the SDK, whose state check requires `rated`; play your bot in lobbies that aren't rated.

**Leaving a real-time game, a breaking change of the outcome and the state (#347).** In a real-time game a player can resign (`POST /game/from-lobby/{lobby_id}/resign`), a player the server hasn't heard from for the lobby's turn limit has left, and a game that nobody plays for that long is abandoned. A team that resigns or leaves is out: its castles turn neutral and keep their soldiers, and its soldier groups vanish, with no clash. The outcome has a new reason, `abandoned`, and `departures`, the teams that resigned or left; `GenerationState` has `departures` too. Every game request made as your player, with a player token or a submit token and late ones included, counts as hearing from it, so a bot that answers each turn it is given never leaves; one that stops answering does. A bot whose team resigned or left has nothing to play: its batches are refused with `team-out`, and `run` stops there. What to do: upgrade the SDK. Older ones read the new states, which only add a field, but fail on the final state of an abandoned game, whose reason they don't know. This SDK needs a server that sends `departures`.

**In a real-time game a slow bot passes its turn, and a late batch has a problem type (#353).** A batch for a turn that has ended is refused with 409 and the problem type `urn:island-battles:problem:stale-generation`, in both kinds of game, which `ErrStaleGeneration` matches; it is also an `ErrRejected`, as the untyped 409 was. In a real-time game `run` takes it as a passed turn: it writes one line, such as `the bot was slower than the turn pace of 1s on generation 12, so it passes this turn`, sends no failure batch and plays the next turn instead of stopping. A bot that returns an error once its `ctx` has ended passes the same way; when the error is its own, such as a panic, rather than the `ctx`'s, `run` also writes the failure and its traceback as a warning. After a pass `run` asks the server to end the turn at its deadline, as it does after completing one, so the game goes on when nobody else calls. In a game that waits for every player, `run` stops on it as before. Both `serve` and `run` now end the bot's `ctx` 1 s before the turn's deadline, or a quarter of the time left when that is less, so `Play` has three quarters of a short turn: 375 ms at a pace of 0.5 s, 750 ms at 1 s, 1.5 s at 2 s and 4 s at 5 s. Before, `serve` gave it nothing at a pace of 1 s or less and passed every turn, and `run` ended `ctx` at the deadline itself, so an answer given when it ended always came late. The same margin applies to a game that waits for every player when less than 4 s of the turn is left. What to do: upgrade the SDK before your bot plays a real-time game, and keep `Play` under three quarters of the pace.

**Real-time games, a breaking change of the state (#339).** A game with a player who plays by hand is real-time: each turn ends after the lobby's turn pace (`turn_pace_milliseconds`, 1 s by default, 0.5 to 5 s), and the server then plays it with the orders that arrived. A bot that answers later passes that turn, and its late batch is refused with 409 as a stale generation; nobody loses for a missed turn. `GenerationState` has the required `turn_pace_milliseconds`, `null` in a game of bots, which keeps the turn limit. `turn_deadline_at` is the turn's end in both kinds of game. What to do: upgrade the SDK, whose state reads fail without the field, and keep `Play` well under the pace if your bot may meet a hand player.

**A new bot kind, `manual`, for a player who plays by hand, a breaking change (#338).** `GenerationState.bot.kind` can be `manual`: a person plays the seat in the 3D client, so nothing plays or relays it. `run` refuses that seat with "this seat is played by hand in the 3D client". What to do: upgrade the SDK, whose state reads fail on a kind they don't know; nothing changes for a localhost bot's seat.

**Turns carry their turn number and the game's last generation (#309).** `Turn` has `TurnNumber`, 1 on the game's first turn and one more each turn, and `MaxGenerations`, the game's last generation (300 unless the host chose another length): the turn that creates it ends the game, so `turn.MaxGenerations - turn.TurnNumber` turns are left, this one included. `turn.Generation.ID` is the server's ID of the generation, which every game shares, so it doesn't start at 1 and isn't the turn number. Older SDKs ignore the new fields, but this one requires them: `DecodeTurn` and `check --turn` reject a turn without `turn_number` and `max_generations`. What to do: use `turn.TurnNumber` instead of counting turns or reading `Generation.ID`, and in your tests set `TurnNumber` and `MaxGenerations` on the turns you build, or build them from `FixtureTurn()`; print the fixture again (`go run . fixture`) for a turn file saved from an older SDK.

**Two new upgrades and a burst command, a breaking change of the turn's board (#281).** `generation_amount` sets what a castle produces each turn (1 to 4 soldiers), and `burst_potential` the largest burst it may order (0, 4, 8 or 16 soldiers); both cost 10, 20 and 30 soldiers for levels 2 to 4. The new command `burst_castle` orders a burst: the very next generation adds it on top of the castle's production and makes it the castle's `burst_debt`, which later production repays before the castle grows again. Every castle now has `burst_debt`, and every `upgrades` map lists the four types. What to do: upgrade the SDK, since older ones refuse every turn whose upgrades list the new types, and count each castle's production and debt where your bot forecasts growth.

**This is a rule change, and it breaks bots that count on neutral castles growing.** A neutral castle no longer gains a soldier each turn; it keeps its soldiers until a team captures it and grows from the next turn on, like any castle a team owns. The API is the same, but a bot that forecasts a neutral castle's defenders with growth overestimates them. What to do: count growth only for castles with a team (the default bots in `examples/defaultbots` show the forecast).

**Localhost bots take a port, and the host can seat several of them, a breaking change for clients of the game API (#286).** `GenerationState` has a required `relayed_bots`, the localhost bot seats that you added as the host, which your browser tab relays, and `GenerationState.bot` a localhost bot's `port`. A player's localhost bot may serve on any port from 1024 to 65535 (`serve --port N`) and say so in the lobby, and the host can seat their own localhost bots, each on its own port, to play them against each other. What to do: upgrade the SDK, whose models read the new fields; `run` still plays one person's seat.

**Two more bot kinds that the server plays, `hosted` and `uploaded` (#222).** Where the server offers them (`server_bots` in `GET /bots/defaults`), a player's bot, and a bot seat's, can also be their own bot server at a public URL, `{"bot": {"kind": "hosted", "url": "https://…"}}`, or one of their bot packages in the lobby, `{"bot": {"kind": "uploaded", "package_id": "…"}}`. `GenerationState.bot.kind` can then be `hosted`, with the URL's `host`, or `uploaded`. `run` refuses those seats, as it does a default bot's, and says which bot the server plays. An older SDK stops on such a seat with a protocol error instead; nothing changes for a localhost bot's seat.

**The official lobby moved to its own site, `https://lobby.elidan.org`, a breaking change for clients of the official game (#232).** The game stays at `https://island-battles.elidan.org`, which no longer serves the lobby: its `/lobby/v1/*` answers `410 Gone`, and its `/lobby/ui/` redirects to the same path on `https://lobby.elidan.org`. What to do: give `run` both addresses, `--game-url https://island-battles.elidan.org --lobby-url https://lobby.elidan.org` (`URLs{Game: …, Lobby: …}` for `NewClient`); `--base-url https://island-battles.elidan.org` no longer works. Upgrade the module for `serve` too: its default origins now include the lobby page's, `https://lobby.elidan.org`, and an older `serve` needs `--allow-origin https://lobby.elidan.org` before the lobby page can deliver turns to it.

**The lobby and the game are separate sites, a breaking change for the client's configuration (#228).** The lobby gets its own address (such as `https://lobby.example.org`), apart from the game's (such as `https://island-battles.example.org`). `NewClient` takes `islandbattles.URLs{Game: …, Lobby: …}`: it sends the lobby's routes (`/lobby/v1/*`: player tokens and the Centrifugo connection token) to `Lobby`, and the game's (`/game/*`, `/generation/*`) to `Game`; `islandbattles.BaseURL(url)` is the shorthand for one server that serves both. `run` takes `--game-url` and `--lobby-url`, or reads `ISLAND_BATTLES_GAME_URL` and `ISLAND_BATTLES_LOBBY_URL`, and `--base-url` stays as the shorthand that sets both. What to do: upgrade the module, and pass `URLs` (or `BaseURL(…)`) where you passed a base URL.

**Turn deliveries are a breaking change for `serve`; upgrade the SDK.** `POST /turn` now takes a `TurnDelivery` (the turn plus a one-turn submit token), answers `202` at once, and submits the bot's answer to the game itself; it no longer answers a `BotTurnResult`. The 3D web client and the lobby page deliver turns this way, so an older SDK's `serve` doesn't work with them, and this one's doesn't work with older game clients. A bot that returns an error or runs out of time now passes its turn instead of leaving it open. What to do: upgrade the module and rebuild your bot; your `Play` function doesn't change.

With this SDK, a bot is **one Go function**: it gets the turn (the board, your team, the upgrade cost table and what it remembered last turn) and returns its commands, a few game log lines and what it wants to remember. Your program's `main` hands that function to `islandbattles.Main`, which gives the program five subcommands:

- `serve` runs the bot on your machine, at `http://127.0.0.1:41414`. The game in your browser delivers it each turn, and it submits its answer to the game itself.
- `check` plays the bot once on a small fixture turn and tells you what is wrong with it.
- `fixture` prints that fixture turn as JSON, for `check --turn`.
- `run` plays your seat straight against the server, without a browser.
- `worker` plays turns for the server's runners over stdin and stdout; [`island-battles-package`](#package-your-bot) zips the program for them.

The SDK needs Go 1.25 or newer. Besides the standard library it uses one module, [centrifuge-go](https://github.com/centrifugal/centrifuge-go), the realtime client `run` connects with. There is a [Python SDK](../island-battles-python/README.md) too, with the same ideas and names; the [table below](#python-names-and-go-names) maps one to the other.

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
2. Open the lobby page, `https://lobby.elidan.org/lobby/ui/` (or the 3D client, `https://island-battles.elidan.org/play/`), in Chrome, Edge or Firefox (Safari can't reach a bot on `127.0.0.1` from an HTTPS page), join a lobby and, in the bot picker, choose **your localhost bot** before you get ready. The [backend README](../../backend/README.md#choosing-a-bot) explains the choice; a player who picks a default level, a hosted bot or an uploaded bot is played by the server instead.
3. When the game starts, your browser tab delivers each turn to your bot, which submits its answer to the game itself. Keep the tab open: it delivers the turns. If your bot fails, it passes the turn, and the game's **Bot logs** show the error.

A turn has a deadline (30 seconds unless the lobby's host chose another): a bot that doesn't answer in time makes its whole team lose. In a real-time game, one with a player who plays by hand, the deadline is the turn pace instead (1 second unless the host chose another), and a bot that doesn't answer in time passes the turn.

### Play it without a browser: run

```sh
go run . run --game-url https://island-battles.elidan.org --lobby-url https://lobby.elidan.org --token=<credential> --lobby-id <lobby ID> [--log-file bot.log] [--log-level info]
```

plays your seat of a lobby's game to its end, straight against the server, with no browser tab; build the program once (`go build -o mybot .`) to start it faster. `--game-url` is the game's address and `--lobby-url` the lobby's: `https://island-battles.elidan.org` and `https://lobby.elidan.org` for the official game, `http://localhost:8888` and `http://localhost:8890` for a local backend and lobby. Without the flags, `run` reads `ISLAND_BATTLES_GAME_URL` and `ISLAND_BATTLES_LOBBY_URL`. `--base-url` sets both, for a server that serves the game and the lobby together; the official game's are two sites, so give it both flags. `--token` is your player's guest credential with the lobby, and `--lobby-id` the lobby whose game it plays. Choose **your localhost bot** in the lobby before you get ready, as for `serve`. `run` reads the game's state, hears of each new turn on your player's realtime channel, calls your bot once per turn and submits its answer, and prints `game ended at generation 300: …` when the game ends, with the final generation's turn number. A request that gets no response is sent again identically, and a turn missed while the connection was down is read again after it reconnects.

While your bot waits for the other players on a turn with a deadline, `run` asks the server to apply the deadline a second after it, which ends the game when someone didn't answer. The bot's `ctx` is cancelled at the turn's deadline; `run` still sends what the bot then returns.

`run` stops with `FAIL: …` and status 1 when your bot returns an error, panics or returns an invalid result, or when the server rejects the result, for example because a castle doesn't have the soldiers it sends. It sends the error and the turn's log lines first, and leaves the turn open: fix the bot and start `run` again for the same turn. In a real-time game, a result that comes after the turn's end, which the server refuses as a stale generation, and an error that your bot returns once its `ctx` ended, are a passed turn instead: `run` writes that the bot was slower than the turn pace (and the bot's own failure, such as a panic's traceback, when the error isn't the `ctx`'s), asks the server to end the turn at its deadline, and plays on. Its log goes to stdout, and to `--log-file` when you give one.

In your own program, `islandbattles.NewClient(islandbattles.URLs{Game: gameURL, Lobby: lobbyURL}, credential, lobbyID, play, options...)` is the same client, with the guest credential, which it sends only to the lobby: the game's routes take a player token, which it gets from the lobby (`POST /lobby/v1/player-tokens`), renews at 80 % of its life, and once more when the game answers 401 (a breaking change of the game API, #218; upgrade the SDK). It has `Run(ctx)`, or `Start(ctx)`, `Wait(ctx)` and `Close(ctx)`, `Refresh(ctx)`, and `Generation()`, `TeamColor()` and `Outcome()`. `WithGameEnded` reports the end; `WithLogLevel`, `WithLogFile`, `WithLogOutput` and `WithHTTPClient` configure it. Its errors are `*islandbattles.GameError` values: test them with `errors.Is(err, islandbattles.ErrInsufficientSoldiers)` (also `ErrCastleNotFound`, `ErrCastleNotOwned`, `ErrUpgradeMaxLevel`, `ErrBurstUnavailable`, `ErrBurstAbovePotential`, `ErrMemoryTooLarge`, `ErrGameEnded`, `ErrTeamOut`, `ErrAuthentication`, `ErrTransport`, `ErrProtocol`, `ErrClientClosed` and `ErrRejected`), and read the HTTP status with `errors.As`.

## The turn

`play` has the type `islandbattles.Play`:

```go
type Play func(ctx context.Context, turn islandbattles.Turn) (islandbattles.Result, error)
```

- `turn.Generation` is the board: `Castles`, `SoldierGroups` and the `Clashes` of the last turn, and its `ID`, the server's ID of the generation, which increases with every turn but isn't the turn number.
- `turn.TurnNumber` is the turn's number in the game, 1 on its first turn, and `turn.MaxGenerations` the game's last generation: the turn that creates it ends the game, so `turn.MaxGenerations - turn.TurnNumber` turns are left, this one included.
- `turn.TeamColor` is your team; `islandbattles.NeutralTeamColor` (0) is no team.
- `turn.Upgrades` is the upgrade cost table: every upgrade type with each level's `Value` and `SoldierCost`. It is the same for the whole game.
- `turn.Memory` is what your bot remembered last turn ([memory](#memory)), and `turn.GameID` the game.
- `turn.TurnDeadlineAt` is when the turn times out, or `nil` when there is no limit.
- `ctx` is cancelled when `serve` or `check` stops waiting for the turn (under `serve`, at `--turn-timeout` or before the turn's deadline by 1 s, or by a quarter of the time left when that is less, whichever comes first), and under `run` before the turn's deadline by the same margin as under `serve`. A bot that may think for long should watch it. `islandbattles.Logger(ctx)` is the bot's [logger](#logging).

Return an `error` to pass the turn: `serve` submits no commands, keeps the memory the bot was given, and the game log gets one `error` line from the logger `island_battles.bots` with the error, which the game's **Bot logs** show you. A panic passes the turn the same way, and `serve` prints its stack.

A **castle** has an `ID` that never changes, its `TeamColor`, `Soldiers`, a position `X`, `Y` in map units, `Upgrades`, the level of every upgrade type (`castle.Upgrades.Level(islandbattles.SpeedUpgrade)` is 1 for a type the map doesn't list), and `BurstDebt`, what it still owes for its last burst. A **soldier group** has the same but the burst, plus `OriginCastleID`, `DestinationCastleID` and `ClashRadius`; its upgrades are its origin castle's levels when it left. Every turn each castle that a team owns produces its `generation_amount` (1 soldier at level 1): a burst ordered that turn arrives on top, and burst debt is repaid first, so only what is left over grows the castle. Then the groups move (a group of speed level 1 moves 0.5 map units a turn) and land in ID order: a friendly group reinforces the castle, and a hostile one takes it when it has more soldiers than the castle.

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
| `islandbattles.Production(turn, castle)` | What the castle produces each turn, its `generation_amount` value from `turn.Upgrades`. |
| `islandbattles.BurstPotential(turn, castle)` | The largest burst the castle may order, its `burst_potential` value from `turn.Upgrades`. Levels bought earlier in the same result are not counted. |
| `castle.CanBurst()` | Whether the castle has no burst debt. |

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
- `UpgradeCastle(castleID, upgradeType)` buys a castle the next level of `islandbattles.SpeedUpgrade` (how far its groups move per turn), `islandbattles.ClashStopGenerationsUpgrade` (how many turns its groups stay stopped after a clash), `islandbattles.GenerationAmountUpgrade` (how many soldiers it produces each turn) or `islandbattles.BurstPotentialUpgrade` (the largest burst it may order). The castle pays the level's cost in soldiers.
- `BurstCastle(castleID, soldierAmount)` orders a burst of 1 up to the castle's burst potential, counting the levels bought in the same result, before or after it, for a castle that `CanBurst`. The very next generation adds it on top of the castle's production and makes it the castle's `BurstDebt`, which its later production repays before the castle grows again. A castle bursts at most once a turn, and a capture clears its debt.
- **Commands run in order, and the server checks them against the board**, counting what your earlier batches already sent this turn. If any command is invalid, such as sending more soldiers than a castle has, the server rejects the **whole result**. Count what your commands spend, upgrades included. At most `islandbattles.MaxCommands` (1,000) commands.

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

The game log stores lines with each turn, and the replay in the lobby page shows them: your team's lines while the game runs, and every team's once it has ended. A bot writes lines in two ways.

Log with `islandbattles.Logger(ctx)`, a standard `log/slog` logger named `island_battles.player`:

```go
func play(ctx context.Context, turn islandbattles.Turn) (islandbattles.Result, error) {
	log := islandbattles.Logger(ctx)
	log.Info("attacking", "castle", 4) // stored as "attacking castle=4"
	...
}
```

Under `serve` and `run`, every line it logs during the turn goes to the game log, and to the console (and `run`'s `--log-file`). `slog.LevelDebug`, `LevelInfo`, `LevelWarn` and `LevelError` are the game log's `debug`, `info`, `warning` and `error`; `run --log-level` sets the lowest level logged, and `serve` logs from `info`. Lines logged after the turn, for example from a goroutine that outlives it, reach the console only, and under `check` the logger is `slog.Default()`.

Or return lines in `Result.Logs`; they follow the lines the bot logged:

- `islandbattles.Log(message)` and `islandbattles.Logf(format, arguments...)` build an `info` line; `islandbattles.LogAt(level, message)` a line of `islandbattles.LogDebug`, `LogInfo`, `LogWarning` or `LogError`.
- Their logger name is `islandbattles.PlayerLogger`, `island_battles.player`. Set the line's `Logger` field to name another, of at most 100 characters.
- The server stores at most **100 lines per player per turn** (`islandbattles.MaxLogLines`), each of **1 to 500 characters** (`islandbattles.MaxLogMessageLength`). The helpers and the logger cut a longer message so that, ending in `… (cut at 500 characters)`, it is 500 characters, and replace a NUL character or invalid UTF-8 with `�`. A result with more than 100 lines, or an empty message, is invalid.
- The logger never fails your bot over the limit: once a turn has 99 lines, the SDK holds the next one back, and when more come it stores, in its place, the error `game log limit reached for generation <id>: 100 lines per generation; <n> lines were not stored` from the logger `island_battles.sdk`. The console gets every line, and that error once, when the first line is dropped. Under `run` the count includes the lines an earlier run stored for the same turn.
- When the bot fails, `run` stores the error, and for a panic its traceback, as an `error` line of `island_battles.sdk`.

Anything your program prints with `fmt` or `log`, or logs with `slog.Default()`, goes to the terminal that runs `serve` or `run`, not to the game log.

## check

```sh
go run . check
go run . check --turn myturn.json
```

plays the bot once on the fixture turn (or on a `BotTurn` JSON file of your own) and exits with status 1 and a `FAIL:` message if the bot returns an error, panics, returns an invalid result (`islandbattles.ValidateResult` says why), returns memory over 64 KiB, or takes longer than 2 seconds. Otherwise it prints how many commands and log lines the bot returned, the size of its memory and how long it took. Use it before you serve, and in your own tests: `islandbattles.FixtureTurn()` is the same turn, and `islandbattles.ValidateResult(result)` the same check.

## serve

```sh
go run . serve [--port 41414] [--turn-timeout 8s] [--allow-origin https://game.example.org] [--allow-submit-host game.example.org]
```

serves the bot on `http://127.0.0.1:41414`, the port the game looks for a localhost bot on unless you type another next to your localhost bot in the lobby page or the 3D client. To play several of your bots in one game, serve each with its own `--port` and, as the lobby's host, seat each as a localhost bot with its port (#286); your tab relays the turns of every seat that you added. Build the program once (`go build -o mybot .`) to start it faster. The game delivers each turn to it, and the bot submits its answer to the game itself; nobody waits on the bot:

- `POST /turn` takes a `TurnDelivery` as JSON (`Content-Type: application/json`, at most 2 MiB): `{"turn": <BotTurn>, "submit": {"url", "token", "deadline_at"}}`, which the game client reads from `GET /game/from-lobby/{lobby_id}/turn-delivery` ([turn deliveries](../../backend/README.md#turn-deliveries)). It answers `202 {"accepted":true}` at once, plays the turn in the background, and submits `{game_id, generation_id, request_id, commands, logs, memory, complete: true}` to `submit.url` with `Authorization: Bearer <submit.token>`, a token that submits this one turn and nothing else. A turn delivered again while it is being played is played once. A body that isn't a delivery answers `422`, a body over 2 MiB `413`, one without a length `411`, and a POST that isn't JSON `415`.
- **The bot has `--turn-timeout` (default 8 s)**, or less: its `ctx` ends before `deadline_at` by 1 s, or by a quarter of the time left when that is less, so the answer arrives in time. In a real-time game, whose deadline is one turn pace away, the bot has three quarters of the pace: 375 ms at 0.5 s, 750 ms at 1 s. A slower bot passes the turn, as below. The delivery doesn't say which kind of game it is, so the same margin applies when a game that waits for every player has less than 4 s of the turn left. A lobby's turn timeout is 30 seconds by default, and its host can set it from 5 to 600 seconds or to none.
- **A bot that returns an error, panics, returns an invalid result or runs out of time passes its turn**: the server submits a completing batch with no commands, the memory it was given and one `error` line from the logger `island_battles.bots` that says what happened, such as `The bot failed on generation 12, so it passes this turn: no plan`. The terminal shows a panic's stack. A bot stuck in a loop keeps its goroutine until you stop the server. When the game rejects the answer (an invalid command, too much memory or too many log lines), the bot passes the turn the same way, with the game's reason. Without an answer from the game, or after a server error, the same request is sent up to four times.
- **Where it submits:** only to `https://island-battles.elidan.org`, the official game, and to `http://localhost` and `http://127.0.0.1` on any port, for a local backend. `--allow-submit-host game.example.org` (repeatable; a name, or `name:port`) adds an `https` host, such as your own deployment. A delivery for any other `submit.url` answers `400` with the reason, and nothing is played or sent.
- `GET /health` answers `{"name": "mybot", "ok": true}`, with your program's name, so the game can show that your bot is connected.
- **Browsers:** the server answers CORS preflights, with `Access-Control-Allow-Private-Network: true` for Chrome's Private Network Access, only for the game's origins: `http://localhost:8888` (a local backend), `http://localhost:8890` (a local lobby's page), `http://localhost:3000` (the web client's dev server), `https://island-battles.elidan.org` (the deployed game's 3D client), `https://lobby.elidan.org` (the deployed lobby's page), and `tauri://localhost` and `http://tauri.localhost` (the desktop app). Add another with `--allow-origin` (repeatable). A request from another origin, or with a `Host` other than `127.0.0.1:<port>` or `localhost:<port>` (against DNS rebinding), gets `403`, and the bot doesn't run. The server closes the connection after every answer.

Deliver a turn by hand with curl, with your guest credential and a started lobby against a local backend. The game's routes take a player token from the lobby, not the guest credential:

```sh
PLAYER_TOKEN=$(curl -s -X POST localhost:8890/lobby/v1/player-tokens -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"game_key": "island-battles"}' | jq -r .token)
curl -s -H "Authorization: Bearer $PLAYER_TOKEN" localhost:8888/game/from-lobby/$LOBBY_ID/turn-delivery \
  | curl -X POST localhost:41414/turn -H 'Content-Type: application/json' -d @-
curl localhost:41414/health
```

**Hosting the same server publicly.** A localhost bot speaks the same protocol as a hosted bot: whoever delivers the turn POSTs a `TurnDelivery`, and the bot answers `202` and submits with the token. The game will deliver turns to bots at a URL itself once hosted bots arrive (#112). To run `serve` on a machine of your own, keep it bound to `127.0.0.1` and put a reverse proxy in front of it that terminates TLS and forwards `POST /turn` and `GET /health` with `Host: 127.0.0.1:41414` (server-to-server requests carry no `Origin`, so the origin list doesn't apply to them). The bot only submits to the allowed hosts above, so a delivery can't send your token or your bot's answer anywhere else.

## Package your bot

```sh
go run github.com/INTODAN/island-battles-go/clients/island-battles-go/cmd/island-battles-package [--output dist/island-battles-bot.zip] [package path]
```

run in your bot's module, builds **one zip that the server's runners can run** (server-run bots, #112). The package is your bot's `main` package (`.` by default), and its `main` must call `islandbattles.Main`, which the command checks. It builds with `GOOS=linux GOARCH=arm64 CGO_ENABLED=0`, so you can package from any machine, and writes:

- `island-battles-bot.json`: `{"format": 1, "language": "native", "command": ["./bot", "worker"]}`,
- `bot`: the static executable.

The zip may be at most 250 MiB and 1 GiB unpacked; over either, the command names the largest files.

### worker

`./bot worker` is the mode the runner starts. It talks JSON Lines over stdin and stdout, UTF-8:

1. It writes `{"ready": true}`.
2. For each turn the runner writes one line `{"turn": <BotTurn>}`; the worker answers one line, `{"result": <BotTurnResult>}` or `{"error": "<message>"}` (for an error, a panic with its stack, an invalid result or memory over 64 KiB). The result is checked like `check` checks it, and the lines the bot logged with `Logger(ctx)` come first in `logs`, as under `serve`.
3. The bot's own stdout goes to stderr, so prints never corrupt the protocol. Stderr is free text.
4. It exits when stdin closes.

Try it, after `go build -o mybot .`:

```sh
./mybot fixture | tr -d '\n' | sed 's/^/{"turn": /; s/$/}/' | ./mybot worker
```

### Upload it to a lobby

```sh
go run github.com/INTODAN/island-battles-go/clients/island-battles-go/cmd/island-battles-package upload --game-url <url> --token=<token> dist/island-battles-bot.zip
```

uploads the zip to a lobby (#221), where everyone in it sees it in the bot picker. Copy the game's address and the **upload token** from the bot picker's **Upload from the command line**, in the lobby page or the 3D client: the token stands for you in that lobby for 15 minutes, so the command needs no session of yours. It is the same as the Python SDK's `island-battles-bot upload`. The zip goes straight to the game's storage, then the game checks it as `package` does (the manifest, its command's file, at most 250 MiB zipped and 1 GiB unpacked, no path outside the package) and prints its reason if it refuses it. A member has one package per lobby: uploading a new one replaces the previous one. The browser pickers can also upload the zip themselves with **Upload bot (.zip)**. Choosing an uploaded package as your bot comes later.

## Investigate a game with your AI agent

[`cmd/island-battles-mcp`](cmd/island-battles-mcp/README.md) is an MCP server that you run on your own computer, so that your AI agent can investigate a game with you, from a demo file or by the lobby's code. You might ask: "On turn 40, how did my castle get captured?" Install it with `go install github.com/INTODAN/island-battles-go/clients/island-battles-go/cmd/island-battles-mcp@latest`; its README says how to register it. The 3D client's **Ask your AI agent** button copies a prompt that sets it up.

The command lives in this module, so from the release that adds it, the SDK's `go.mod` also requires the [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) (`github.com/modelcontextprotocol/go-sdk`) and a newer `golang.org/x/sys` (v0.41.0, from v0.11.0). A bot that imports the SDK gets them in its module graph, though only the command uses the MCP Go SDK.

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
| `Upgrade`, `UpgradeLevel`, `UpgradeType.speed`, `UpgradeType.clash_stop_generations`, `UpgradeType.generation_amount`, `UpgradeType.burst_potential` | `Upgrade`, `UpgradeLevel`, `SpeedUpgrade`, `ClashStopGenerationsUpgrade`, `GenerationAmountUpgrade`, `BurstPotentialUpgrade` |
| `PublicGenerationCommand` | `Command` |
| `GenerationLogLine` | `LogLine` |
| `send_soldier_group(origin, destination, amount)` | `SendSoldierGroup(origin, destination, amount)` |
| `upgrade_castle(castle_id, upgrade_type)` | `UpgradeCastle(castleID, upgradeType)` |
| `next_upgrade_cost(turn, castle, upgrade_type)` (`None` at the maximum) | `NextUpgradeCost(turn, castle, upgradeType)` (`false` at the maximum) |
| `burst_castle(castle_id, amount)`, `production(turn, castle)`, `burst_potential(turn, castle)`, `can_burst(castle)` | `BurstCastle(castleID, amount)`, `Production(turn, castle)`, `BurstPotential(turn, castle)`, `castle.CanBurst()` |
| `log_line(message, level="info")` | `Log(message)`, `Logf(format, ...)`, `LogAt(level, message)` |
| `logger.info(...)` | `islandbattles.Logger(ctx).Info(...)` |
| `turn_result(commands, logs, memory)` | `Result{Commands: ..., Logs: ..., Memory: ...}` |
| `turn.turn_number`, `turn.max_generations` | `turn.TurnNumber`, `turn.MaxGenerations` |
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
| `island-battles-bot run mybot:play --game-url … --lobby-url … --token=… --lobby-id …` | `go run . run --game-url … --lobby-url … --token=… --lobby-id …` |
| `IslandBattlesClient(credential, lobby_id, play=play, game_url=…, lobby_url=…)`: `start`, `refresh`, `wait`, `run`, `close` | `NewClient(URLs{Game: …, Lobby: …}, credential, lobbyID, play)`: `Start`, `Refresh`, `Wait`, `Run`, `Close`, each with a `ctx` |
| `client.generation`, `client.team_color`, `client.outcome` | `client.Generation()`, `client.TeamColor()`, `client.Outcome()` |
| `on_game_ended=`, `log_level=`, `log_file=`, `log_to_stdout=False` | `WithGameEnded(...)`, `WithLogLevel(...)`, `WithLogFile(...)`, `WithLogOutput(nil)` |
| `CastleNotFoundError`, `CastleNotOwnedError`, `UpgradeMaxLevelError`, `InsufficientSoldiersError`, `BurstUnavailableError`, `BurstAbovePotentialError`, `GameEndedError`, `TeamOutError` | `ErrCastleNotFound`, `ErrCastleNotOwned`, `ErrUpgradeMaxLevel`, `ErrInsufficientSoldiers`, `ErrBurstUnavailable`, `ErrBurstAbovePotential`, `ErrGameEnded`, `ErrTeamOut`, with `errors.Is` |
| `GameAuthenticationError`, `GameTransportError`, `GameProtocolError`, `GameClientClosedError`, `GameError` | `ErrAuthentication`, `ErrTransport`, `ErrProtocol`, `ErrClientClosed`, `ErrRejected`; `*GameError` has `Status` and `Detail` |
| `StaleGenerationError` | `ErrStaleGeneration`, which is also an `ErrRejected` |

## Differences from the Python SDK

- Go has no hot reload: restart `serve` after a change.
- The bot's logger comes from its `ctx`, `islandbattles.Logger(ctx)`, and its lines are always named `island_battles.player`; a Python bot may also log with child loggers of its own names. Under `check`, logged lines aren't counted with the result's lines.
- Errors are values to test with `errors.Is` and `errors.As`, not exception classes. Go's helpers take typed values and can't fail, so a command the contract doesn't allow, such as a `soldier_amount` of 0, is reported by `ValidateResult` (`ErrInvalidResult`) when the bot returns, still before any request.
- A failed turn's game log line has a traceback only for a panic. An error the bot returns has no stack, so its text is stored alone.
- The bot's `ctx` is cancelled at the turn's deadline, and when the client closes. A Python bot gets no such signal; both clients send what a late bot returns.

## Developing the SDK

The SDK is its own Go module in this repository, connected to the others by the root `go.work`. From the repository root:

```sh
go vet ./clients/island-battles-go/...
golangci-lint run --timeout=5m ./clients/island-battles-go/...
go test ./clients/island-battles-go/...
```

The client passes the [client conformance suite](../conformance/README.md) through its adapter, [conformance/main.go](conformance/main.go): offline with `ISLAND_BATTLES_CLIENT_COMMAND="go run ./clients/island-battles-go/conformance" uv run --project clients/conformance --locked pytest clients/conformance/tests/test_models.py`, and live, against the real backend, with `./scripts/test-client-conformance.sh "go -C $PWD run ./clients/island-battles-go/conformance"`. Its unit tests use a fake server and a fake realtime connection. After changing the SDK's dependencies, run `go mod tidy` both here and in the repository root, whose module requires the SDK.

The types are written by hand. Tests keep them equal to the contract the server publishes: [contract_test.go](contract_test.go) checks every property of `BotTurn`, `BotTurnResult` and the schemas they reference, and of the messages the client sends and reads (`GenerationState`, `GenerationEvent`, `GameEndedEvent`, `RegisterGenerationCommands`, `ConnectionToken` and the upgrade table), in the [player API document](../../backend/api/openapi.json) against the Go types' JSON names, both ways, with the enums and limits; the backend's [bots tests](../../backend/bots/bots_test.go) send a full turn and result of the server through these types and back, unchanged. The fixture turn, [fixture_turn.json](fixture_turn.json), is the Python SDK's, which a [Python test](../island-battles-python/tests/test_fixture.py) checks. The SDK never imports the backend or the lobby; the backend imports the SDK, to play the default bots.
