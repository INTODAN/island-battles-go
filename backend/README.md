# Island Battles backend

The Go/Huma server hosts the reusable [`lobby/`](../lobby/README.md) package and Island Battles game APIs. PostgreSQL holds identities, memberships, leases and game state, and no messages waiting to be sent. Centrifugo handles WebSocket transport and history recovery. A request that commits a lobby change or a new generation pushes it to Centrifugo before it responds, and the backend runs no work between requests. Commands use HTTP, and sockets never implicitly create games or rooms.

## Run locally

Follow the [root setup](../README.md#development-setup). Ten of the backend settings in [`.env.example`](../.env.example) are required: database URL, HTTP address, public WebSocket URL, server-only Centrifugo API URL/key, connection-token signing secret, internal proxy secret, the secret of the bots' [submit tokens](#turn-deliveries), `ISLAND_BATTLES_SUBMIT_TOKEN_SECRET`, the lobby's [player token](#player-tokens) keys, `LOBBY_PLAYER_TOKEN_KEYS`, and the shared secret of the lobby's hooks and game API, `ISLAND_BATTLES_LOBBY_HOOK_SECRET`, which the lobby in the same process reads as `LOBBY_GAME_SECRET_ISLAND_BATTLES` ([binding to the lobby](#binding-to-the-lobby)). Startup rejects missing values. `ISLAND_BATTLES_PUBLIC_URL`, where bots submit their turns, is optional. Only the HTTP server host reads the HTTP address; the [Lambda host](#hosts-and-deploys) has none. The database pool settings are optional; see [the connection pool](#the-connection-pool). So is `ISLAND_BATTLES_ADMIN_API_KEY`, the [admin API key](#map-files) that `POST /map/create` requires; without it, that endpoint refuses every request. So is `ISLAND_BATTLES_BOT_PACKAGES_BUCKET`, the S3 bucket of the lobbies' [bot packages](#bot-packages): without it, the local server keeps them on disk. So is `ISLAND_BATTLES_SERVER_BOTS`, `on` (the default) or `off`, which offers [hosted and uploaded bots](#hosted-and-uploaded-bots) and sends them their turns, and so are `ISLAND_BATTLES_BOT_RUNNER_PYTHON` and `ISLAND_BATTLES_BOT_RUNNER_NATIVE`, both or neither, the [bot runner](#bot-runners) functions that play them. Without the runners' names the local server runs the runners in its own process, with `ISLAND_BATTLES_LOCAL_BOT_PYTHON` (`python3.14` by default) for uploaded Python bots; see [how server-run bots play](#how-server-run-bots-play). So are `ISLAND_BATTLES_BOT_COMPUTE_BUDGET_GB_SECONDS` (`300000` by default) and, for local development and tests only, `ISLAND_BATTLES_BOT_COMPUTE_FAKE_GB_SECONDS`, of the [monthly compute guard](#monthly-compute-guard). Public clients receive only their guest credential, short-lived player tokens and short-lived connection JWT.

Run `./scripts/dev.sh` from the repository root to load `.env`, start local dependencies with Docker Compose or Podman, migrate the schema and run the backend. If dependencies are already running and the environment is exported, run `go run ./backend/cmd/migrate` and then `go run ./backend`. The backend only connects to the database, so it fails on a database the migrate command hasn't brought up to date. `/docs` exposes Huma documentation, `/openapi.json` the combined schema, and `/healthz` HTTP readiness. The lobby-only generated contract is in [`lobby/api/`](../lobby/api/README.md).

Open `/lobby/ui/` for the [temporary two-player lobby frontend](../lobby/frontend/README.md); `/` redirects there. It is embedded in the backend and needs no separate frontend build.

Compose mounts PostgreSQL 18's parent data directory at `/var/lib/postgresql`.

## Browser demo

The lobby lists Island Battles in the lobby frontend's game catalog with the `frontend` of its `LOBBY_GAMES` entry, [`lobbyadapter.LobbyGame`](lobbyadapter/lobby_game.json) ([binding to the lobby](#binding-to-the-lobby)), and the backend serves the browser [example player](demo/example-player.mjs), the create form's [map choice](demo/lobby-settings.mjs) and the waiting room's [bot choice](demo/bot-choice.mjs) under `/game/demo/`. The bot choice is the catalog's room module: a select of **My localhost bot** and the default levels, and, when the server offers [hosted and uploaded bots](#hosted-and-uploaded-bots), **My hosted bot (URL)**, with a field for the URL, and **Uploaded: …** for each of the player's own packages, which saves the player's member settings `{"bot": ...}` through the page with the lobby's `PUT …/member-settings`, and a line naming every member's bot, from the snapshot's `member_settings` ([choosing a bot](#choosing-a-bot)). The module's `seatOptions` also gives the host an **Add bot** control per team, with a select of the levels and, when the server offers them, the lobby's packages and **Hosted bot (URL)** with a field for the URL, which [seats that bot](#bot-seats) on that team. For a player whose hosted or uploaded bot the server plays, the example player's panel says **Played by the server** and relays nothing. With the localhost bot chosen, the module also polls `GET http://127.0.0.1:41414/health` and shows **Connected** or **Not running**, with the `island-battles-bot serve` command to copy and a note that Safari can't reach localhost from an https page ([localhost-bot.mjs](demo/localhost-bot.mjs) holds the bot's protocol: health, the turn delivery, errors). In every lobby, the example player relays the turns of a player whose bot is the localhost bot: for each generation it reads the player's [turn delivery](#turn-deliveries) and posts it to `127.0.0.1:41414/turn`, which answers `202` and submits the turn itself; the page submits no commands. If the bot is not running it asks again every 2 seconds; if the bot refuses the delivery the page shows why, and the turn's deadline decides. A bot that fails passes its turn, with an `error` line in its game log. Once a lobby starts, the frontend shows the lobby replay from `/game/from-lobby/{lobby_id}/replay`, which takes new generations from the page's `lobby.game-state` messages and reads each generation's [game logs](#game-logs) and the [demo](#demos) it saves through the page's `game.request` messages. The catalog entry also lets people outside the roster watch: its `WatchResultURL` is `GET /game/from-lobby/{lobby_id}/watch/replay`, the replay page rendered for watchers (the same template, which shows no **Logs** panel and saves the demo through `/watch/demo`; a watcher the host kicked gets 403, anyone else who may not watch 404), and its `WatchDataURLPrefix` is `/game/from-lobby/{lobby_id}/watch/`, the only paths a watcher's frame may read. The lobby page passes the lobby's watch channel to that replay as `lobby.watch-event` messages, and the replay asks the page to load it anew (`game.reload`) when a game of a later lobby round is published. The lobby page's **Copy invite link** and **Watch a lobby**, and its watcher list for members with a host **Remove**, are the lobby frontend's ([README](../lobby/frontend/README.md#watching-a-lobby)). The catalog entry also links **Watch a saved demo**, the [demo viewer](#demos), and **Play in 3D**, the web client that the deployment serves at `/play/` ([deploy README](../deploy/README.md#the-web-client)), on the lobby page.

For the quickest test, open `/lobby/ui/` and click **New demo lobby**. It creates Alex on Blue in the current tab, opens Sam on Red in a second tab, chooses each player's localhost bot, marks both ready, and starts automatically. The demo players choose their localhost bot, not a default level, because the example player in each tab plays their turns: a default bot's turns are played by the backend, and the tab would have nothing to submit. Both tabs run the example player: send one soldier from the first available owned castle to the first enemy castle, then finish the turn. Players submit immediately when a generation arrives, without an artificial client delay. Replay playback has its own viewing pace. Failed requests still use retry backoff.

The board follows new generations live. **Pause this player** stops that tab from submitting its next turn; the other player then waits at the turn barrier, until the [turn deadline](#turn-deadlines) makes the paused player's team lose. **Resume this player** continues if it's in time. The timeline lets you review previous generations; **Follow live** returns to the newest state. Keep both tabs open. A matching, fully typed Python implementation is in [`examples/player.py`](../clients/island-battles-python/examples/player.py).

In manually created or joined lobbies, each player chooses a bot in the room before getting ready. Players with a default level need no client; players with their localhost bot are served by their open lobby tab, which delivers each turn to `island-battles-bot serve` (or, with the [Island Battles Python client](../clients/island-battles-python/README.md), by `island-battles-bot run`). Only demo invitations let the browser example player play its own strategy. Disconnected players remain part of the turn barrier. In every lobby, the example player [applies the turn's deadline](#turn-deadlines) while it waits: after completing its turn, while paused, and when it only follows the game.

Run the browser player's strategy, deduplication, retry, deadline and no-polling checks from the repository root with `node backend/demo/example-player.test.mjs`, the map choice's and turn timeout's checks with `node backend/demo/lobby-settings.test.mjs`, and the bot choice's with `node backend/demo/bot-choice.test.mjs`.

## Lobby lifecycle

A guest session lasts 24 hours. Create/join is explicit and scoped by game key. The lobby in the backend's process is configured with Island Battles, as an HTTP game of its `LOBBY_GAMES` ([binding to the lobby](#binding-to-the-lobby)); the lobby's own tests use its [mock game](../lobby/mockgame/) as a second game. A create request may carry a `game_settings` object, which the game checks at creation and receives, compacted with sorted keys, in the started snapshot. The host can change them, with the teams, while the lobby waits, with `PUT /lobby/v1/games/{game_key}/lobbies/{lobby_id}/settings` ([changing the map](#changing-the-map-before-the-start)). Island Battles reads them as its [map choice](#choosing-a-lobbys-map) and [turn timeout](#turn-deadlines). Each guest consumes one seat even with several simultaneous client instances. Each instance heartbeats every 15 seconds into a 45-second lease; when all leases end, membership retains its team, readiness and host position through a 60-second grace period. There is no background sweeper. Every lobby request, including the heartbeat, and every Centrifugo subscription refresh applies due deadlines first and pushes the result. Connected members therefore see a peer's expiry within one heartbeat or refresh, and a lobby nobody is connected to catches up on its next access. Deadlines are persisted, so they also apply after a restart. Closing a client releases its own lease; leaving removes the identity's membership across devices.

The host is the earliest active joiner. Team changes clear readiness. A team holds one member, a person or a bot seat, so a lobby holds as many members as it has teams. At least two members are required to start; all active members must choose a team, become ready and have a live lease. In an Island Battles lobby every member must also [choose a bot](#choosing-a-bot) before getting ready. The host can fill a team with a [bot seat](#bot-seats) instead of waiting for another person. Starting freezes the roster; only roster players can subsequently rejoin that started room. An empty waiting room closes. Expired guest credentials require an explicit new session and do not transfer ownership.

### Choosing a bot

Every member of an Island Battles lobby chooses a bot before getting ready. The bot is Island Battles' [member settings](../lobby/README.md#member-settings) in the lobby, `{"bot": ...}` with one of:

- **Their localhost bot**, `{"kind": "localhost"}`: their own bot, which `island-battles-bot serve` ([Python client](../clients/island-battles-python/README.md)) or the `serve` subcommand of a [Go SDK](../clients/island-battles-go/README.md) program runs on their machine. Their game client delivers each turn to it, and the bot submits its answer itself ([turn deliveries](#turn-deliveries)), or `island-battles-bot run` plays the seat without a browser.
- **A default bot**, `{"kind": "default", "level": 2}`: one of the [default bots](#default-bots), with a level that `GET /bots/defaults` lists, which the backend plays itself.
- **Their hosted bot**, `{"kind": "hosted", "url": "https://bot.example.com/turn"}`, and **their uploaded bot**, `{"kind": "uploaded", "package_id": "…"}`: only while the server offers [hosted and uploaded bots](#hosted-and-uploaded-bots).

The lobby's `PUT /lobby/v1/games/island-battles/lobbies/{lobby_id}/member-settings` (`setLobbyMemberSettings`) with `{"member_settings": {"bot": {"kind": "default", "level": 2}}}` saves the caller's choice, with their guest bearer credential, while the lobby is waiting; a member can change it as often as they like. A new choice clears the member's ready, so the start sees the bot they confirmed, and publishes `lobby.changed`; the same choice again changes nothing. It returns the lobby snapshot, whose members each carry their `member_settings`, null until they choose, so every member sees every member's bot. The lobby stores the settings and Island Battles only checks them ([`services.BotRules`](services/lobby_bots.go)): settings other than exactly `{"bot": ...}` with a valid bot are 422 `invalid_member_settings`, whose `detail` says what is wrong; a caller who isn't an active member gets 404 `not_found`, and once the lobby has started or closed choosing conflicts (409 `phase_conflict`). The lobby stores the choice inside its own transaction, so a choice that races the host's start either lands before the start (and is copied) or is refused and not stored.

**The ready gate.** Island Battles answers the lobby's `ready-check` hook ([`lobbyadapter.Registration`](lobbyadapter/registration.go), [binding to the lobby](#binding-to-the-lobby)), the generic lobby's hook for a game's own readiness condition ([lobby README](../lobby/README.md#binding-a-game-over-http)). Until the member has a choice, `PUT …/ready` with `ready: true` fails with the lobby's 409 problem `game_requirement`, whose `type` is `urn:island-battles:problem:bot-not-chosen` and whose `detail` tells the player to choose their bot. The start checks the whole roster again, so a member who got ready before choices were required can't start a game without one; that refusal's `detail` names the members without a bot by display name, since the host who sees it can't choose for them. Clearing readiness never asks the gate. The lobby calls the hook inside the transaction of the ready or start request, holding the lobby's lock, so the check reads as little as it can. A member getting ready is checked from the request alone, the members' settings, unless their bot is an uploaded one, whose package it reads. At the start, with server bots on, it also reads the lobby's bot seats through the lobby's game API, which reads without waiting for that lock, and the lobby's packages when a member or a seat has an uploaded bot ([hosted and uploaded bots](#hosted-and-uploaded-bots)).

**Storage.** The lobby keeps the choices, as each membership's `member_settings`; Island Battles has no table for them and never reads the lobby's tables. A member who leaves keeps their choice for when they rejoin, and the choices survive the lobby being [reopened for its next game](#several-games-in-one-lobby). [Lobby retention](../lobby/README.md#deleting-old-data) deletes them with the lobby. Builds before member settings kept the choices in their own table, `island_lobby_bot_choices`, with a foreign key to `lobby_memberships`; the migrate command drops it, and the members of waiting lobbies choose again.

**Freeze.** When the lobby starts, the game binding copies each roster player's choice, from the member settings in the start's snapshot, to the game: `frozen_game_players.bot_kind` is `localhost`, `default`, `hosted` or `uploaded`; a default bot's level goes to `bot_level`, a hosted bot's URL to `bot_url`, and an uploaded bot's package to `bot_package_id`, with the zip's S3 key in `bot_package_key` and its language in `bot_package_language`, so the server knows which players to play and with what. The game never reads the lobby again, so the choice survives the lobby's deletion. `GenerationState.bot` tells each player their bot's kind, with the level of a default bot and only the host of a hosted bot's URL (`{"kind": "hosted", "host": "bot.example.com"}`), so their client knows whether to relay turns to their localhost bot; both SDKs' `run` refuse to play a seat that the server plays. Builds before bot kinds read a null `bot_level` as the localhost bot; the migrate command adds `bot_kind` and sets it, once, to `default` for the rows with a level and `localhost` for the others.

### Bot seats

The host can put a default bot on a team instead of waiting for a second person, and play alone. A bot seat is the generic lobby's [seat that no person plays](../lobby/README.md#seats-that-no-person-plays):

- `POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/seats` with `{"team_id": ..., "seat_settings": {"bot_level": 2}}` adds one, and `DELETE …/seats/{seat_id}` removes it, only by the host and only while the lobby waits. Both publish `lobby.changed` and return the snapshot, where the seat is a member of `kind` `seat` named `Bot (level 2)`.
- Island Battles declares the seat validator in its lobby registration ([`lobbyadapter.Registration`](lobbyadapter/registration.go)): `seat_settings` must be exactly `{"bot_level": N}` with a level that `GET /bots/defaults` lists, or, while the server offers [hosted and uploaded bots](#hosted-and-uploaded-bots), `{"bot_url": "https://…"}`, a hosted bot named `Hosted bot`, or `{"bot_package_id": "…"}`, any ready package of the lobby, named after its owner, such as `Alex's bot`; anything else is 422 `invalid_seat_settings`. The lobby asks through the `validate-seat` hook ([binding to the lobby](#binding-to-the-lobby)).
- A seat takes its team, which then holds no other member. It counts toward the two members a start needs, is always ready and holds no lease. It makes no [bot choice](#choosing-a-bot): its `member_settings` stay null, its `seat_settings` name its default bot, and the ready gate doesn't ask it for one. A start still needs the host, a person, on a team and ready.
- At the start, the seat joins the frozen roster with its own player ID, and the game binding copies its bot as it does a member's ([freeze](#choosing-a-bot)): a level seat's `bot_level`, so the [default bots](#default-bots) play it from the first generation, or a hosted bot's URL, or an uploaded bot's package. Nobody can read the game as the seat's player, since it has no session.

### Hosted and uploaded bots

Two kinds of bot play without a player's tab, for a player's own bot and for a bot seat:

- **Hosted:** the player's own server at a public URL, which speaks the [turn delivery](#turn-deliveries) protocol, like `island-battles-bot serve` but reachable from the internet.
- **Uploaded:** a [bot package](#bot-packages) uploaded to the lobby, which the server's runners play.

The server sends them each turn: see [how server-run bots play](#how-server-run-bots-play). The server offers them while `ISLAND_BATTLES_SERVER_BOTS` is `on`, the default; `GET /bots/defaults` says which in `server_bots`, and both UIs show their choices only then. With the setting `off`, choosing them is refused, the ready check treats a member who chose one while it was on as having no bot, and no turn is sent.

**A hosted bot's URL** ([`hostedbot/`](hostedbot/hostedbot.go)) is `https://`, at most 2048 characters, without credentials, on port 443 or none. Its host must be a name, not an IP literal, and every address the name resolves to must be public: not loopback, private, link-local, unique-local, unspecified or multicast. The name is resolved once when the bot is chosen; nothing is sent to the bot then. Its addresses can change after that (DNS rebinding), so the runner that delivers each turn checks the URL again and connects only to public addresses on port 443, checking the very address it connects to (`hostedbot.Client`), and follows no redirects. `GenerationState.bot` shows only the URL's host. **For local development only**, the local server host also accepts a hosted bot on this machine, such as `http://127.0.0.1:41414/turn` from `island-battles-bot serve`, since its own runners deliver there; the Lambda host never does.

**An uploaded bot** is a package of the lobby whose status is `ready`, when it is chosen and again at the start. A member's own bot must be one of their packages: the member-settings check doesn't learn who sets the settings, so it accepts any ready package of the lobby, and the ready check refuses a member whose package someone else uploaded. A seat may use any member's package. Completing a new upload replaces the owner's previous package, and the owner can delete one, so a chosen package can disappear: the ready check then refuses the member with `game_requirement` of type `urn:island-battles:problem:bot-package-gone` ("Your uploaded bot was replaced; choose it again"), and the start too, naming them. The lobby doesn't pass its seats to the ready check, so the gate reads the lobby's members through the game API before it, and refuses the start while a seat's package is gone; the host removes the seat and adds the bot again. A package deleted in the moment between the start's check and its copy is frozen without its key, language and size, and that player passes every turn at once, with an `error` line from `island_battles.bots` that says why.

So one person plays a whole game against a bot: create a lobby with two teams, choose a team, add a level 2 bot seat to the other team, choose your own bot, get ready and start. With `island-battles-bot run` (or the localhost relay) playing your seat, the game plays to its end.

### How server-run bots play

When a turn opens, the server sends it to every hosted and uploaded bot of a team still in the game that hasn't completed it (#225), and never waits for the bot:

1. A generation is created in a transaction under the game lock: at the start, and whenever a completing batch or the [default bots](#default-bots) complete a turn. The code that creates it collects the turns to send, each the [turn delivery](#turn-deliveries) that `GET …/turn-delivery` would answer that player, with its submit token. They are sent only once the transaction has committed; a generation that is rolled back sends nothing. A finished game, an eliminated team or a completed turn sends nothing.
2. Each turn is one asynchronous invocation (`InvocationType: Event`) of a [bot runner](#bot-runners): the native runner delivers a hosted bot's turn to its URL, and the runner of an uploaded package's language plays it from a presigned GET of its zip, valid for an hour, within 10 seconds to load and 2 to play. Lambda answers once the event is queued.
3. The bot submits its answer itself with the submit token, as a localhost bot does, and that request plays the turn once every team still in the game has completed it, and sends the next one. A bot that never answers loses at the turn's deadline, as any other does.

**When a runner can't be started**, because Lambda throttled the invocation or answered with an error, the server logs it, and the player gets one `error` line from `island_battles.bots` in that turn's game log: "The server couldn't start your bot this turn." The turn then waits for its deadline. The request that created the generation never fails for it.

**When every player is a server-run bot**, the game still moves on with every tab closed: each submission is a request that creates the next generation and sends it on. If a runner fails without submitting, the game waits for the deadline, which a client that watches the game applies (`POST …/deadline`); with no time limit, it waits until someone does. This is accepted.

**Where the runners run.** With `ISLAND_BATTLES_BOT_RUNNER_PYTHON` and `ISLAND_BATTLES_BOT_RUNNER_NATIVE`, which Terraform sets on Lambda, the backend invokes those functions with its own role ([`botrunner/`](botrunner/botrunner.go)). Without them, the local server host runs the same runners in its own process ([`botrunner/local.go`](botrunner/local.go)), so local games and the live acceptance test need no AWS: the native runner's Go code in a goroutine, and the Python runner's `runner.py --events` in a child process with `ISLAND_BATTLES_LOCAL_BOT_PYTHON` (`python3.14` by default; for example `uv run --no-project --python 3.14 python`). Like a Lambda instance, each keeps a package loaded between turns and plays one turn of it at a time. They submit to the server's own origin, and play packages built for this machine (see [Bot runners](#bot-runners)). The Lambda host without the runners' names sends nothing, and logs why when it starts.

**What a player sees:** the Bot logs panel shows the bot's own lines each turn, a runner's pass with its reason (a load or play timeout, a crash, an error, a rejected answer), and the line above when the server couldn't start the bot. A bot that never submits loses at the deadline: "Red lost: Sam's bot didn't answer within 30 s".

### Monthly compute guard

The runners share Lambda's always-free 400,000 GB-s a month with the backend function, and the owner's target is 100 free games a month with uploaded bots. So the runners get a **budget of 300,000 GB-s a month** (`ISLAND_BATTLES_BOT_COMPUTE_BUDGET_GB_SECONDS`, `300000` by default), and once they have used it, **no new game with an uploaded bot starts until the first of the next month (UTC)** (#226). At 1 GB × 2 s a turn, a typical game (4 uploaded bots, about 150 turns) uses about 1,200 GB-s, so about 250 games fit, or about 31 worst-case games (16 uploaded bots over 300 turns).

- **Measured, not estimated** ([`botcompute/`](botcompute/botcompute.go)): the runners' month-to-date `AWS/Lambda` `Duration` (Sum), plus `InitDuration` where the account publishes it, for both runner functions since the first of the month at midnight UTC, in hourly datapoints, times their 1 GB. Hosted bots' turns, about 0.1 GB-s each, are in the same metric and count too. The backend's role reads them with `cloudwatch:GetMetricData`.
- **Cached for 10 minutes** in the one-row table `island_bot_compute_usage` (`month`, `gb_seconds`, `read_at`; GORM AutoMigrate, no game's data), so CloudWatch is read at most once per 10 minutes for all instances: a refresh takes a transaction-level advisory lock and reads the row again before it reads CloudWatch. A new month's first read replaces the row. A failed read keeps nothing, and the check that needed it answers as unavailable.
- **Where it applies:** the [ready check](#hosted-and-uploaded-bots) at the start. A start roster with any uploaded bot, a member's or a bot seat's, is refused with `game_requirement` of type `urn:island-battles:problem:bot-compute-spent`: "Uploaded bots have used this month's free compute. They're back on 1 December." Getting ready isn't refused, games already running go on, and games with only localhost, hosted and default bots are unaffected.
- **`GET /game/bot-compute`** returns `{month, used_gb_seconds, budget_gb_seconds, available}`, public and from the same cache. Both bot pickers read it when they show the room, and grey out the uploaded choices (your own bot's and the host's Add bot) with the same sentence while `available` is false.
- **Local development:** without the runner functions' names there is no CloudWatch, and the guard is off: `available` is always true and `used_gb_seconds` 0. To test it, set `ISLAND_BATTLES_BOT_COMPUTE_FAKE_GB_SECONDS`, the month's compute that the guard then reads instead, uncached; for example `300000` refuses every start with an uploaded bot. It can't be set together with the runner functions.

### Bot packages

Any active member of a lobby, in any phase, can upload a **bot package** to it (#221): the zip that the SDKs' `package` command builds, with `island-battles-bot.json` at its root. A package belongs to the lobby; a member has at most one, and completing a new upload deletes their previous one. There are no quotas, only the size limits, which are the SDKs' own in binary units: 250 MiB zipped (262,144,000 bytes) and 1 GiB unpacked (1,073,741,824 bytes). The [bot runners](#bot-runners) play them each turn of a game where a player or a seat chose one ([how server-run bots play](#how-server-run-bots-play)). [`services/bot_packages.go`](services/bot_packages.go) owns the rules.

1. `POST /game/lobbies/{lobby_id}/bot-packages` with `{"file_name": "island-battles-bot.zip"}` stores a `pending` row and answers `201` with `package_id` and a **presigned POST**, `upload: {url, fields}`, valid 15 minutes, for 1 byte to 250 MiB, to the key `lobbies/<lobby_id>/<package_id>.zip`. The zip never passes through the backend, since a Lambda Function URL takes at most 6 MB: the client posts `fields` and then the file, as `file`, to `url`.
2. `POST …/bot-packages/{package_id}/complete` checks the zip with range requests, without downloading it ([`botpackage/`](botpackage/botpackage.go), [`packagestore/`](packagestore/packagestore.go)): the tail and the central directory, then the manifest, usually three requests. The manifest must be format 1, either `python` with `"python": "3.14"` and a `module:function` entry, or `native` with a `command` whose program is a file in the zip; the files must come to at most 1 GiB unpacked, judged from the directory, so a zip bomb is never unpacked; and no path may leave the package (`..`, absolute paths, backslashes, symbolic links or other non-regular files). A package that fails is 422 with a plain `detail` that says what is wrong, and its row and zip are deleted. Before the zip arrives, it is 409 and nothing changes. Only the member who started the upload can complete it.
3. `GET …/bot-packages` lists the lobby's ready packages, newest first: `package_id`, `owner_player_id`, `owner_name`, `language`, `entry_or_command` (the entry, or the command joined with spaces) and `size_bytes`, `uploaded_at`. `DELETE …/bot-packages/{package_id}` deletes a package and its zip, only by its owner (others get 403).

Every endpoint takes a [player token](#player-tokens) (or, for the upload, an upload token) and reads the lobby as its game does (`domain.GameAPI`), so anyone who isn't an active member of the lobby gets 404. The guest credential is refused with 401.

**Upload tokens.** The command line doesn't hold the player's credentials, so `POST …/bot-packages/upload-token` gives a member an **upload token** for themselves and the lobby, valid 15 minutes, which the bot pickers show in a ready-to-copy `island-battles-bot upload --game-url <url> --token=<token> dist/island-battles-bot.zip`. The token is `ibut1.<payload>.<mac>`: the compact JSON `{"lobby_id", "player_id", "exp"}` and its HMAC-SHA256 under `ISLAND_BATTLES_SUBMIT_TOKEN_SECRET`, both base64url without padding ([`uploadtoken/`](uploadtoken/uploadtoken.go)). The MAC covers the `ibut1.` prefix as well, so a [submit token](#turn-deliveries) signed with the same secret is never an upload token, nor the reverse. The start and complete endpoints take it in place of a player token (`Authorization: Bearer ibut1.…`), for its own lobby only; the game still checks that its player is an active member. Nothing else takes it. The CLIs read `lobby_id` from the payload, which isn't secret.

**Table.** `island_bot_packages` (GORM AutoMigrate, game data): `package_id`, `lobby_id`, `owner_player_id`, `owner_name`, `status` (`pending` or `ready`), `s3_key`, `language`, `entry_or_command`, `size_bytes`, `uploaded_at` and `created_at`. It has no foreign key to the lobby's tables (#118). [Game retention](#game-retention) deletes rows 8 days after they were created, as the bucket's lifecycle rule deletes the zips.

**Storage.** On Lambda the zips are in the S3 bucket `<name>-bot-packages`, which Terraform creates and names in `ISLAND_BATTLES_BOT_PACKAGES_BUCKET`, in the function's region and with its role's credentials ([deploy README](../deploy/README.md#bot-packages)). A host without the setting answers the endpoints with 503. **Locally, the backend keeps the packages on disk:** when `ISLAND_BATTLES_BOT_PACKAGES_BUCKET` isn't set, the local server host (`go run ./backend`, which `scripts/dev.sh` runs) serves a fake S3, [gofakes3](https://github.com/johannesboyne/gofakes3), under `/local-s3/` on its own address, keeping each zip as a file in `ISLAND_BATTLES_BOT_PACKAGES_DIR` (`island-battles-bot-packages` in the system's temporary folder by default). Browsers and the CLIs upload to it with the same presigned POST and the backend reads it with the same range requests, so the local path is the deployed one; nothing more to start. It checks no signatures or upload policies, so the 250 MiB limit is checked only on completing, and it has no lifecycle rule, so local zips stay until you delete the folder. Presigned URLs point at `ISLAND_BATTLES_PUBLIC_URL`, or at `http://localhost:<port>` without it.

## Player tokens

Every game route, `/game/from-lobby/{lobby_id}/…`, `/game/lobbies/{lobby_id}/bot-packages…` and `POST /generation/commands`, takes a **player token** where it took the guest credential before (breaking, #218): `Authorization: Bearer <token>`, with a token from the lobby's `POST /lobby/v1/player-tokens` and `{"game_key": "island-battles"}` (the lobby's [player tokens](../lobby/README.md#player-tokens)). The guest credential is refused there with 401. A token lasts 15 minutes, and never past the session; clients get a new one at about 80 % of its life, and once more when a game request answers 401. The 3D client, the lobby page and its game modules, and both SDKs do this themselves. The lobby's own routes still take the guest credential.

The game verifies a token with the lobby's keys in this process ([`handlers.PlayerTokens`](handlers/player_token.go)), without reading `lobby_sessions`, and takes the player and their display name from it. A missing, expired or invalid token, or one for another game, is 401 with the same problem as before. Its `iss` is `ISLAND_BATTLES_PUBLIC_URL`, or `urn:island-battles:local-lobby` without one. `LOBBY_PLAYER_TOKEN_KEYS` holds the keys ([deploy README](../deploy/README.md#rotating-the-player-token-key)); `LOBBY_PLAYER_TOKEN_LIFETIME`, from `1m` to the default `15m`, shortens tokens to try the clients' renewal in development.

## Interactive Island Battles generations

Starting an `island-battles` lobby initializes generation one. Each team starts with a castle and 30 soldiers, and has exactly one player: a start roster with two players on one team is refused and starts no game. The frozen roster defines who takes part in each generation: every roster player can submit commands and then complete their turn, and the game advances exactly once after all roster players complete. A player disconnecting does not remove them from this barrier; the game waits until that player reconnects and completes the turn, or until the turn's [deadline](#turn-deadlines) passes, which makes their team lose.

Players read their authorized current state with `GET /game/from-lobby/{lobby_id}/generation`, then register commands with `POST /generation/commands`. Both requests require a [player token](#player-tokens) of a player on the game's frozen roster. Identity and team color come from that token and the frozen roster, so clients cannot choose another player or team. The game reads its lobby only once, when it binds at start; afterwards it authorizes against its own roster and finds itself by the lobby ID it stored, so players keep their access after they leave the lobby or it is deleted. Example request:

```json
{
  "game_id": 1,
  "generation_id": 1,
  "request_id": "f8b7e4e2-8e32-4a13-9370-4d5fa740e21b",
  "complete": false,
  "commands": [
    {
      "command_type": "send_soldier_group",
      "parameters": {
        "origin_castle_id": 0,
        "destination_castle_id": 1,
        "soldier_amount": 5
      }
    },
    {
      "command_type": "upgrade_castle",
      "parameters": {"castle_id": 0, "upgrade_type": "speed"}
    }
  ]
}
```

Players have two commands: `send_soldier_group` sends soldiers from a castle their team owns, and `upgrade_castle` buys the next level of one upgrade type for such a castle, as [Castle upgrades](#castle-upgrades) describes. Other command types are forbidden. Commands may be sent in several batches with `complete: false`; `complete: true` seals the turn. Omitting `complete` defaults to true for a one-batch turn. An empty command list is valid, so send `{"commands":[],"complete":true}` (with the other required fields) to pass. Once complete, that player's turn is sealed for the generation. The server checks each command against the generation and every command already accepted this turn, in the player's own earlier batches: it tracks, per castle, the soldiers not yet reserved and each upgrade type's pending level. The turn deducts exactly those totals, so an accepted command cannot fail when the turn runs. The server rejects a whole invalid batch without storing any part of it or completing the turn.

Each request UUID identifies one immutable batch for one player and generation. Retrying the exact request is safe, including after a timeout or generation advance; reusing that UUID with changed content conflicts. New requests for an old generation and new commands after turn completion also conflict. A game ends after the turn that leaves one team with castles or soldier groups (neutral castles need not be captured), as a draw when the last teams are eliminated in the same turn, or after the turn that creates generation 300, where the team with the most castles wins and equal counts draw. Turns are not paced, so the cap keeps one game of fast bots from loading the server. A game also ends when a turn passes its [deadline](#turn-deadlines) before every roster player completed it. The game is then over: its final generation stays readable, `GenerationState.outcome` says how it ended, and every new command conflicts with problem type `urn:island-battles:problem:game-ended`. Errors use Huma problem JSON: 401 for missing/invalid credentials, 404 for an inaccessible game, 403 for forbidden commands or ownership, 409 for stale/sealed turns, UUID conflicts, insufficient unreserved soldiers, or an upgrade past its maximum level, and 422 for invalid parameters. Command rejections that clients tell apart carry a problem `type`, shared by both commands:

| Rejection | Status | Problem type |
|---|---|---|
| Parameters missing, of the wrong type, or with extra keys | 422 | `about:blank` |
| `upgrade_type` is not a known type | 422 | `urn:island-battles:problem:unknown-upgrade-type` |
| A castle isn't in this generation | 422 | `urn:island-battles:problem:castle-not-found` |
| The castle belongs to another team or is neutral | 403 | `urn:island-battles:problem:castle-not-owned` |
| The type's pending level is already its maximum | 409 | `urn:island-battles:problem:upgrade-max-level` |
| Soldiers or an upgrade cost beyond the castle's unreserved soldiers | 409 | `urn:island-battles:problem:insufficient-soldiers` |
| The batch's [game log](#game-logs) lines would pass 100 for the player's generation, or one has more than 500 characters | 422 | `urn:island-battles:problem:game-log-limit` |
| The batch's [bot memory](#bot-turns) is larger than 64 KiB as compact JSON | 422 | `urn:island-battles:problem:bot-memory-too-large` |

Each `detail` names the castle and the numbers, such as `castle 3 has 12 unreserved soldiers; speed level 3 costs 20`. Read it before retrying with a new request UUID.

The response is the caller's current `GenerationState`. The final player's completion returns the new generation. The request that completes the turn also pushes a `game.next_generation` event to every roster player's authenticated personal `player:<player_uuid>` channel. The turn that ends the game pushes `game.ended` instead, with the final generation and its outcome; it is the game's last event. Centrifugo keeps a short history on that channel for reconnecting clients. A client can see the same generation both in an event and over HTTP, so clients deduplicate by game and generation. The [Island Battles Python client](../clients/island-battles-python/README.md) calls the bot's `play(turn)` function once per generation with a typed `BotTurn` and sends its result as the turn's one completing batch ([bot turns](#bot-turns)). The client reconciles authorized HTTP state on subscription/reconnect. The browser demo uses the same wire events. Neither game client polls by default. The generic lobby protocol is unchanged.

`GET /game/from-lobby/{lobby_id}/replay` shows the players on the frozen roster the recorded generation history. The replay is not stored: each request loads the game's generations in ID order, in a fixed number of batched queries, and turns each into a frame. Completing a turn writes no replay data. Each frame is the same `Generation` board view that players receive in `GenerationState.generation`, with castles and soldier groups sorted by ID, so the page appends live generations unchanged. The page writes each castle's upgrade levels under its name, such as `Speed 2 · Stop 1`. Only the replay's static metadata lives on the game: its teams with their lobby names and players' display names, copied at start because the lobby is deleted later, and its outcome, which stays empty while the game runs and is saved by the turn that ends the game. The page data carries the outcome as players see it, the `GameOutcome` with its summary, or null while the game runs. The page shows the outcome, also when it arrives with a live state. Its controls play and pause, step one generation back or forward, jump to the latest generation, follow live generations and scrub with a slider, next to the generation number. A **Speed** select of 0.5×, 1×, 2× and 4× sets the pace of playback: 1× shows a generation for 500 ms, and the others scale that (1000, 250 and 125 ms); following live is not paced by it. The 3D client has the same speeds. **Save demo** saves the game as a [demo file](#demos).

Clashes are saved atomically with each generation. Generation snapshots, live notifications and replay frames always include a `clashes` array, empty for turns without combat. Each event has a generation-local `id`, a `time` fraction within the turn, and participant records with `group_id`, `team_color`, contact `x`/`y`, `clash_radius`, `soldiers_before`, and `soldiers_after`. Counts describe each individual contact, including multiple clashes by the same group in one turn. The stored and published form is the same `models.Clash`.

The lobby battlefield pulses an impact ring, shows loss numbers only above surviving troops, and leaves a dashed outline with × for defeated groups. Defeated markers stay at their recorded contact positions, including when they overlap other troops or castles. Losses from multiple clashes are combined above each surviving troop. Effects remain visible while paused and clear on the next generation; scrubbing restores static effects, and reduced-motion settings disable the pulse. Combat mechanics and soldier totals are unaffected.

Soldier groups have a persisted `ClashRadius`, defaulting to **0.25 world units**, which is copied to subsequent generations. Enemies clash when their center-to-center distance reaches either group's radius (the larger radius, rather than their sum). Contacts resolve in time order using troop counts before casualties. If one group has strictly less than half the other's soldiers, the survivor completes its movement. Otherwise the groups stop at the midpoint between their contact positions for the generation; exactly half also stops. In a simultaneous connected clash, any comparable opposing pair causes a stop at the average participant position. Paths are recalculated after a stop so survivors can still fight later enemies. Dead and empty groups cannot participate in later clashes. Simultaneous connected contacts resolve together; each team loses up to the combined opposing force, with losses assigned in persistent group ID order.

Each group's `clash_stop_generations` upgrade controls its own pause duration: **level 1 → 3 generations**, **level 2 → 2**, and **level 3 → 1**, including the clash generation. Troops copy this level when spawned, so upgrading the castle does not change existing troops. Each group persists its remaining `MovementPauseGenerations`, which is copied across generations and decremented when it stays still. Smaller attacks do not restart or extend an existing pause.

Groups leave field combat when they reach their destination, including arrivals exactly tied with contact. Already-arrived groups cannot clash with newly spawned troops. Short trips retain normal movement speed until arrival. At the end of the turn, castle arrivals resolve in persistent group ID order against the updated castle state, preserving earlier damage, captures, and reinforcements. Sending zero soldiers or sending to the same castle is rejected.

The replay draws troop circles using their stored radius and castle circles with a visual-only radius of **0.5 units**, with the same world scale on both axes.

The Island Battles adapter wraps the generic start operation. Generic lobby start freezes the roster first; game initialization uses that roster and persists generation one and its player/team bindings. Concurrent or repeated starts reuse the initialized game. Other game keys are unaffected.

### Turn deadlines

The host sets how long each turn lasts when creating the lobby, and can change it until the start ([changing the map](#changing-the-map-before-the-start)), with `game_settings.turn_timeout_seconds`: a whole number of seconds from 5 to 600, or `null` for no limit, which helps while debugging a bot at a breakpoint. Settings without it get **30 seconds**. The lobby page's create form offers 10 s, 30 s, 2 min, 10 min and No limit. The game copies the timeout at the start, to `games.turn_timeout_seconds`, which is null for no limit; AutoMigrate adds the column, so games started before it have no limit.

- **The deadline.** Each generation's turn times out at the generation's creation time plus the timeout. `GenerationState.turn_deadline_at` and `BotTurn.turn_deadline_at` carry it, also in pushed events. They are `null` without a limit and once the game has ended.
- **One missed turn loses.** When the deadline passes before every roster player completed the turn, every team still in the game with a player who hadn't has timed out and loses, and the game ends at once. A team is still in the game while it owns a castle or has soldier groups on the field, the same rule as the end rule's; an **eliminated team never times out**. Its players have nothing to play, so the turn doesn't wait for them: the turn plays as soon as every player of a team still in the game completed it, and a deadline that finds only eliminated players late changes nothing. Among the other teams that are still standing, the team with the most castles wins, as at the generation cap, and equal counts are a draw. When several teams time out together, they all lose; when every team still in the game does, the game is a draw.- **The outcome** has reason `turn_timeout` and the winner, or `null` for a draw. Its `summary` names each team that timed out and its players who didn't answer, and then the winner: `Red lost: Sam's bot didn't answer within 30 s; Blue wins` (an eliminated team is never named), `Blue lost: Alex's and Lee's bots didn't answer within 2 min; Red lost: Sam's bot didn't answer within 2 min; Green wins`, `…; the other teams draw`, or, when every team still in the game timed out, `Draw: Blue's and Red's bots didn't answer within 30 s`. The saved `games.outcome` also lists the teams that timed out and those players' start display names under `timed_out`, and the colours of the teams still in the game that didn't time out under `survivors`. The open generation stays the final one; it is pushed once more to every roster player as `game.ended`, each with their own `submitted`, and every later command conflicts with `urn:island-battles:problem:game-ended`, including a late batch for the expired generation. A retry of a batch that was accepted in time still answers as before.
- **No scheduler.** The backend runs no work between requests, so a deadline applies when a request finds it overdue, as lobby leases expire. Every game request applies it first, under the game lock: the state read, every batch, the replay, game log and demo reads, and `POST /game/from-lobby/{lobby_id}/deadline`. The reads that take no game lock check the deadline with one light query first and lock the game only once it has passed. A batch that arrives after the deadline ends the game itself and is rejected, while the end still commits. Applying a deadline again changes nothing, so the game ends once and `game.ended` is pushed once, also when several requests race.
- **`POST /game/from-lobby/{lobby_id}/deadline`** applies the deadline for a roster player and answers their `GenerationState`, as the state read does. Before the deadline, without a limit, or after the end, it changes nothing. A client that waits for other players calls it once, about a second after `turn_deadline_at`, so a game nobody answers still ends. The [Python client](../clients/island-battles-python/README.md) does this after completing its turn, and the lobby page's example player does it while it waits; the web client will too ([#101](https://github.com/INTODAN/island-battles-go/issues/101)). If the turn is still open, for example because the server's clock is behind the client's, they ask again after 2 seconds, doubling up to 30.

### Game logs

A bot can send the lines it logs while playing a generation, its **game log**, with that generation's batches. `POST /generation/commands` takes an optional `logs` array:

```json
"logs": [{"level": "info", "logger": "island_battles.player", "message": "attacking castle 3 with 12 soldiers"}]
```

- `level` is `debug`, `info`, `warning` or `error`. `logger` names the logger that wrote the line, in at most 100 characters. `message` has 1 to 500 characters, counted as Unicode code points, and no NUL.
- A batch may carry logs, commands, both, or neither. The server appends a batch's lines, in order, after the player's earlier lines for the generation, under the game lock. The payload hash includes them, so retrying a request stores nothing twice.
- A player has at most **100 lines per generation**. A batch that would take the player past 100 lines, or that has a line longer than 500 characters, is rejected whole with 422 and problem type `urn:island-battles:problem:game-log-limit`; the detail names the limit. Nothing of a rejected batch is stored: not its lines, its commands, or its completion. Other invalid lines, such as an unknown level, are a plain 422. The [Python client](../clients/island-battles-python/README.md#logging) cuts its lines and counts them from the state's `log_lines_stored`, so it doesn't send such a batch unless another client of the same player logged meanwhile; then it sends the batch again without its lines.
- Every `GenerationState`, from `GET .../generation` and from batches, has `log_lines_stored`: how many lines the caller has stored for its generation. A client that starts again during a turn counts on from it. Pushed events carry 0, because a new generation has no lines yet.
- Lines are accepted only while the turn is open, as commands are: a new batch after completion, or for a generation that is no longer current, conflicts with 409.

The lines live in the `generation_logs` table, one row per line, with the game, the generation, the player, the line's `sequence` within the player's generation (from 1), its level, logger and message. The unique index on the generation, player and sequence serves the line count under the lock and the reads. The table is game data: AutoMigrate creates it, and [retention](#game-retention) deletes the lines with their generation. Players are named in the logs by `FrozenGamePlayer.DisplayName`, the lobby display name copied at start, as the replay's team players are; a player that the start snapshot doesn't list is named `Player <n>` after their roster position.

`GET /game/from-lobby/{lobby_id}/logs?generation_id=N` reads one generation's game log, with the same authorization as `/generation`: a place on the game's frozen roster. Its response is the `GenerationLogs` schema:

```json
{
  "generation_id": 41,
  "players": [
    {"display_name": "Alex", "team_color": 1, "lines": [{"level": "info", "logger": "island_battles.player", "message": "attacking castle 3 with 12 soldiers"}]}
  ],
  "hidden_team_colors": [2]
}
```

- While the game runs, the caller sees only their own team's lines. Once the game has an outcome, every roster player sees every player's lines.
- `players` lists every player of the visible teams, also those without lines, ordered by team colour and then display name. Each player's `lines` keep the order they were stored in.
- `hidden_team_colors` lists, in ascending order, the teams whose lines the caller can't see yet, so a page can explain why. It doesn't reveal whether those teams logged anything. It is empty once the game has ended.
- The response contains no player IDs.
- `generation_id` must be a generation of this game; any other is 404, as are callers off the roster. Like the other game reads, the request ends with a game [retention](#game-retention) pass.

A [demo](#demos) holds one `GenerationLogs` per generation, with the visibility rule that applied when it was saved.

The replay shows the game log in a **Logs** panel below the board, for the generation on the board. The replay runs in the lobby page's sandboxed frame without the credential, so it reads the logs through the page: Island Battles' catalog entry sets the lobby frontend's `DataURLPrefix` to `/game/from-lobby/{lobby_id}/`, and the replay posts `game.request` messages for `/game/from-lobby/{lobby_id}/logs?generation_id=N`, which the page fetches for it (see the [lobby frontend README](../lobby/frontend/README.md#what-a-game-provides)). The replay page data carries the `lobby_id` for that path. The panel groups the lines by team and then by player, in monospace, coloured by level, and scrolls inside a fixed height, so the frame's reported size doesn't change as lines load. It caches the lines of generations whose turn is over. For the open turn, the latest generation of a running game, it says that lines may still arrive and, while that generation is shown in a visible tab, reads its lines again every 3 seconds, at most one read at a time; a generation that was open when shown is read once more when the next one arrives, for its final lines. It explains hidden teams ("Red’s logs appear when the game ends") and generations without lines, and loads every generation again when the game's outcome arrives. Opened outside the lobby page, it says the logs need the lobby.

### Bot turns

A bot is a **stateless function**. Each turn it gets a `BotTurn` and answers with a `BotTurnResult`; both are schemas in the [player API document](#player-api-document), so every client generates them:

```json
{"game_id": 7, "generation": {"id": 41, "castles": [], "soldier_groups": [], "clashes": []}, "team_color": 1,
 "memory": {"target": 3}, "turn_deadline_at": "2026-11-07T12:00:30Z"}
```

```json
{"commands": [{"command_type": "send_soldier_group", "parameters": {"origin_castle_id": 0, "destination_castle_id": 3, "soldier_amount": 12}}],
 "logs": [{"level": "info", "logger": "island_battles.player", "message": "attacking castle 3 with 12 soldiers"}],
 "memory": {"target": 3, "sent": 12}}
```

- `generation` is the generation of `GenerationState`, and `team_color` is the bot's team. There are no previous generations: a generation is 10 to 25 KB of JSON, so a whole game would be megabytes per turn. A bot that needs history keeps what it needs in its memory.
- `memory` is the memory the bot returned last turn: any JSON object, or `null` on its first turn. The bot returns its new memory, or `null` for none.
- `turn_deadline_at` is when the turn times out, `GenerationState.turn_deadline_at`, or `null` when the lobby set no limit. A bot that hasn't answered by then makes its whole team lose; see [turn deadlines](#turn-deadlines).
- `commands` and `logs` have the shapes of `POST /generation/commands`, and the same rules apply.

Whoever runs the bot, a client relaying it or the backend itself, sends the result as the turn's completing batch: `POST /generation/commands` with its `commands`, `logs` and `memory`, and `complete: true`. The **server stores the memory**, so every way of running a bot behaves the same, and a bot keeps nothing between turns itself:

- Only the batch with `complete: true` saves `memory`, under the game lock, replacing the player's previous memory. An absent or `null` memory clears it. Batches with `complete: false` may carry memory, but it isn't saved. A client that sends a turn in several batches puts the memory in the last one.
- Memory is at most **64 KiB** (65,536 bytes) as compact JSON; whitespace around values doesn't count. Larger memory is rejected whole with 422 and problem type `urn:island-battles:problem:bot-memory-too-large`, and nothing of the batch is stored: not its commands, its lines, its memory or its completion. A memory that isn't a JSON object or `null` is a plain 422.
- The payload hash includes the memory, so a retry stores it once, and a late retry of an earlier turn doesn't bring that turn's memory back. Absent and `null` memory hash alike.
- Every `GenerationState` has `memory`: the memory sent with the caller's latest completed turn of the game, or `null`. It's the next `BotTurn.memory`. Events pushed after a turn carry each player's own memory, on their personal channel only.
- Memory is kept as sent: numbers keep every digit. PostgreSQL may reorder an object's keys.

The memory lives in the `game_player_bot_memory` table, one row per player who has memory, keyed by the game and the player, with the generation whose turn saved it. The memory column is `bytea`, the exact compact JSON the service validated, because `jsonb` would reorder keys, expand numbers like `1e131071` into every digit and reject `\u0000`. A turn that clears the memory deletes the row. The table is game data: AutoMigrate creates it, and [retention](#game-retention) deletes it with the game.

### Turn deliveries

A bot that runs outside the backend gets each turn as a **turn delivery** and submits its answer itself, so nobody waits on the bot. The localhost bot works this way, with the player's browser tab delivering its turns; hosted and uploaded bots get theirs the same way from the server's [bot runners](#how-server-run-bots-play).

1. The deliverer reads `GET /game/from-lobby/{lobby_id}/turn-delivery` with the player's player token. The server builds the `BotTurn` (the board as the player's team sees it, the team, the upgrade table, the stored memory and the deadline) and returns a `TurnDelivery`:

   ```json
   {"turn": {"game_id": 7, "generation": {"id": 41, "castles": []}, "team_color": 1, "upgrades": [], "memory": null, "turn_deadline_at": "2026-11-07T12:00:30Z"},
    "submit": {"url": "https://island-battles.example.org/generation/commands", "token": "ibst1.eyJ….Zm9v", "deadline_at": "2026-11-07T12:00:30Z"}}
   ```

   A player with nothing to play gets 409: the game has ended (problem type `urn:island-battles:problem:game-ended`), their team has been eliminated, or they have completed the turn. Like every game request, the read first applies an overdue deadline.
2. The deliverer POSTs the delivery to the bot server's `/turn`, which answers `202 {"accepted": true}` at once.
3. The bot server plays the turn and submits `{game_id, generation_id, request_id, commands, logs, memory, complete: true}` to `submit.url` with `Authorization: Bearer <submit.token>`. A bot that raises or runs out of time passes the turn: no commands, its old memory, and one `error` line from the logger `island_battles.bots`, as the [default bots](#default-bots) do.

**The submit token** is `ibst1.<payload>.<mac>`: the compact JSON `{"game_id", "player_id", "generation_id", "exp"}` and its HMAC-SHA256 under `ISLAND_BATTLES_SUBMIT_TOKEN_SECRET`, both base64url without padding ([`submittoken/`](submittoken/submittoken.go)). `exp` is the turn's deadline plus 30 seconds, or 24 hours after the generation when turns have no limit. `POST /generation/commands` takes it in place of the player's player token for a batch of that game and generation only, compared in constant time; any other game or generation, an expired or altered token, or a player no longer on the roster is 401. Everything else about the batch is as with a player token: the batch rules, the memory, the logs, the retries and the deadline. The server stores nothing per token, and every read of the delivery issues a new one.

**The submit URL** is `ISLAND_BATTLES_PUBLIC_URL` plus `/generation/commands`: the game's public origin, such as `https://island-battles.example.org`, which the VM deploy writes from `game_url` ([deploy README](../deploy/README.md#the-lambdas-settings)). Without it, each request's own scheme and `Host` are used, which suits a local backend that players reach directly. The bot servers submit only to `https://island-battles.elidan.org`, to `http://localhost` and `http://127.0.0.1` on any port, and to hosts added with `serve --allow-submit-host`.

Bots that the backend plays itself implement the Go interface `bots.Bot` in [`bots/`](bots/bots.go), `Play(ctx, BotTurn) (BotTurnResult, error)`, on the Go types `models.BotTurn` and `models.BotTurnResult` that the schemas are exported from. To write a bot of your own in Go, use the [Go SDK](../clients/island-battles-go/README.md); in Python, the [Python SDK](../clients/island-battles-python/README.md).

### Bot runners

Two Lambda functions play the turns of [uploaded packages](#bot-packages) (#223), one turn of one bot per invocation, within the owner's budget of 1 GB and 2 seconds of play a turn, plus up to 10 seconds to load a package. The backend invokes them asynchronously when a turn opens ([how server-run bots play](#how-server-run-bots-play)); [deploy/README.md](../deploy/README.md#bot-runners) has the functions' settings.

| Runner | Code | Plays |
|---|---|---|
| `<name>-bot-runner-python` | [`botrunner/python/runner.py`](botrunner/python/runner.py), the standard library only, on the `python3.14` runtime | `python` packages |
| `<name>-bot-runner-native` | [`cmd/bot-runner-native`](cmd/bot-runner-native/main.go), a Go `bootstrap` on `provided.al2023`, with its code in [`botrunner/native`](botrunner/native/runner.go) | `native` packages, such as Go bots, and hosted bots' deliveries |

**The event** is [`models.BotRunnerEvent`](models/bot_runner.go), as JSON; both runners' tests use the example in [`botrunner/testdata/event.json`](botrunner/testdata/event.json):

```json
{"package": {"id": "6f1c…", "url": "<presigned GET, valid 1 hour>", "size_bytes": 123},
 "delivery": <TurnDelivery>,
 "limits": {"load_seconds": 10, "play_seconds": 2}}
```

**A turn:**

1. **Load**, unless the instance already has this package. An instance keeps at most one package, under `/tmp/bot/<package id>`. Another package stops the worker and deletes `/tmp/bot`, then the runner downloads the zip (refusing one over 250 MiB, or of another size than `size_bytes`), checks it as the upload does ([`botpackage/`](botpackage/botpackage.go): the manifest, and no `..`, absolute paths, backslashes, links or other special files), and unpacks it, counting the bytes it writes and stopping past 1 GiB, since the sizes the zip declares come from the uploader. It starts the worker of #216 and waits for `{"ready": true}`. All of this has `load_seconds`.
   - Python: `python3.14 -m island_battles_client.worker <entry>` in the unpacked folder, with `PYTHONPATH=<unpacked>/site`. Not `-I`, which would ignore `PYTHONPATH`, while the SDK's worker is inside `site/`.
   - Native: the manifest's `command`, such as `./bot worker`, in the unpacked folder.
   - The worker gets only `PATH`, `HOME`, `TMPDIR`, `LANG`, the runtime's `LD_LIBRARY_PATH` and, for Python, `PYTHONPATH`: none of the runner's environment, such as its AWS credentials. It runs in a process group of its own.
2. **Play:** write `{"turn": <BotTurn>}` and wait at most `play_seconds` for the answer line.
3. **Submit** `{game_id, generation_id, request_id, commands, logs, memory, complete: true}` to `delivery.submit.url` with `Authorization: Bearer <submit.token>`, as a hosted bot does ([turn deliveries](#turn-deliveries)), again after no answer or a 5xx while there is time. When the bot fails, the runner submits a **pass** instead: no commands, the turn's own memory, and one `error` line from `island_battles.bots`, as the [default bots](#default-bots) pass. A bot fails when it answers `{"error": …}`, doesn't load or answer in time, crashes, breaks the protocol, or answers with something the server rejects (403, 422, or a 409 for soldiers or an upgrade level). The line says why and ends with the worker's last stderr lines, cut to the line's 500 characters.
4. A worker that timed out, crashed or broke the protocol is killed with its whole process group, and the next turn loads the package again. One that answered `{"error": …}` keeps running.

**What a runner may reach.** The submit URL must be `https` on the host in `GAME_SUBMIT_HOST` (Terraform sets the game's name), or `http` when that host is `localhost` or `127.0.0.1`, so a crafted event can't make a runner post a token, or anything else, elsewhere: such an event fails and submits nothing. The package URL must be `https`. Neither runner follows redirects. Their role may only write their own logs, so they have no AWS access and hold no secrets; a package arrives as a presigned GET.

**Hosted bots.** The native runner also takes `{"hosted": {"url": "…"}, "delivery": <TurnDelivery>}`: it POSTs the delivery to the URL with a 5-second timeout and `User-Agent: island-battles-bot-delivery/1`, and does nothing more, since a hosted bot submits for itself. The URL must pass [the hosted bot rules](#hosted-and-uploaded-bots) again, and the POST goes through `hostedbot.Client`, which connects only to public addresses on port 443, checked on the address it really connects to, uses no proxy and follows no redirects. The backend itself never calls a player's URL.

**Running a runner locally.** Each runner has a `--local` mode that plays the current turn of a player in a local game, through the same code as on Lambda: it reads the player's turn delivery from the game, serves the zip on `127.0.0.1` in place of S3, and submits to the game. Start a lobby against a local backend (`./scripts/dev.sh`), and get a player token for the player whose turn the bot plays, as for [`serve`](../clients/island-battles-go/README.md):

```sh
PLAYER_TOKEN=$(curl -s -X POST localhost:8888/lobby/v1/player-tokens -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"game_key": "island-battles"}' | jq -r .token)
# A native package, such as a Go bot:
go run ./backend/cmd/bot-runner-native --local --package bot.zip --game-url=http://localhost:8888 --lobby-id=$LOBBY_ID --token=$PLAYER_TOKEN
# A Python package:
uv run --no-project --python 3.14 python -I backend/botrunner/python/runner.py --local --package bot.zip --game-url=http://localhost:8888 --lobby-id=$LOBBY_ID --token=$PLAYER_TOKEN
```

Each prints what it did, such as `{"outcome": "submitted", "status": 200}`, or `passed` with the reason. `--load-seconds` and `--play-seconds` change the limits, and `GAME_SUBMIT_HOST` defaults to the game URL's host. Choose the player's localhost bot in the lobby, so the backend waits for the turn.

The package runs on your machine, so it must be built for it. The SDKs' `package` builds for Linux arm64, the Lambda's architecture, which runs as is only on a Linux arm64 machine. Elsewhere, build the same package for your machine, as the end-to-end tests do:

- **Go:** build the bot for your machine and zip it with the same manifest:

  ```sh
  mkdir -p /tmp/host-bot && go -C clients/island-battles-go build -o /tmp/host-bot/bot ./examples/level1
  printf '{"format": 1, "language": "native", "command": ["./bot", "worker"]}\n' >/tmp/host-bot/island-battles-bot.json
  (cd /tmp/host-bot && zip -q ../host-bot.zip bot island-battles-bot.json)
  ```

- **Python** (Linux only, for dependencies such as `pydantic-core` that hold compiled code): run the SDK's packager with your machine's platform, in the bot's project. Its `uv.lock` needs an SDK with the worker mode, newer than 0.2.0 on PyPI; the end-to-end test points the template at this repository's SDK:

  ```sh
  uv run --project <repository>/clients/island-battles-python --locked python -I -c \
    'from pathlib import Path; from island_battles_client import bundle; bundle.PYTHON_PLATFORM = "x86_64-manylinux_2_28"; bundle.package(Path("."), "bot:play", Path("dist/host-bot.zip"))'
  ```

`python runner.py --events` is the local server host's Python runner: it plays one event per line on stdin, keeping its package loaded like one Lambda instance, and answers each with its outcome; `GAME_SUBMIT_HOST` must be set.

**Tests.** The Go tests in `botrunner/native` and the Python unit tests in `botrunner/python` run each runner against fake packages, shell scripts and a stand-in for the SDK's worker, and a fake game: loading once per package, load and play timeouts, crashes, refused paths and sizes, the submit host, and hosted deliveries. Each also has an end-to-end test of a real package: the native runner plays the Go SDK's level 1 example, packaged by its `island-battles-package`, and the Python runner the [bot template](../templates/python-bot/README.md), packaged by the SDK's `package` from this repository's SDK, and both check the batch the fake game receives. On a machine that isn't Linux arm64 they package for the machine's own architecture, as above; the Python one needs PyPI and is skipped without it.

```sh
go test ./backend/botrunner/...
uv run --no-project --python 3.14 python -I -m unittest discover -s backend/botrunner/python
```

### Default bots

The **default bots** are numbered levels that the backend plays itself, in Go, so they work with any client and with every tab closed. `GET /bots/defaults` lists them in level order, each with its `level`, `name` and `description`; it needs no authentication.

| Level | Plays |
|---|---|
| 1 | The example player's strategy: each turn one soldier from its first castle with soldiers to the first enemy castle, in ID order. It never attacks neutral castles. |
| 2 | Each of its castles attacks the nearest castle, enemy or neutral, that it can take: it forecasts the defenders when its group lands, as the turn rules play it (a castle that a team owns grows by one soldier a turn and a neutral castle keeps its soldiers until it is captured, then the groups heading there land in ID order; a castle captured in a turn grows from the next turn on), and sends one more soldier plus a margin of 2 and a tenth. Otherwise it waits. It doesn't defend. |
| 3 | Level 2's forecasts, and it also keeps enough soldiers in each castle to hold against the groups heading for it and as a guard against the strongest enemy castle near it; sends help to a castle that would fall even keeping every soldier; combines attacks from several castles, the farthest sending first and the nearer ones later, so the groups land together; counts the enemy groups that its attack meets on the way; and buys speed levels 2 and 3 with soldiers it can spare from turn 15. Its memory keeps a turn counter and the launches its combined attacks have yet to send. |

The bots are deterministic: the same `BotTurn` always gives the same result, and they use no randomness. Each writes one or two game log lines a turn that explain its choice, with the logger `island_battles.bots.level1` to `level3`.

**Their code is the Go SDK's examples**, in [`clients/island-battles-go/examples/defaultbots`](../clients/island-battles-go/examples/defaultbots), so players can read, run and copy them (`go run ./examples/level3 serve` in the SDK's directory plays level 3 as a localhost bot). They are written against the SDK's types and read their speeds from the turn's upgrade table. [`bots/`](bots/bots.go) keeps the catalog, `Defaults()` and `ForLevel()`, and runs each level's example on the server's types, converting `models.BotTurn` and `models.BotTurnResult` to and from the SDK's `Turn` and `Result` through JSON; a test keeps the two sets of types carrying the same JSON. The root `go.mod` requires the SDK module and replaces it with `./clients/island-battles-go`, like the lobby module.

**Which players they play.** The game's frozen roster (`frozen_game_players`) names each player's `bot_kind`; for kind `default`, `bot_level` is the level the backend plays for that player. The start sets them from the player's [bot choice](#choosing-a-bot), or from the level of a [bot seat](#bot-seats).

**How they play.** In the transaction that creates a generation, at the start or when a turn plays, the backend gives each Go bot of a team still in the game its `BotTurn`, with the memory it returned last turn, checks the result with the rules of a completing `POST /generation/commands` batch, and stores its commands, game log lines and memory and completes its turn, before the generation is pushed. So a Go bot never times out, and a game of one human and one Go bot plays each turn as soon as the human completes it. Go bots play one after another, in player ID order, and each bot's `BotTurn.generation` shows its team's castles as the commands already accepted this turn leave them: the soldiers not yet reserved by the commands its own player stored before the backend took over, and the upgrade levels with those the commands bought. The batch check counts the same reservations. Other teams' castles show what every player sees. A Go bot's game log lines follow any its player stored for the generation; its first lines are stored up to the [limit](#game-logs) of 100 a generation, and the rest are left out, rather than the turn being rejected. A Go bot that returns an error, panics or answers with something the rules reject passes the turn: it sends no commands, keeps its memory, and its game log gets one `error` line from the logger `island_battles.bots`.

**Games that only Go bots play.** While every player still in the game is a Go bot, the turn is complete as soon as it is created. The request that got there keeps playing turns, each in its own transaction under the game lock and pushed as it commits, until the game ends or its time budget has passed, and less when the request's own deadline, such as the Lambda's, is nearer. Every game request first has the Go bots catch up the same way, so the next one continues where the last stopped, and its deadline never ends a turn that the Go bots completed. The budget depends on the request:

| Requests | Budget |
|---|---|
| The game start, every `POST /generation/commands` batch, and the deadline call | about **5 seconds** |
| The reads: `GET` state, the replay, the game log and the demo | about **1 second**, so reading a game that only Go bots play stays quick |

Every request plays at least one turn, however small its budget, so a game always makes progress. A game without Go bots pays one read of its roster for this. Locally a game plays about 60 turns a second.

### Demos

A roster player can save the game as a **demo file** at any time, and watch it later in the browser, after the lobby, the guest session and the game itself are gone.

`GET /game/from-lobby/{lobby_id}/demo` returns the demo, with the same authorization as `/generation`: a place on the game's frozen roster. Its body is the `Demo` schema in the [player API document](#player-api-document), sent as an attachment named `island-battles-game-<game_id>-generation-<n>.json`, where `n` is the number of generations played:

```json
{
  "format": "island-battles-demo",
  "schema_version": 1,
  "saved_at": "2026-10-31T12:00:00Z",
  "game_id": 12,
  "teams": [{"color": 1, "name": "Blue", "players": ["Alex"]}],
  "outcome": {"reason": "last_team_standing", "winner_team_color": 1, "summary": "Blue wins as the last team standing"},
  "frames": [],
  "logs": []
}
```

- `frames` holds every generation played when the demo was saved, in order, each the same `Generation` board view that players and the replay receive.
- `logs` holds one entry per frame, in the same order, each a `GenerationLogs` as the [game log read](#game-logs) returns it. Its lines follow that read's visibility rule when the demo is saved: a demo saved during the game holds only the saver's team's lines, and one saved after the end holds every team's. `hidden_team_colors` names the teams left out.
- `outcome` is null when the demo was saved while the game was running.
- `schema_version` is `models.DemoSchemaVersion`. Raise it whenever the format, or a type it holds such as `Generation`, changes in a way the viewer can't read. Following the [breaking-changes skill](../.agents/skills/breaking-changes/SKILL.md), the viewer opens only the current version and keeps no readers for older ones.

The request loads the generations with the replay's batched loader and the visible lines in one query, by generation ID and player, so the number of queries doesn't grow with the game. It takes no game lock, like the replay: the game is read first, so a game that ends meanwhile is saved by the stricter rule. It streams the file a frame and a generation's logs at a time, without escaping `<`, `>` and `&`. Like the other game reads, the request ends with a game [retention](#game-retention) pass.

A long game's demo is large, and the deployed backend can't send a large response quickly. The [Lambda host](#hosts-and-deploys) streams a response's first 6 MB at full speed and the rest at about 2 MB/s, and a request has 30 seconds by default, less the backend's 2-second margin. So when the request's `Accept-Encoding` allows gzip, as every browser's does, the demo is packed with gzip as it is written (`Content-Encoding: gzip`, with `Vary: Accept-Encoding`), never whole in memory. Browsers unpack it as they read it. CloudFront forwards the browser's `Accept-Encoding` to the Lambda, because its origin request policy forwards every viewer header, and passes `Content-Encoding` back unchanged; it compresses nothing itself. Bots' lines repeat their wording, so they pack well:

| Two players at the generation cap, both logging 100 lines of 500 characters every turn | Unpacked | Packed |
|---|---|---|
| Bot-like lines, with numbers that vary | 35 MiB | 4.4 MiB |
| Random characters, the worst case for packing | 35 MiB | 22 MiB |

A packed demo of bot-like lines fits in the first 6 MB. Even random lines from three players at the limit, about 52 MiB unpacked, stream in under 20 seconds. The lobby page reads at most 64 MiB, counted unpacked ([lobby frontend README](../lobby/frontend/README.md#what-a-game-provides)), so four or more players all logging at the limit every turn can pass it, and then saving reports that the demo is too large.

The replay's **Save demo** button asks the lobby page for the demo with a `game.request` message and the relay's longest wait, 120 seconds, because a large demo can take longer than the default 8 seconds to arrive. The frame has no credential, so the page fetches it ([lobby frontend README](../lobby/frontend/README.md#what-a-game-provides)). The button counts the seconds while it waits, and then the replay saves the answer with a `Blob` download under the same file name; the result frame's sandbox allows downloads. A failure is reported next to the button. Opened outside the lobby page, the replay has no **Save demo** button.

The demo viewer at `/game/demo-viewer/` is a public page from the [`demoviewer`](demoviewer/) package, embedded in the backend like the map builder. It is the replay's own template rendered without a game, so both draw a game with the same code: teams, board, castle upgrade levels, clashes, outcome and the **Logs** panel. **Open demo file** reads the file in the browser and uploads nothing. The page checks every value it draws before showing anything, writes all of the file's text with `textContent` or SVG attributes, and names the first wrong value of a damaged file, such as `frames[3].castles[0].x must be a number`. Positions must lie within a million units of the origin, so the board has a size to draw, and every player in the logs must belong to one of the file's teams. It refuses a file that isn't a demo, and a demo of another `schema_version`, saying that it comes from an earlier or later version of Island Battles. A demo starts at its first generation; play, step, jump to the last generation or scrub through it. The page says when a demo was saved before the game ended. The lobby page links the viewer as **Watch a saved demo**, through the catalog's `Links`.

Run the viewer's file checks from the repository root with `node backend/demoviewer/demo-file.test.mjs`.

### Watching a game

People outside the roster can watch a lobby's game with its join code. The generic lobby keeps them as [watchers](../lobby/README.md#watchers) and authorizes the watch channel; Island Battles decides what they see: the board, every turn, the teams and the outcome, and the game log only once the game has ended, as other teams' lines are for players ([game logs](#game-logs)).

- **Start watching** with `POST /lobby/v1/watches {"game_key": "island-battles", "join_code": ...}`. It returns the lobby's watch channel, `watch:island-battles:<lobby_id>`. The host lists watchers with `GET /lobby/v1/games/island-battles/lobbies/{lobby_id}/watchers` and kicks one with `DELETE …/watchers/{player_id}`.
- **Live turns.** Every push of `game.next_generation` and `game.ended` to the players' channels also goes to the watch channel, in the same Centrifugo request: after each turn, at the turn deadline, at the start, and for each turn that the [default bots](#default-bots) play. The watch channel's events, `WatchGenerationEvent` and `WatchGameEndedEvent`, carry a `WatchState`.
- **Reads.** `GET /game/from-lobby/{lobby_id}/watch` returns the current `WatchState`: the game and generation IDs, the watch channel, the teams with their players' names, the board, the turn's deadline and the outcome. It has none of `GenerationState`'s player-only fields: no player ID, team colour, submission, memory, bot or log count. `GET /game/from-lobby/{lobby_id}/watch/demo` returns the history in the [demo format](#demos), for scrubbing back; while the game runs it holds no log lines and each frame's `hidden_team_colors` names every team, and once it has ended every team's lines. Both admit a watcher the host hasn't kicked and an active lobby member, which the lobby checks (`domain.WatchAuthorizer`), and the game's roster players. A kicked watcher gets 403, anyone else 404; a roster player is checked first and always reads, whatever their watcher row says. A kick bars that player ID, the guest session, not the person: a new session can watch again with the code. Like the other reads they first apply an overdue turn deadline and let default bots catch up.
- **No effect on the game.** Watchers aren't on the frozen roster, so they never count toward the turn barrier or a deadline, and they never submit. They hold no lease and don't keep the lobby alive. When the lobby is deleted, its watchers can no longer read the game, although its players still can.

### Binding to the lobby

Island Battles talks to the lobby only the way a separate game would (#224): the lobby calls the game's signed **hooks**, the game reads the lobby through the lobby's signed **game API**, and it verifies the lobby's [player tokens](#player-tokens). The protocol is the lobby's ([lobby README](../lobby/README.md#binding-a-game-over-http)). Both still run in the same process and Lambda.

- **Hooks.** The backend serves Island Battles' hooks at `POST /game/lobby-hooks/<hook>` with the lobby's handler adapter, [`lobbyadapter.Hooks`](lobbyadapter/hooks.go), which refuses a request whose `Lobby-Signature` doesn't verify with 401. The validation code behind them is the game's own: `validate-settings` checks the [map choice](#choosing-a-lobbys-map) and turn timeout, `validate-seat` and `validate-member-settings` the [bots](#choosing-a-bot), with the [hosted and uploaded](#hosted-and-uploaded-bots) rules, `ready-check` the [ready gate](#choosing-a-bot), and `round-ended` whether a round's game [has ended](#several-games-in-one-lobby). A hook whose check can't run answers 503, which the player gets as `unavailable`; its cause goes on the error line of the lobby request that called it.
- **Starting.** The lobby commits a start, then calls the `start` hook with the started snapshot. It binds the round's game and initializes its first generation (`services.RunLobbyGame`), idempotently by lobby and round, so a second call returns the same game. When it fails, the start answers 502 `game_unavailable` and the lobby stays started; the host's next `POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/start` calls the hook again, until the game has confirmed the round, and changes nothing after that. The 3D client starts again by itself while the host waits for the game, and the lobby page's host has **Retry starting game**. There is no game route to bind a lobby any more: `POST /game/from-lobby` is gone.
- **Reading the lobby.** The game reads the lobby through the game API client (`gamehooks.GameAPIClient`): the lobby's snapshot for the [bot packages](#bot-packages)' membership checks and the ready check's bot seats, and `authorize-watch` for [watchers](#watching-a-game).
- **One process.** While `ISLAND_BATTLES_LOBBY_URL` is empty, the lobby's hook calls and the game's game API calls reach the other side's `http.Handler` in memory, through [`app.Loopback`](app/loopback.go), an `http.RoundTripper`: signed HTTP requests and answers, without a network, a second Lambda invocation or more concurrency. The backend also serves the lobby's game API at `/lobby/v1/game-api/…` and the hooks at `/game/lobby-hooks/…` over the network. Setting `ISLAND_BATTLES_LOBBY_URL` to a lobby's URL sends both over the network instead, for when the lobby runs on its own (#227).
- **Configuration.** The lobby reads its games from `LOBBY_GAMES`. Island Battles' entry is [`lobby_game.json`](lobbyadapter/lobby_game.json): its name, the start rule that every team needs a player, the lobby page's catalog entry and its secret's variable, `LOBBY_GAME_SECRET_ISLAND_BATTLES`, with `hooks_url` set to `ISLAND_BATTLES_PUBLIC_URL` + `/game/lobby-hooks` (or `http://localhost/game/lobby-hooks` without a public URL, whose calls never leave the process). The backend adds it to `LOBBY_GAMES`, which may list other games, unless that lists `island-battles` itself. The game reads the same secret as `ISLAND_BATTLES_LOBBY_HOOK_SECRET`; while the lobby runs in the process, startup refuses two secrets that differ. Both are at least 32 bytes, and the VM deploy generates one ([deploy README](../deploy/README.md#secrets)).
- **Documentation.** What Island Battles adds to the lobby's requests, such as the bots of `member_settings` and `seat_settings` and the map of `game_settings`, is described on the game's own schemas in [`api/openapi.json`](api/openapi.json): `MemberSettings`, `BotSeatSettings` and `LobbySettings`. The lobby's document stays the lobby's.
- **Import rule.** Outside the lobby's hosts, the backend imports no lobby Go package but `lobby/gamehooks`, `lobby/playertoken` and `lobby/domain`, whose types those two speak and whose channel names the game publishes to. The hosts build the in-process lobby until it moves: `app`, the two servers that serve it (`backend` and `cmd/lambda`), the schema (`schema`, `cmd/migrate`), the admin MCP server with the lobby's tools, the OpenAPI export, which describes the lobby's connection token route, and the test fixtures. [`lobby_imports_test.go`](lobby_imports_test.go) enforces it with `go list -deps`.

### Several games in one lobby

A lobby plays one game per round (`lobby.round` in its snapshot, from 1). Once a game has ended, any member reopens the lobby with the lobby's `POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen` ([lobby README](../lobby/README.md#playing-again-in-the-same-lobby)): it waits again in the next round with the same members, teams, bot seats, map and turn timeout, every person's ready cleared, and the host starts the next game as before. Island Battles answers the lobby's `round-ended` hook ([`lobbyadapter/registration.go`](lobbyadapter/registration.go)): a round is over once its game has an outcome, and also when no game exists for it, so a start that failed to bind a game never keeps the lobby from reopening. While the game runs, reopening fails with `game_running`.

- **Binding.** A game stores its lobby and round (`games.lobby_id`, `games.lobby_round`, unique together), and `dal.BindLobbyGame` binds by the round of the started snapshot, so the second start makes a second game. The migration drops the old unique index on `lobby_id` alone (`idx_games_lobby_id`), which AutoMigrate never drops; games stored before it are round 1.
- **The newest game answers.** Every `/game/from-lobby/{lobby_id}/…` route serves the lobby's newest game, the one of its highest round (`dal.FindLobbyGame`), and authorizes against that game's frozen roster: a player of the first game who isn't on the second's roster gets 404 from them once the second starts, and can't send its commands. An older game can't be reached once the next one starts; save a demo first. Between games, while the lobby waits, the newest game is still the one that ended. `GenerationState`, `WatchState` and the replay data carry `lobby_round`, so a client can tell whether a state is of the round it shows, and `GET …/replay?lobby_round=N` answers 404 until the newest game is of round N; the lobby page's result URL asks for its round that way.
- **Bots.** Bot choices are the members' member settings and bot seats belong to the lobby, so both carry over to the next round; each game copies them at its start.

`lobbyadapter` holds Island Battles' side of the lobby: its hooks and its `LOBBY_GAMES` entry ([binding to the lobby](#binding-to-the-lobby)). It uses types directly from `models`, without aliases. Island Battles persistence lives in `dal/lobby_binding.go`; its only translation is from generic team UUID/order to simulation colors 1..N, with `models.NeutralTeamColor` (0) reserved for neutral. Clients expose the same value as `NEUTRAL_TEAM_COLOR`.

## Turn simulation

`services.MigrateGeneration` plays a turn in three steps. `dal.LoadGameState` locks the game and reads its latest generation once. `simulation.Simulate` applies the turn's commands, then the end-of-turn steps (movement and clashes, castle growth, arrivals; only castles that a team owns grow, and a neutral castle keeps its soldiers until a team captures it, so a castle captured in a turn grows from the next turn on), to the in-memory `simulation.GameState` and returns the clashes as events. `dal.SaveGeneration` stores the result as the next generation with one batched insert per table. The game rules live only in `backend/simulation`, which never touches the database, so its tests run without Postgres.

## Castle upgrades

Castles and soldier groups persist an `upgrades` map, such as `{"speed":1,"clash_stop_generations":1}`. Each upgrade type defines its own level effects, fixed soldier costs, and maximum in `models/upgrades.go`, the only copy of the table. Speed currently has four levels: **1 → 0.5**, **2 → 1**, **3 → 1.5**, and **4 → 2** world units per turn. The `clash_stop_generations` type has three levels, reducing stoppage from **3** to **2** to **1** generations. Purchasing its levels **2** and **3** costs **10** and **20 soldiers**. Omitted upgrades default to level 1, and every created castle and group stores all of its levels. Map templates can provide initial castle levels. Purchasing speed levels **2**, **3**, and **4** costs **10**, **20**, and **30 soldiers**, respectively; the default level 1 has no purchase cost.

`GET /game/upgrades` serves the table without authentication: every type, sorted by type, with a description and, for each level, its effect (`value`) and the soldiers it costs to reach (`soldier_cost`). The map builder's `upgrades.json` is the same table. Every castle and soldier group in a player's state, its events and the replay carries `upgrades` with every type's level; a group copies its origin castle's levels when it leaves.

Players buy levels with `upgrade_castle` in `POST /generation/commands`:

```json
{"command_type": "upgrade_castle", "parameters": {"castle_id": 0, "upgrade_type": "speed"}}
```

Each command buys the type's next level for a castle the player's team owns and pays that level's cost from the castle's soldiers. A castle may be upgraded several times in one turn, also of the same type: speed 1 → 2 → 3 in one turn costs 10 + 20 soldiers. Registration counts every upgrade and send already accepted this turn, in the player's earlier batches, and rejects an upgrade past the maximum level or beyond the castle's unreserved soldiers with the problem types in [Interactive Island Battles generations](#interactive-island-battles-generations). Spending the exact balance is allowed.

Commands execute in the supplied order before movement. Sending troops after upgrading copies the new levels; troops sent before upgrading retain the old levels. Every group keeps its own snapshot throughout later turns. The purchase atomically deducts the target level's cost and advances the upgrade; an exact balance leaves zero soldiers before normal turn production. The engine still checks each command when it runs, and returns typed model errors for insufficient soldiers, maximum levels, unknown upgrade types and invalid levels; a rejected command rolls back the whole turn, which registration prevents for player commands.

## Map files

A map file is the JSON that `POST /map/create` stores as a default map, published as the `MapTemplate` schema in [`api/openapi.json`](api/openapi.json):

```json
{
  "map_name": "crossing",
  "player_amount": 2,
  "plane_size_x": 8,
  "plane_size_y": 6,
  "neutral_starting_soldier_amount": 10,
  "castles": [
    {"team_color": 1, "x": -3, "y": 0, "starting_soldier_amount": 30, "upgrades": {"speed": 2}},
    {"team_color": 2, "x": 3, "y": 0, "starting_soldier_amount": 30},
    {"team_color": 0, "x": 0, "y": 2.5}
  ]
}
```

- The plane is centred on the origin, so a castle's `x` lies from `-plane_size_x/2` to `plane_size_x/2` and its `y` from `-plane_size_y/2` to `plane_size_y/2`. The replay and the game graph draw it the same way.
- `player_amount` is the number of teams the map is made for, from 2 to 16, the lobby's limits; each team has one player.
- `team_color` runs from 1 to `player_amount`, and 0 is neutral. Every team colour from 1 to `player_amount` owns at least one castle, so every team starts with one.
- A castle without `starting_soldier_amount` starts with `neutral_starting_soldier_amount`. Omitted upgrades start at level 1.
- A castle's position in `castles` is its castle ID in every game built from the map.

Only admins create default maps. Send the deployment's admin API key, `ISLAND_BATTLES_ADMIN_API_KEY`, in the `X-Admin-API-Key` header:

```sh
curl -X POST http://localhost:8888/map/create \
  -H "X-Admin-API-Key: $ISLAND_BATTLES_ADMIN_API_KEY" -H 'Content-Type: application/json' \
  --data @crossing.json
```

The key is compared in constant time, before the body is read. A missing or wrong key returns 401. When the backend has no key set, the endpoint is disabled: every request returns 403, and startup logs a warning. `scripts/dev.sh` reads the key from `.env`; an `.env` copied from an older `.env.example` lacks it, so add the line from `.env.example`.

`models/map_template.go` holds the format and the functions every map source uses: `models.ValidateMapTemplate` checks a map without storing it and returns a `*models.MapTemplateError` that lists each invalid field, such as `castles[2].x`; `models.BuildMap` builds the game map. Read map files with `models.DecodeMapTemplate`, not plain `encoding/json`: it rejects unknown keys and missing required fields, which `encoding/json` ignores or reads as 0, then validates the map. `/map/create` answers an invalid map with 422 and one error per field, stores nothing, and answers a `map_name` that another map uses with 409. Stored templates keep working: games built from them are not validated again, and castles stored before `starting_soldier_amount` existed start with `neutral_starting_soldier_amount`.

`POST /map/validate` checks a map file with `models.DecodeMapTemplate` and stores nothing, so it is safe to leave public. Any non-empty body answers 200 with `{"valid": ..., "errors": [...]}`, each error located relative to the file, such as `castles[2].x`. A `*models.MapTemplateError` lists at most 100 problems and counts the rest in a last detail, and it echoes only scalar values, never a whole object, so a large invalid file gets a small answer. A `map_name` that a default map already uses is not a problem here.

### Map builder

Open `/map/builder/` to build a map in the browser, from the [`mapbuilder`](mapbuilder/) package embedded in the backend. Set the map's name, team count, default soldiers and plane size; add castles with **Add castle** or a double-click on the plane, drag them or move them with the arrow keys, and remove them with **Delete**. Each castle has a team colour or neutral, an optional starting soldier count and its upgrade levels, which the page reads from `upgrades.json` next to it, the same table as `GET /game/upgrades`, so they follow `models/upgrades.go`.

The page holds no copy of the map rules. Each edit sends the file to `POST /map/validate`, and the page shows each problem next to the castle and field it names. **Download map** saves exactly the text that the server last accepted; while the map is invalid or unchecked, the button is disabled and the page says why. **Open map file** loads a file to keep editing; a downloaded file reopens unchanged. A hint names any team from 1 to `player_amount` that owns no castle, which the server refuses.

When the lobby's create form opens the page with its **Build a map…** button, the page also shows **Use in lobby** next to **Download map**. It needs what Download needs; while it is disabled, the page says why. It posts `{"type": "island-battles.map-file", "text": ...}` with exactly the accepted text to the form's window, with this page's origin as the target origin, and says so on the page. The button appears only when a same-origin window opened the page and is still open; when that window closes, the page hides the button and says to download the map instead. A form that reloaded while the builder stayed open names itself to the builder with an `island-battles.lobby-page` message, so the builder sends to the new page and keeps its unsaved map. Opened directly, the page shows no such button.

Run the page logic's checks from the repository root with `node backend/mapbuilder/builder.test.mjs`.

### Choosing a lobby's map

An Island Battles lobby starts on the map its `game_settings` choose, published as the `LobbySettings` schema:

- `{"map_template_id": 4}` picks a default map. `GET /map/templates` lists them with each map's name, `team_count`, plane size and castle count.
- `{"map": {...}}` carries a whole map file, such as one downloaded from the map builder. It must pass `models.DecodeMapTemplate`.
- `{}`, or no settings, starts on the generated map, with one castle per team.

Pass at most one of the two. Either way, the settings may also set `turn_timeout_seconds`; see [turn deadlines](#turn-deadlines). The map sets the lobby's teams and its neutral castles, so a lobby on a map with `player_amount` N has exactly N teams, and the map's castle team colours are exactly 1 to N, with 0 neutral. The lobby's first team plays colour 1, its second colour 2, and so on. `GET /map/templates` leaves out stored maps that fit no lobby: those whose colours skip one, and those made for fewer than 2 or more than 16 teams, the lobby's limits. A bad choice fails lobby creation, or a change of the settings, with `invalid_game_settings`, and the detail names the invalid fields, listing the first 20 and counting the rest, such as `game_settings.map_template_id: no default map has ID 9` or `game_settings.map.castles[2].x: must be inside the plane, from -5 to 5`. Settings are capped at the lobby's 16 KiB, which holds a map file of over a hundred castles.

The lobby frontend's create form offers the same choice through the catalog's settings module, [`demo/lobby-settings.mjs`](demo/lobby-settings.mjs): the generated map, a default map, or a loaded map file. The generated map shows a **Teams** select from 2 to 6 (2 is selected), and the lobby gets that many teams, since the generated map fits any number; the room's **Change map** shows the lobby's current count. Picking a map fills in one team per colour, named after the colour the board draws (Blue, Red, Green, Purple, Amber, Teal), with one seat each, and the room shows the chosen map's name and the turn limit, such as `Map: Strait · Turn limit: 30 s`. A **Turn timeout** select below the map sets `turn_timeout_seconds`: 10 s, 30 s, which is selected, 2 min, 10 min, or No limit, which sends `null`. The board fits itself to the map's castles, keeping at least the generated map's 8 by 6 units.

The form's **Build a map…** button opens [the map builder](#map-builder) in a window named `island-battles-map-builder`, which keeps a reference to the form, and clicking it again focuses that window, also after the form reloaded, because the form asks the browser for the window by name before it navigates anything. When the builder's **Use in lobby** posts a map, the form selects **Load a map file…** with that map, as if the host had loaded its file, and names the map, its size and its teams. The form accepts the message only when its origin is the form's own origin and its source is the window the form opened; it ignores everything else. A message that holds no usable map shows its error as a bad file does. The latest map or file the host picks wins.

### Changing the map before the start

While the lobby waits, the host can change the map and the turn timeout as often as they like, with the lobby's `PUT /lobby/v1/games/{game_key}/lobbies/{lobby_id}/settings` and `{"teams": [...], "game_settings": {...}}`. [`lobbyadapter.Registration`](lobbyadapter/registration.go) checks the new settings as on create: the default map must exist, a map file must decode, the map's `player_amount` must equal the number of teams sent, and `turn_timeout_seconds` must be 5 to 600 or null; otherwise `invalid_game_settings`. Send one team per colour of the new map, named as the create form names them. The lobby rebuilds its teams by position, so colour 1 stays the first team and keeps its player; a new map with fewer teams leaves the players of the removed teams without a team and removes the bot seats on them, and one with more teams adds empty ones. Every person gets ready again; bot seats stay ready, and each member's [bot choice](#choosing-a-bot) is kept, because it belongs to the membership. The start uses the map and timeout set last.

In the lobby page, the host of a waiting room gets **Change map** under the room's settings line. It opens the same form as the create form, with the generated map, the default maps, a map file, **Build a map…** and the turn timeout, showing the lobby's current choice; **Save** sends it. Every member's room then shows the new map and turn limit, the new teams, and their ready cleared, without reloading. The 3D client's room screen has the same **Change map** button ([its README](../clients/island-battles-web/README.md)).

## Schema and recovery

The hosts never change the schema. The migrate command, [`cmd/migrate`](cmd/migrate/main.go), does, once per deploy and before the new build serves; `scripts/dev.sh`, the acceptance and conformance runners and the Go test helpers (`testutils.InitTestDB`) run it, or the `dependencies.MigrateSchema` function behind it, first. It reads only `ISLAND_BATTLES_DATABASE_URL` and the optional pool settings. It first applies two sets of versioned SQL migrations, which keep existing rows: the lobby's (`lobby/postgres/migrations/`, recorded in `lobby_schema_migrations`) and the map templates' (`backend/dal/migrations/`, recorded in `map_template_schema_migrations`). Each set runs in one transaction under a PostgreSQL advisory lock, so concurrent runs of a set wait for each other. A database created before the schema was versioned is recorded at migration 1 without running it, but only if it has every table and column that migration 1 creates; otherwise the command fails and names what is missing. It then AutoMigrates the game tables and game-team bindings, backfills `frozen_game_players.bot_kind` once in a database that had no such column ([freeze](#choosing-a-bot)), drops the retired `island_lobby_bot_choices` table (the [bot choice](#choosing-a-bot) is lobby member settings now) and the retired `generation_notifications` table, and drops the `games.replay` column, because replays are now derived from generations. The game steps take no lock, so never run two migrate commands against one database at once. Running it again finds nothing to do. Game data is not migrated: when a schema change strands it, reset the game tables (see the [breaking-changes skill](../.agents/skills/breaking-changes/SKILL.md)). New games use the nullable unique `lobby_id` link. Creating or joining a lobby does not create a game; starting an Island Battles lobby initializes generation one.

Lobby invalidations and generation events are pushed after their transaction commits, through the lobby's `realtime.Publisher`. It sends a request's messages to Centrifugo's HTTP API in one call and retries what failed twice within about a second. If Centrifugo is still unreachable, it logs an error and the request succeeds anyway, because the change is already committed. Nothing is queued for later. Generation events contain the recipient's authorized generation state and go only to that player's personal channel. Clients reconcile authorized snapshots after subscription/reconnect, so a failed push or a restart recovers through HTTP state.

## Game retention

A game is deleted with all its rows 7 days after its latest generation was created, or after the game itself was created if it never got a generation. Finished and abandoned games are treated alike, because an abandoned game never gets an outcome. The replay goes with the game, because it is built from the generations. Map templates (`map_templates`, `castle_templates`) are never deleted: a game's maps are copies of them. The game's [`retention`](retention/retention.go) package and [`dal/game_retention.go`](dal/game_retention.go) own this. They never read lobby tables, and the lobby deletes its own data separately.

| Variable | Default | Meaning |
| --- | --- | --- |
| `ISLAND_BATTLES_GAME_RETENTION_PERIOD` | `168h` | How long a game is kept after its latest generation, as a positive Go duration. Go durations have no days unit, so 7 days is `168h`. |

There is no timer. The game's own requests run the passes as they end: turns (`POST /generation/commands`), which are what adds game data, and the state, replay, game log and demo reads (`GET /game/from-lobby/{lobby_id}/generation`, `/replay`, `/logs` and `/demo`). When nobody plays, no game data is added, so none needs deleting. The pass runs before the response is sent, so the request waits for it. Each process runs at most one pass at a time, and at most one a minute while passes find no backlog. A pass that fills a batch lets the next request run another, so a backlog drains one batch per request. A turn adds one generation and a pass deletes up to 10, so deleting keeps ahead of playing. A pass has a 5-second timeout, and a failed pass is logged without failing the request. Against a local database a pass takes under a millisecond when there's nothing to delete and 5 to 7 ms for a full batch. On Lambda every statement, and every BEGIN and COMMIT, is a round trip over TLS to the VM: 5 for a pass with nothing to delete, about 20 for a batch from one long game, and about 50 for a pass that finishes 3 small games, the most a pass purges. That is a few to about a hundred milliseconds at a few milliseconds a round trip, at most once a minute per instance while there's no backlog, and on every game request while there is one.

A pass has three steps. Each runs even when another fails, and the pass logs every failure:

1. **Retire** up to 10 expired games by soft-deleting the game row. Every request finds and locks its game through GORM's default scope, so a retired game disappears from every endpoint at once, and its turns and reads return 404. The step skips games that a request holds locked (`FOR UPDATE SKIP LOCKED`), so it never waits for a turn and never retires a game while a turn on it is in progress. It checks each locked candidate again in a new statement, so a turn that commits just before the lock keeps its game.
2. **Purge** up to 10 generations of at most 3 retired games, lowest game IDs first, with `Unscoped`, so rows are removed for real. Each generation goes with its requests, player inputs, game log lines, commands and soldier groups, then the generation, then its map's castles and the map. A retired game with no generations left then goes with its remaining requests, bot memory, frozen players and team bindings. Every game is purged in its own transaction, so a game whose purge fails is rolled back and retried by later passes while the other games are still deleted. A long game takes several passes, and no transaction deletes more than 10 generations' rows.
3. **Delete bot packages** older than 8 days, up to 10, oldest first, in one statement: the rows of `island_bot_packages`, pending or ready, as the [bot package](#bot-packages) bucket's lifecycle rule deletes their zips 8 days after their upload. This period is fixed and doesn't follow `ISLAND_BATTLES_GAME_RETENTION_PERIOD`.

The rows a pass deletes belong to games nobody can reach any more, and the indexes on the foreign key columns (`generations.game_id` and `map_id`, `castles.map_id`, `commands.generation_id`, and `soldier_groups.generation_id`, `origin_castle_id` and `destination_castle_id`) keep the deletes and their foreign key checks from scanning other games' rows. Passes on several hosts at once lock different games, so they never wait for each other either.

## Hosts and deploys

[`app.New`](app/app.go) builds the HTTP handler from the configuration and a database connection. Two hosts serve it:

- **The HTTP server,** [`backend/main.go`](main.go), for local development. It listens on `ISLAND_BATTLES_HTTP_ADDR`, shuts down gracefully on SIGINT or SIGTERM and logs in colour through `tint`. It alone passes `app.WithGameGraph()`, so it alone serves `GET /game/{id}/graph`, which reads any game without authentication.
- **The AWS Lambda function,** [`cmd/lambda`](cmd/lambda/main.go), behind a Function URL, which players reach through CloudFront at the game's name ([deploy/README.md](../deploy/README.md)). It is public, so it serves no graph page, and every route it serves that reads game data requires a player on the game's frozen roster. It serves the handler through `lambdaurl`, so the Function URL must use the `RESPONSE_STREAM` invoke mode. Streaming also lets replay pages, which inline JSON that grows with the game, and [demos](#demos) pass Lambda's 6 MB limit on buffered responses; streamed responses may reach 200 MB. It logs warnings and errors only, as JSON lines with the `level` and `timestamp` keys that Lambda's log-level filtering reads, within the limits in [Logs and traces](#logs-and-traces).

Build the Lambda for the `provided.al2023` runtime on arm64. The runtime runs the executable named `bootstrap` at the root of the zip:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -trimpath -ldflags='-s -w' -o bootstrap ./backend/cmd/lambda
zip -j bootstrap.zip bootstrap
```

A deploy runs `go run ./backend/cmd/migrate` (or the same command built for the deploy machine) against the production database first, then publishes the new code. A cold start runs no DDL, and its first connection gives up after `dependencies.ConnectTimeout`, 5 seconds, well inside Lambda's 10 seconds for init. The Lambda has no VPC and reaches PostgreSQL over the internet, which accepts only TLS connections. `sslmode=require` encrypts the connection but doesn't check the server's certificate. To check it, bundle the VM's certificate with the Lambda and use `sslmode=verify-full&sslrootcert=<path>`, as [the deploy guide](../deploy/README.md#the-lambdas-settings) describes. Every page the backend serves, including the lobby UI, the browser demo and the local host's `GET /game/{id}/graph`, is embedded in the binary.

### The connection pool

Every process has its own pool, and every Lambda instance is a separate process. These optional settings bound it:

| Variable | Default | Meaning |
| --- | --- | --- |
| `ISLAND_BATTLES_DATABASE_MAX_OPEN_CONNECTIONS` | 2, and 10 on the local server host | Most connections open at once |
| `ISLAND_BATTLES_DATABASE_MAX_IDLE_CONNECTIONS` | 2, or the open limit if lower | Most connections kept open while idle |
| `ISLAND_BATTLES_DATABASE_CONNECTION_MAX_IDLE_TIME` | `1m` | How long an idle connection stays open, as a Go duration |

A Lambda instance serves one request at a time, so the defaults suffice there. The local server host serves many at once, and the lobby holds a connection and a lobby's lock while it calls a game hook, such as Island Battles' ready check, which may read with a second connection while other requests to that lobby hold theirs, waiting for the lock ([binding to the lobby](#binding-to-the-lobby)); so it opens up to 10 by default. Cap the function's reserved concurrency so that **reserved concurrency × max open connections < PostgreSQL `max_connections`** (100 by default), leaving room for the migrate command and administration. For example, with the default of 2 connections, the default reserved concurrency of 5 uses at most 10. The free-tier budget below also grows with the concurrency. A frozen Lambda instance keeps its idle connections until it thaws or ends, so count them as open.

If PgBouncer is added later in transaction mode, it must be 1.21 or newer with `max_prepared_statements` set, because pgx uses named prepared statements by default.

### Logs and traces

[`observability`](observability/) gives both hosts the same request logging (#46):

- **Every request** is logged once, after it is answered. A 5xx answer or a recovered panic is one ERROR line, `Request failed` or `Request panicked`, with the operation, route, status, the error chain and the IDs involved: `lobby_id`, `game_key`, `game_id`, `generation_id` and `player_id`, as far as the request got. A panic also carries its value and stack, and answers 500. Every other request, client errors included, is an INFO line.
- **Recording causes:** a handler that turns an error into a response without its cause calls `observability.RecordError(ctx, err)` first, and `observability.Annotate` adds IDs. Huma's 5xx problems and the lobby's problems are recorded on their own, with what they wrap. A Huma API middleware, `observability.NameOperations`, names the operation before any group middleware runs. The answers that bypass Huma's transformers record their causes through hooks: the app's authenticator wrapper for the lobby's authentication problems, and the Centrifugo proxy's `realtime.WithErrorReporter` for its 503s.
- **Failed queries** are warnings, like slow ones: some fail as expected, such as a unique violation that answers 409.
- **Traces** are off unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set, as OpenTelemetry's standard variable. Then each request, each GORM query and each Centrifugo API call gets a span. A request's sampled spans are exported over OTLP/HTTP before it returns, in its own goroutine: there's no batch processor, timer or kept-alive connection to the collector, so nothing runs between requests. Locally every request is traced. Trace headers on incoming requests are ignored, so callers can't start traces. A line logged inside a sampled trace carries `trace_id` and `xray_trace_id`.

The Lambda host also applies limits that keep CloudWatch Logs and X-Ray inside AWS's free tier, worst case included. Terraform sets them, and refuses them in `lambda.env` ([deploy/README.md](../deploy/README.md#logs-alerts-and-traces)):

| Variable | Default | Meaning |
| --- | --- | --- |
| `ISLAND_BATTLES_CONCURRENCY` | 5 | C: the most instances that run at once, the function's reserved concurrency |
| `ISLAND_BATTLES_LOG_LINES_PER_MINUTE` | 5 | N: the most warning and error lines an instance writes a minute. Warnings may use N−1. The rest become one line that counts them |
| `ISLAND_BATTLES_LOG_LINE_BYTES` | 1024 | L: the most bytes of a line. Longer lines are cut and stay JSON |
| `ISLAND_BATTLES_TRACE_INTERVAL` | `180s` | T: an instance starts at most one trace every T, the first one T after it starts |

At startup the Lambda host works out the worst case for a 31-day month, C × 44,640 minutes × (N × (L + 128) + 384) bytes of logs and C × 2,678,400 s / T traces. Over 1.5 GB or 80,000, it refuses to serve: it answers 503 and logs why. A backend that fails to start for any other reason, such as an unreachable database, does the same and tries again at most every 10 seconds, rather than failing the cold start, which Lambda would retry on every request. The local server host applies none of these limits.

## Admin MCP server

[`cmd/admin-mcp`](cmd/admin-mcp/main.go) is a [Model Context Protocol](https://modelcontextprotocol.io) server (#79) that answers questions about lobbies, players and games, such as "How many lobbies are currently active?" or "How many players are in lobby X?". It runs on your own machine, serves one client over stdio, and connects straight to PostgreSQL, the local database or production. It adds no endpoint to the backend and is never deployed. It's built on the official Go SDK, `github.com/modelcontextprotocol/go-sdk`.

| Tool | What it answers |
| --- | --- |
| `overview` | Lobby counts by game key and phase; active lobbies, which have a connected member; members connected now; live guest sessions; games running and finished; generations created in the last hour |
| `list_lobbies` | Lobbies, newest first, by game key, phase or only active ones, with their member and connected counts, host, times and game |
| `get_lobby` | One lobby by ID or join code: also its settings, teams, members with their leases and grace deadlines, start roster and retention deadline |
| `find_players` | Players by ID or by part of a display name, with their live sessions (times only), lobbies and start roster places |
| `list_games` | Games, newest first, running or finished, with teams, roster, generation count, outcome and retention deadline |
| `get_game` | One game by ID or lobby ID: also its current turn, who has completed it, and each team's castles and soldiers |
| `get_generation` | One generation by ID or turn: each player's commands and completion, the commands that produced it, its clashes, and optionally the board that players receive |
| `describe_schema` | The tables, or one table's columns, from `information_schema` |
| `query` | One SQL query, for everything else |

A member is connected while one of the leases of its current membership has neither expired nor been released. The tools work this out from the timestamps, as the lobby does, but never apply expiry themselves, so a lobby that nobody has touched since its members left may still be waiting. Answers are JSON with stable field names and times in UTC as RFC 3339. No answer contains `lobby_sessions.bearer_hash`, a connection token or any other secret.

The lobby tools are the lobby's own, [`lobby/adminmcp`](../lobby/README.md#admin-mcp-tools), which reads only the `lobby_*` tables, so another game can reuse them. The binary adds the Island Battles game tools and the SQL tools. The game tools read only the game's tables: they name a game's players by the display names that its frozen roster keeps from the start, not from the lobby's players.

**It doesn't write.** Its one connection's sessions start with `default_transaction_read_only=on` and a 5-second `statement_timeout`, and every tool runs in a read-only transaction that is rolled back. After every call it releases the session's advisory locks, which outlive the rollback and would otherwise block the migrations until the server exits. `query` runs the statement through a cursor (`DECLARE ... CURSOR FOR <sql>`), which takes only a `SELECT`, `VALUES`, `TABLE` or `WITH` query and fetches at most one row more than its cap. Commands such as `SET`, `COPY` or `COMMIT` are therefore syntax errors, and the read-only transaction refuses anything in a query that writes, such as `nextval()` or `FOR UPDATE`. Statements go over PostgreSQL's extended protocol, which refuses a string of several statements. Results are cut to 200 rows and 64 KiB, and values to 16 KiB, and `truncated` says when. The driver still reads each whole row into memory first, so one huge value, such as a `repeat()` of many megabytes, costs about twice its size in memory on your machine, within the statement timeout.

**The guarantee is complete only for the production read-only role**, which is no superuser and may only read. A superuser can do much that a read-only transaction doesn't stop, such as reading server files, ending other sessions or creating replication slots, and the local development role is one. When the server connects as a superuser, it logs a warning and its sessions `SET ROLE pg_read_all_data`, which reads every table and nothing more. That is best-effort: a query can switch the role back with `set_config` for its own duration.

**`bearer_hash` is protected only by the production role's column grant**, which makes `lobby_sessions.bearer_hash` unreadable. The typed tools never read it. Locally, `query` also leaves out any result column named `bearer_hash`, but that is best-effort: an alias or `row_to_json` gets past it. If a migration outruns the production role's grants and the role can read the column, `query` refuses to run, and the server logs why at startup and on each refused call, until a VM deploy grants them again ([the deploy guide](../deploy/README.md#the-admin-mcp-server)).

It requires `ISLAND_BATTLES_ADMIN_DATABASE_URL`, and reads the optional [`ISLAND_BATTLES_GAME_RETENTION_PERIOD`](#game-retention) the same way the backend does, to say when retention deletes a game. Set it only if the backend you ask about sets it. It logs to stderr only, because stdout carries the MCP stream. Locally, use the development database's URL from `.env`. For production, use the read-only role described in [the deploy guide](../deploy/README.md#the-admin-mcp-server).

Register it with Claude Code from the repository root, which `go run` needs as its working directory:

```sh
claude mcp add island-battles-admin \
  --env ISLAND_BATTLES_ADMIN_DATABASE_URL='postgres://islandBattles:islandBattles@localhost:5432/islandBattles?sslmode=disable' \
  -- go run ./backend/cmd/admin-mcp
```

`go run` compiles it on every start. A built binary starts at once and runs from any directory:

```sh
go build -o ~/.local/bin/island-battles-admin-mcp ./backend/cmd/admin-mcp
claude mcp add island-battles-admin --env ISLAND_BATTLES_ADMIN_DATABASE_URL=... -- ~/.local/bin/island-battles-admin-mcp
```

Rebuild it after pulling changes to the tools. `claude mcp list` shows whether the server connects, and `/mcp` inside Claude Code shows its tools. Other MCP clients start the same command with the same environment variable. To use the local database and production side by side, register the server twice under different names.

## Verification

```sh
go test ./... ./lobby/...
go vet ./... ./lobby/...
go test -race ./lobby/... ./backend/lobbyadapter/... ./backend/dal/...
./scripts/test-lobby-acceptance.sh
```

Database tests require a Docker-compatible runtime and use disposable PostgreSQL. On rootless Podman, set the runtime up first as described in [Database tests on rootless Podman](#database-tests-on-rootless-podman). The live runner supports Linux Docker or Podman, creates unique test containers, and preserves failure logs. It never uses the configured developer database.

Castle and soldier group IDs stay stable across generations. Backend Go models call them `PersistentCastleID` and `PersistentSoldierGroupID`, stored in `persistent_castle_id` and `persistent_soldier_group_id`; the API calls them `id`, `origin_castle_id`, `destination_castle_id`, `group_id` and, for castle upgrades, `castle_id`. Soldier group IDs come from a per-game counter (`games.next_soldier_group_id`), so a defeated or arrived group's ID is never reassigned.

### Database tests on rootless Podman

The Go database tests start their PostgreSQL containers through testcontainers-go. Rootless Podman runs them without code changes once two things are set up.

1. Enable Podman's Docker-compatible API socket. It's off by default:
   ```sh
   systemctl --user enable --now podman.socket
   ```
2. Point testcontainers at the socket and disable Ryuk, its cleanup container. Ryuk asks for Docker's default `bridge` network, which rootless Podman calls `podman`, so with it enabled every database test fails with a `reaper:` error. To configure the shell:
   ```sh
   export DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock
   export TESTCONTAINERS_RYUK_DISABLED=true
   ```
   IDEs started from the desktop often don't load shell rc files. To configure the terminal and IDEs at once, put the same settings in `~/.testcontainers.properties` instead, replacing `<uid>` with the output of `id -u`:
   ```properties
   docker.host=unix:///run/user/<uid>/podman/podman.sock
   ryuk.disabled=true
   ```
   testcontainers-go v0.40.0 resolves the Docker host in this order: its `tc.host` setting, the `DOCKER_HOST` variable, the Docker context, `/var/run/docker.sock`, and only then the `docker.host` property. If `/var/run/docker.sock` exists, for example from the `podman-docker` package, it wins over the property.

With Ryuk off, normal runs still remove their containers through `t.Cleanup`. An interrupted run (Ctrl+C, stopping the run in an IDE, or a test timeout) leaves its PostgreSQL containers behind. List them with `podman ps -a --filter label=org.testcontainers=true` and remove them with `podman rm -f`. Don't use `podman container prune` for this: if they're stopped, it also removes the `island-battles-dev-db` and `island-battles-dev-centrifugo` containers that `scripts/dev.sh` creates.

## Player API document

[`api/openapi.json`](api/openapi.json) describes the operations a player client calls, including `GET /game/upgrades` and `GET /bots/defaults` ([default bots](#default-bots)), the `GenerationEvent` and `GameEndedEvent` pushed on its channel, the `MapTemplate` [map file format](#map-files), the `Demo` [demo file format](#demos), and `BotTurn` and `BotTurnResult`, the [bot turn contract](#bot-turns). Player clients are generated from it; see the [conformance suite](../clients/conformance/README.md) for the behavior they implement by hand. API view types are published without their `View` suffix (`CastleView` becomes `Castle`). After changing them, update the document; a test fails while it is stale:

```sh
go run ./backend/cmd/export-openapi --output backend/api/openapi.json
```
