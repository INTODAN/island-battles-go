// Generated from backend/api/openapi.json by tools/codegen/generate.ts. Do not edit; run `bun run generate`.

export interface paths {
    "/account": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read your account: your hand play and your bot names
         * @description Requires the player token of a signed-in player, which names their account; a guest's token is 403 with problem type urn:island-battles:problem:sign-in-required. Every call stores the account the first time the game sees it, with its hand play, and the display name of the token whenever it changed, so a rename in the lobby reaches the game with the next player token. A competitor is what gets a rating: each bot name an account registers, and the account's hand play, shown under its display name. Each starts at a rating of 1500. Returns the account's ID and display name, max_bot_names (5), its hand play and its bot names, oldest first, each bot name with its hosted bot's url, which is null for a name registered before names had URLs.
         */
        get: operations["getIslandAccount"];
        put?: never;
        post?: never;
        /**
         * Delete your account's part of the game
         * @description Requires the player token of a signed-in player; a guest's token is 403 with problem type urn:island-battles:problem:sign-in-required. Deletes every competitor of the account, its bot names and its hand play with their ratings, and the game's record of the account, unless the account is hidden from the leaderboard: then the game keeps its record, with its display name, so that it stays hidden, with its new hand play and bot names, when it plays again. Call it before the lobby's DELETE /lobby/v1/account, so the game's part is gone first. Deleting an account the game doesn't know is 204 too.
         */
        delete: operations["deleteIslandAccount"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/account/bot-names": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Register a bot name in your account, with its hosted bot's URL
         * @description Requires the player token of a signed-in player, which names their account; a guest's token is 403 with problem type urn:island-battles:problem:sign-in-required. Every call stores the account the first time the game sees it, with its hand play, and the display name of the token whenever it changed, so a rename in the lobby reaches the game with the next player token. Registers a bot name, a competitor at a rating of 1500, with the URL of the hosted bot that plays it, and returns it. A name is 3 to 24 characters after trimming, of letters, digits, spaces, - and _, and can't start with Bot (level, otherwise 422. The url is required and must pass a hosted bot's check (https:// on port 443, without credentials, on a host name whose addresses are all public), otherwise 422. In a rated lobby the name plays as a hosted bot at this URL. Once the name has played a rated game, the leaderboard lists it with its URL, so that others can play against it. Names are unique across all accounts without regard to case: a taken name is 409 with problem type urn:island-battles:problem:bot-name-taken. An account has at most 5 bot names: a sixth is 409 with problem type urn:island-battles:problem:bot-name-limit. A name can't be renamed: delete it and register another.
         */
        post: operations["registerIslandBotName"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/account/bot-names/{competitor_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /**
         * Change the hosted bot URL of one of your bot names
         * @description Requires the player token of a signed-in player, which names their account; a guest's token is 403 with problem type urn:island-battles:problem:sign-in-required. Every call stores the account the first time the game sees it, with its hand play, and the display name of the token whenever it changed, so a rename in the lobby reaches the game with the next player token. Sets the bot name's url, which must pass a hosted bot's check, otherwise 422, and returns the bot name. It keeps its rating. A rated game plays the URL that the name had when the game started. A listed name's new URL shows on the leaderboard within a minute. A name registered before names had URLs needs one before it can play in a rated lobby. Another account's bot name, or an unknown one, is 404.
         */
        put: operations["setIslandBotNameURL"];
        post?: never;
        /**
         * Delete one of your bot names
         * @description Requires the player token of a signed-in player, which names their account; a guest's token is 403 with problem type urn:island-battles:problem:sign-in-required. Every call stores the account the first time the game sees it, with its hand play, and the display name of the token whenever it changed, so a rename in the lobby reaches the game with the next player token. Deletes the bot name with its rating. The name is free at once, and registering it again makes a new competitor at 1500. Another account's bot name, or an unknown one, is 404.
         */
        delete: operations["deleteIslandBotName"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/account/bot-names/{competitor_id}/away-pause": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        /**
         * Let one of your bot names play away again after it missed a turn
         * @description Requires the player token of a signed-in player, which names their account; a guest's token is 403 with problem type urn:island-battles:problem:sign-in-required. Every call stores the account the first time the game sees it, with its hand play, and the display name of the token whenever it changed, so a rename in the lobby reaches the game with the next player token. A bot name plays away when it sits in a bot seat, or when a member of another account chose it, in a rated lobby that someone else opened. One that misses a turn there loses the game, as any rated game, and is paused: it plays away in no rated game until its owner resumes it here. Your own games with it are unaffected. Clears the bot name's away_paused and answers 200 with the bot name; resuming a name that isn't paused changes nothing. Another account's bot name, your hand play, or an unknown one is 404.
         */
        delete: operations["resumeIslandBotNameAway"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/bots/defaults": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List the default bots that the backend plays, and whether it offers hosted and uploaded bots.
         * @description Lists the default bots in level order, each with its level, a name and a description of how it plays. Higher levels play better. The backend plays a default bot itself, with no client: it plays each turn as soon as the generation is created, so it never misses a turn's deadline. A game in which only default bots are left plays on by itself, and each game request continues it for a few seconds. The list is static and needs no authentication. server_bots says whether the server also offers hosted and uploaded bots, which it plays without a player's tab, in member settings and bot seats.
         */
        get: operations["listDefaultBots"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/bot-compute": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Say whether games with uploaded bots may start this month.
         * @description Uploaded bots run in the server's bot runners, whose compute has a monthly budget, so that they stay inside Lambda's free tier. Once the runners have used it this month (UTC), a lobby whose start roster has an uploaded bot, a member's or a bot seat's, can't start until the first of the next month: the start fails with the lobby's game_requirement of type urn:island-battles:problem:bot-compute-spent, and the bot pickers grey out uploaded bots with the same sentence. Games already running go on, and games with only localhost, hosted and default bots are unaffected. The runners' compute is read from CloudWatch at most once per 10 minutes and kept in between, so this needs no authentication. A server that doesn't measure it, such as a local one, reports 0 and available. 503 when the compute can't be read.
         */
        get: operations["getBotCompute"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/from-lobby/{lobby_id}/deadline": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * End the current turn once its deadline has passed, for a roster player
         * @description Requires a place on the game's frozen roster; the lobby is not consulted. The server runs no timers, so a turn ends when a request finds its deadline, GenerationState.turn_deadline_at, passed. Every game request does that first, and a client that is waiting for the turn to end calls this once the deadline has passed. Before the deadline, and for a game that has ended or has no turn timeout, it changes nothing. In a real-time game (GenerationState.turn_pace_milliseconds isn't null, because a player plays by hand) the deadline is the turn pace after the generation was created, and this plays the turn with the commands that arrived: the batches of players who haven't completed it, complete false included, are played, a player who sent nothing passes, and nobody loses for passing. The next generation is pushed as after any turn. Calling it again changes nothing until the next turn's deadline. A real-time game can also end without a conquest. A team leaves when its player resigns (POST /game/from-lobby/{lobby_id}/resign), or when the server hasn't heard from its player for the lobby's turn_timeout_seconds, or for one turn pace plus 5 seconds when that is longer, by the end of a turn: the turn that ends then takes the team out. Every game request made as a player, with a player token or a submit token, also one that is refused, counts as hearing from them, and a default bot is always heard. A team that leaves has its castles turn neutral, keeping their soldiers, and its soldier groups removed, and GenerationState.departures lists it; with two teams the other team wins. When no request has reached a real-time game for turn_timeout_seconds after its open turn's end, the next game request, whoever makes it, or the lobby's reopen, ends it on the open generation with reason abandoned: among the teams still in it the team with the most castles wins, and equal counts are a draw. With turn_timeout_seconds null nobody leaves by silence and no game is abandoned. In a game that waits for every player, after the deadline every team with a player who hasn't completed the turn loses, the game ends with reason turn_timeout, and among the other teams still standing the team with the most castles wins; equal counts, or no team left, are a draw. The open generation stays the final one, game.ended is pushed to every roster player, and later commands conflict (409) with problem type urn:island-battles:problem:game-ended. Calling it again is safe and ends the game once. Returns your current state, as GET /game/from-lobby/{lobby_id}/generation does. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
         */
        post: operations["applyIslandLobbyTurnDeadline"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/from-lobby/{lobby_id}/demo": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Save a lobby's game as a demo file for a roster player
         * @description Requires a place on the game's frozen roster; the lobby is not consulted. Returns the game as a demo file, the Demo schema, as an attachment named island-battles-game-<game_id>-generation-<n>.json, where n is the number of generations played. Any roster player can save a demo at any time: one saved during the game holds the generations played so far and a null outcome. Each frame's game log lines follow the rule of GET /game/from-lobby/{lobby_id}/logs when the demo is saved: while the game runs only your own team's lines, once it has ended every team's. The demo viewer at /game/demo-viewer/ opens the file without the lobby, a session or the game. The body is streamed, and a long game with full logs takes tens of megabytes, so it is packed with gzip (Content-Encoding: gzip) when Accept-Encoding allows it. The game is deleted 7 days after its last generation by default; then this returns 404. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
         */
        get: operations["getIslandLobbyDemo"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/from-lobby/{lobby_id}/game-ended-delivery": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read the end of your game, or a localhost bot seat's that you relay, as a delivery for the bot, with a demo link
         * @description Requires a place on the game's frozen roster; the lobby is not consulted. Once the game has an outcome, returns it as a GameEndedDelivery for the caller's bot: the outcome, the final generation as the bot's team sees it, the memory the bot sent last, and demo.url, a self-contained link to GET /game/{id}/demo with a read-only demo token. The server sends the same delivery to every hosted and uploaded bot on the roster when the game ends; a game client reads this to relay it to a localhost bot, island-battles-bot serve, at POST /game-ended, which answers 2xx. With player_id, the player who added a localhost bot seat reads the seat's, as for GET /game/from-lobby/{lobby_id}/turn-delivery; any other player_id is 403. Like every game request, it first ends the turn once its deadline has passed. While the game runs it returns 409 with problem type urn:island-battles:problem:game-not-ended. Each call issues a new link that expires at the same time. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
         */
        get: operations["getIslandLobbyGameEndedDelivery"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/from-lobby/{lobby_id}/generation": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read the current generation for a roster player
         * @description Requires a place on the game's frozen roster. The game does not consult the lobby, so access continues after the lobby is deleted. Returns castle and soldier group IDs, which are never reassigned within a game, troop state, every castle's and soldier group's upgrade levels, recorded clashes with per-participant before/after counts, your turn completion flag, the turn's deadline, and the outcome once the game has ended. Like every game request, it first ends the turn once its deadline has passed: a real-time game plays it, and in a game that waits for every player the turn times out, which ends the game. A finished game stays readable until the game is deleted, 7 days after its last generation by default; then this returns 404. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
         */
        get: operations["getIslandLobbyGeneration"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/from-lobby/{lobby_id}/logs": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read a generation's game log lines for a roster player
         * @description Requires a place on the game's frozen roster; the lobby is not consulted. Returns the game log lines that players' bots sent with their batches for the generation, grouped by player, each named by the display name they had at the start, never by ID. While the game runs you see only your own team's lines; once it has an outcome you see every team's. hidden_team_colors lists the teams you can't see yet, without saying whether they logged anything. A generation of another game returns 404. The lines are deleted with the game, 7 days after its last generation by default; then this returns 404. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
         */
        get: operations["getIslandLobbyGenerationLogs"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/from-lobby/{lobby_id}/resign": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Resign your team from a real-time game
         * @description Requires a player token of a player on the game's frozen roster; the lobby is not consulted. Only a real-time game (GenerationState.turn_pace_milliseconds isn't null) can be resigned; in a game that waits for every player this is 409 with problem type urn:island-battles:problem:not-real-time and changes nothing. Like every game request, it first ends the turn once its deadline has passed. It then records that your team resigns, and returns your state, as GET /game/from-lobby/{lobby_id}/generation does. The resignation takes effect when the open turn plays, before that turn's commands: every castle of your team turns neutral, keeping its soldiers and losing its burst_debt, every soldier group of your team is removed without a clash, and your team's own commands of that turn are dropped. Then the game's end rule runs as after any turn: with two teams the other team wins in that turn, and with more the game goes on without you, and GenerationState.departures lists your team. The open turn no longer waits for your team, so when every other player has completed it, as default bots do at once, it plays in this request and the answer is the next generation's state, or the outcome; otherwise it plays at turn_deadline_at, at most one turn pace later. Calling it again before the turn plays changes nothing and returns your state. After the outcome it is 409 with problem type urn:island-battles:problem:game-ended, and once your team is out (it resigned, left the game, or was eliminated) 409 with problem type urn:island-battles:problem:team-out, as is every later batch of commands and turn delivery of a team that resigned or left. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
         */
        post: operations["resignIslandLobbyGame"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/from-lobby/{lobby_id}/turn-delivery": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read your turn, or a localhost bot seat's that you relay, as a delivery for the bot, with a one-turn submit token
         * @description Requires a place on the game's frozen roster; the lobby is not consulted. Returns the caller's turn of the current generation as a TurnDelivery: the BotTurn the server builds for their bot (the board, their team, the upgrade cost table, the memory their latest completed turn sent and the deadline), and how to submit the answer: submit.url, POST /generation/commands on the game's public URL, and submit.token, a submit token that submits exactly this player's turn of this generation. A game client relays it to the player's bot server, island-battles-bot serve, which answers 202 at once, plays the turn and submits its commands, logs and memory itself, with complete true and Authorization: Bearer <token>; the client submits nothing. Each call issues a new token for the same turn. With player_id, the player who added a localhost bot seat to the lobby reads the seat's turn instead, one of their GenerationState.relayed_bots, to relay it to the seat's bot on its own port; the delivery and its token are the seat's. A player_id that is not a localhost bot seat that the caller added is 403, for a later host too. Like every game request, it first ends the turn once its deadline has passed; in a real-time game it then delivers the turn that follows. When the player has nothing to play it returns 409: the game has ended (problem type urn:island-battles:problem:game-ended), their team resigned or left a real-time game (problem type urn:island-battles:problem:team-out), their team has been eliminated, or they have already completed the turn. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
         */
        get: operations["getIslandLobbyTurnDelivery"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/from-lobby/{lobby_id}/watch": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read the current generation of a lobby's game for a watcher
         * @description For the people who watch the lobby with its join code (POST /lobby/v1/watches): a watcher the host hasn't kicked, an active member of the lobby, or a roster player. Returns the WatchState: the board, the teams, the turn's deadline and the outcome once the game has ended, without the player-only fields of GenerationState. The lobby's watch channel, WatchState.channel, receives WatchGenerationEvent after each turn and WatchGameEndedEvent after the last one, in the same push as the players' events; read this after subscribing or reconnecting. A watcher never plays and never delays a turn. Like the other game reads, it first applies the turn's deadline once it has passed. A kicked watcher gets 403. Anyone else, a lobby whose game hasn't started, and a lobby that was deleted get 404. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
         */
        get: operations["watchIslandLobbyGeneration"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/from-lobby/{lobby_id}/watch/demo": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read a lobby game's history for a watcher
         * @description For the people who may read GET /game/from-lobby/{lobby_id}/watch. Returns the game so far as a demo file, the Demo schema, as GET /game/from-lobby/{lobby_id}/demo does, for scrubbing back through it. A watcher belongs to no team, so while the game runs no game log lines are included and every frame's hidden_team_colors names every team; once the game has ended every team's lines are. Packed with gzip when Accept-Encoding allows it. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
         */
        get: operations["watchIslandLobbyDemo"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/from-lobby/{lobby_id}/watch/logs": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read a generation's game log lines for a watcher
         * @description For the people who may read GET /game/from-lobby/{lobby_id}/watch. Returns a generation's game log lines in the shape of GET /game/from-lobby/{lobby_id}/logs, for a reader of no team: bots often log their plans, so while the game runs no lines are included and hidden_team_colors names every team, and once the game has an outcome every team's lines are. A roster player reads it as a watcher too. A generation of another game returns 404. A kicked watcher gets 403; anyone else, and a lobby whose game hasn't started, get 404. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
         */
        get: operations["watchIslandLobbyGenerationLogs"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/lobbies/{lobby_id}/bot-packages": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List a lobby's bot packages
         * @description Requires an active membership of the Island Battles lobby, in any phase, with your player token (PlayerToken); anyone else gets 404. Lists every package that a member uploaded and completed, newest first, with who uploaded it, its language, its entry or command and its size. A member has at most one. Packages are deleted 8 days after their upload, with their zips. A server without storage for packages answers 503.
         */
        get: operations["listIslandBotPackages"];
        put?: never;
        /**
         * Start uploading a bot package to a lobby
         * @description Requires an active membership of the Island Battles lobby, in any phase, with your player token (PlayerToken) or an upload token for this lobby (Authorization: Bearer ibut1.…); anyone else gets 404, and an upload token for another lobby, or an expired one, 401. A bot package is the zip that the SDKs' package command builds: island-battles-bot package in Python, or the Go SDK's island-battles-package. A package belongs to the lobby, and anyone in it may upload one. This stores a pending package and returns its package_id and a presigned POST: send a multipart/form-data POST to upload.url with every field of upload.fields, unchanged, and the zip last, in a field named file. The zip goes straight to storage, never through this server, so it may be up to 250 MiB (262,144,000 bytes); the presigned POST refuses an empty or larger file, and expires after 15 minutes. Then call POST /game/lobbies/{lobby_id}/bot-packages/{package_id}/complete. file_name must end in .zip, or this is 422. A server without storage for packages answers 503.
         */
        post: operations["startIslandBotPackageUpload"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/lobbies/{lobby_id}/bot-packages/upload-token": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Get an upload token for uploading a bot package from the command line
         * @description Requires an active membership of the Island Battles lobby, in any phase, with your player token (PlayerToken); anyone else gets 404. Returns an upload token for you and this lobby, valid for 15 minutes, for a command line that doesn't hold your session: island-battles-bot upload --game-url <url> --token=<token> dist/island-battles-bot.zip, or the Go SDK's island-battles-package upload with the same arguments. The two upload endpoints, POST /game/lobbies/{lobby_id}/bot-packages and its complete, accept it as Authorization: Bearer <token> in place of your player token; nothing else does. The command line reads the lobby from the token. A server without storage for packages answers 503.
         */
        post: operations["createIslandBotUploadToken"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/lobbies/{lobby_id}/bot-packages/{package_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        /**
         * Delete your bot package from a lobby
         * @description Requires an active membership of the Island Battles lobby, in any phase, with your player token (PlayerToken); anyone else gets 404. Deletes the package and its zip. Only the member who uploaded it can: anyone else gets 403. A package that the lobby doesn't have is 404. A server without storage for packages answers 503.
         */
        delete: operations["deleteIslandBotPackage"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/lobbies/{lobby_id}/bot-packages/{package_id}/complete": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Check an uploaded bot package and add it to the lobby
         * @description Requires an active membership of the Island Battles lobby, in any phase, with your player token (PlayerToken) or an upload token for this lobby (Authorization: Bearer ibut1.…); anyone else gets 404, and an upload token for another lobby, or an expired one, 401. Only the member who started the upload can complete it; for anyone else the package is 404. It reads the zip's central directory and island-battles-bot.json with range requests, without downloading the zip, and checks that: the manifest is format 1, either python with python "3.14" and a module:function entry, or native with a command whose program is a file in the zip; the files come to at most 1 GiB (1,073,741,824 bytes) unpacked; and no path leaves the package (.., absolute paths or symbolic links). A package that fails a check is 422 with a detail that says what is wrong, and is deleted with its zip: start another upload. Before the zip is uploaded this is 409, and nothing changes. A valid package is the member's package in the lobby from then on, and every member sees it in GET /game/lobbies/{lobby_id}/bot-packages. A member has one package per lobby: completing a new one deletes their previous one. Completing it again returns it. A server without storage for packages answers 503.
         */
        post: operations["completeIslandBotPackageUpload"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/upgrades": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List the castle upgrades, their levels and their costs.
         * @description Lists every upgrade type, sorted by type, with what its levels measure and, for each level from 1 to max_level, its effect and the soldiers a castle pays to reach it from the level below. Every castle starts at level 1. The table is static and needs no authentication. Castles and soldier groups carry their current levels, and upgrade_castle in POST /generation/commands buys the next one. generation_amount is what a castle produces each generation, and burst_potential the most a burst_castle command may order.
         */
        get: operations["listUpgrades"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/game/{id}/demo": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Save a game, by its ID, as a demo file, with a demo link or for a roster player
         * @description The demo of GET /game/from-lobby/{lobby_id}/demo, the same file, for a game named by its ID. A bot that runs outside the backend has no player token and doesn't know the lobby, so the end of its game, GameEndedDelivery, carries demo.url: this path with a read-only demo token in its token query parameter, which downloads the game's demo as the bot's player sees it, once the game has ended with every team's game log lines and the outcome, and nothing else. The demo token can also go in Authorization: Bearer ibdt1.…, and a roster player's player token works too. A demo token of another game, an expired or altered one, or one of a player no longer on the roster is 401. Packed with gzip when Accept-Encoding allows it. The game is deleted 7 days after its last generation by default, when the link expires; then this returns 404.
         */
        get: operations["getIslandGameDemo"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/generation/commands": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Register an atomic player command batch and optionally complete the turn
         * @description Requires a player token of a player on the game's frozen roster (PlayerToken), or a submit token: Authorization: Bearer ibst1.… from GET /game/from-lobby/{lobby_id}/turn-delivery stands for its player for the batch of its game and generation only, and anything else (another game or generation, an expired or altered token, a player no longer on the roster) is 401. Either way the lobby is not consulted, and the rules below are the same. Players send send_soldier_group, upgrade_castle and burst_castle commands for castles their team owns; other command types are forbidden (403). upgrade_castle buys the next level of one upgrade type and pays that level's soldier_cost, listed by GET /game/upgrades, from the castle's soldiers. A castle may be upgraded several times in one turn, and each upgrade buys the next level. Every generation, each castle a team owns produces its generation_amount value of soldiers. burst_castle orders a burst of 1 up to the castle's burst_potential value, counting the turn's upgrades (every upgrade of its batch, before or after it, and of earlier batches; a batch's bursts are checked after its other commands): the very next generation adds it to the castle on top of its production and makes it the castle's burst_debt. Each later generation's production repays the debt first, and only what is left over grows the castle. A castle with burst_debt can't burst, nor can a castle burst twice in one turn; a capture clears the debt. Every command is checked against the generation and every command already accepted this turn, including your own earlier batches, so an accepted command cannot fail when the turn runs. An invalid command rejects the whole batch, and nothing of it is stored. Malformed parameters are 422. Rejections that clients tell apart carry a problem type: a castle that is not in the generation is 422 urn:island-battles:problem:castle-not-found, an unknown upgrade_type is 422 urn:island-battles:problem:unknown-upgrade-type, a castle your team does not own is 403 urn:island-battles:problem:castle-not-owned, an upgrade past its type's max_level, counting this turn's upgrades, is 409 urn:island-battles:problem:upgrade-max-level, soldiers or an upgrade cost beyond what the castle has left after this turn's reservations is 409 urn:island-battles:problem:insufficient-soldiers, a burst of a castle with burst_debt or a burst accepted this turn is 409 urn:island-battles:problem:burst-unavailable, and a burst above the castle's burst potential is 409 urn:island-battles:problem:burst-above-potential. A castle may spend all its soldiers. Complete defaults to true; an empty batch can complete a turn. The last frozen roster completion advances exactly once. The game ends after the turn that leaves one team with castles or soldier groups, a draw when the last teams are eliminated in the same turn, or after the turn that creates the game's last generation, game_settings.max_generations (300 when the lobby sets none), where the team with the most castles wins. That turn's generation is the last, its state carries the outcome, and every later command conflicts (409) with problem type urn:island-battles:problem:game-ended. In a game that waits for every player, a turn also ends the game when its deadline, GenerationState.turn_deadline_at, passes before every roster player completed it: each team with a player who hadn't loses (outcome reason turn_timeout), the open generation stays the last, and a batch for it after the deadline conflicts with the same problem type. In a real-time game (GenerationState.turn_pace_milliseconds isn't null) a turn ends at its deadline, the turn pace after its generation was created: the request that finds it passed plays the turn with the commands that arrived, as if every player had completed it, and a batch for it that arrives later conflicts (409) as a stale generation, with a detail that names the pace. A real-time game can also end without a conquest. A team leaves when its player resigns (POST /game/from-lobby/{lobby_id}/resign), or when the server hasn't heard from its player for the lobby's turn_timeout_seconds, or for one turn pace plus 5 seconds when that is longer, by the end of a turn: the turn that ends then takes the team out. Every game request made as a player, with a player token or a submit token, also one that is refused, counts as hearing from them, and a default bot is always heard. A team that leaves has its castles turn neutral, keeping their soldiers, and its soldier groups removed, and GenerationState.departures lists it; with two teams the other team wins. When no request has reached a real-time game for turn_timeout_seconds after its open turn's end, the next game request, whoever makes it, or the lobby's reopen, ends it on the open generation with reason abandoned: among the teams still in it the team with the most castles wins, and equal counts are a draw. With turn_timeout_seconds null nobody leaves by silence and no game is abandoned. A batch of a player whose team resigned or left conflicts (409) with problem type urn:island-battles:problem:team-out. A new batch for a generation that is no longer current, in either kind of game, conflicts (409) with problem type urn:island-battles:problem:stale-generation. Retry the same request UUID and payload safely, also after the game ended; changed payloads and stale new requests conflict. A batch may also carry game log lines, which are stored in order with the generation and read with GET /game/from-lobby/{lobby_id}/logs. A player has at most 100 lines per generation, each of at most 500 characters; a batch over either limit is rejected whole (422) with problem type urn:island-battles:problem:game-log-limit. A batch may also carry the memory of the player's bot, any JSON object of at most 64 KiB as compact JSON. The batch with complete true saves it, replacing the player's previous memory, and an absent or null memory clears it; other batches ignore it. GenerationState.memory returns the memory sent with the player's latest completed turn, and the retry hash covers it. Larger memory is rejected whole (422) with problem type urn:island-battles:problem:bot-memory-too-large.
         */
        post: operations["registerGenerationCommand"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/leaderboard": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read the leaderboard: the competitors ranked by rating
         * @description Public: it needs no credential. It lists the competitors that have played at least one rated game: each registered bot name, shown with its owner's display name ("Kraken, by Dana"), each account's hand play, shown under the account's display name, and the house bots, one per default bot level, shown as Bot (level N). A deleted bot name and an account that the admin hid aren't listed, and the game holds no Google name, picture or email to show. Ratings are rounded to whole numbers. The answer carries Cache-Control: public, max-age=60, and CloudFront keeps it for a minute, so a change, such as a game that ends, shows within a minute. The order is rating, highest first, then more rated games first, then the older competitor. rank is the place in that order within the kind asked for, from 1, and equal ratings, as shown, get the same rank. kind lists only one kind, ranked among itself; limit (1 to 100, default 50) and offset (default 0) page through it, and total counts the competitors of that kind.
         */
        get: operations["listIslandLeaderboard"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/leaderboard/{competitor_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read a competitor's page: its place on the leaderboard and its latest results
         * @description Public: it needs no credential. It lists the competitors that have played at least one rated game: each registered bot name, shown with its owner's display name ("Kraken, by Dana"), each account's hand play, shown under the account's display name, and the house bots, one per default bot level, shown as Bot (level N). A deleted bot name and an account that the admin hid aren't listed, and the game holds no Google name, picture or email to show. Ratings are rounded to whole numbers. The answer carries Cache-Control: public, max-age=60, and CloudFront keeps it for a minute, so a change, such as a game that ends, shows within a minute. Returns the competitor's leaderboard entry, ranked among every kind, and history, its latest 50 rated results, newest first, each with its result, its rating before and after, and the game's other competitors by the name they are shown by now. An opponent that was deleted since, or whose account is hidden, is shown as Deleted bot or Deleted player, without its ID or owner. A competitor that the leaderboard doesn't list is 404: unknown, deleted, hidden, or without a rated game yet.
         */
        get: operations["getIslandCompetitor"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/connection-token": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Create a connection token
         * @description Creates an authenticated Centrifugo connection token; it never creates or joins a lobby.
         */
        post: operations["createLobbyConnectionToken"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/map/templates": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List the default maps a lobby can start on.
         * @description Lists every default map whose castle team colours are exactly 1 to player_amount, ordered by name. Choose one when creating an Island Battles lobby by passing its map_template_id in game_settings, with exactly team_count teams. Maps that fit no lobby are left out: those whose team colours skip a colour, and those made for fewer than 2 or more than 16 teams.
         */
        get: operations["listMapTemplates"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        AwayPause: {
            /**
             * Format: date-time
             * @description When it missed the turn.
             */
            at: string;
            /** @description Which turn it missed, such as "Missed turn 12 of a rated game on 21 November". */
            reason: string;
        };
        BotChoice: {
            /** @description For kind hosted, the host name of the bot's URL, such as bot.example.com; the rest of the URL is not shown. Absent for the other kinds. */
            host?: string;
            /**
             * @description localhost: the player's own bot, which island-battles-bot serve runs on their machine; their game client relays each turn to it. default: a default bot of GET /bots/defaults, which the backend plays itself, so the player's client submits nothing. hosted: the player's own bot server at a public URL, which the server sends each turn to; no client relays it. uploaded: a bot package uploaded to the lobby, which the server runs; no client relays it. manual: a person who plays by hand in the 3D client; nothing plays or relays the player, and their client sends their commands.
             * @enum {string}
             */
            kind: "localhost" | "default" | "hosted" | "uploaded" | "manual";
            /**
             * Format: int64
             * @description For kind default, the level of the default bot, one that GET /bots/defaults lists. Absent for the other kinds.
             */
            level?: number;
            /** @description In a rated game, the bot name that a hosted bot plays as, such as Kraken: one that its player registered in their account, or a bot from the leaderboard that plays away, for a member of another account or in a seat. Absent for the other kinds and in a game that isn't rated. */
            name?: string;
            /**
             * Format: int64
             * @description For kind localhost, the port on 127.0.0.1 where the player's client delivers each turn: the one they chose in the lobby, 41414 by default. Absent for the other kinds.
             */
            port?: number;
        };
        BotCompute: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/BotCompute.json
             */
            readonly $schema?: string;
            /** @description Whether a game with an uploaded bot, a member's or a bot seat's, may start: used_gb_seconds is under budget_gb_seconds. When false, the start is refused until the next month, and the bot pickers grey out uploaded bots. Games already running go on. */
            available: boolean;
            /**
             * Format: double
             * @description The runners' monthly budget in GB-seconds (ISLAND_BATTLES_BOT_COMPUTE_BUDGET_GB_SECONDS).
             */
            budget_gb_seconds: number;
            /**
             * @description The month, in UTC, as YYYY-MM. The budget starts again on the first of the next month.
             * @example 2026-11
             */
            month: string;
            /**
             * Format: double
             * @description The GB-seconds that the bot runners have used this month, from CloudWatch, read at most once per 10 minutes. 0 on a server that doesn't measure them, such as a local one.
             */
            used_gb_seconds: number;
        };
        BotPackage: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/BotPackage.json
             */
            readonly $schema?: string;
            /** @description A python package's module:function entry, such as bot:play, or a native package's command, its arguments joined with spaces, such as ./bot worker. */
            entry_or_command: string;
            /**
             * @description python: a Python bot for the python3.14 runtime. native: a program for Linux arm64, such as a Go bot.
             * @enum {string}
             */
            language: "python" | "native";
            /** @description Their display name when they uploaded it. */
            owner_name: string;
            /**
             * Format: uuid
             * @description The member who uploaded it. Only they can delete it.
             */
            owner_player_id: string;
            /** Format: uuid */
            package_id: string;
            /**
             * Format: int64
             * @description The zip's size in bytes.
             */
            size_bytes: number;
            /** Format: date-time */
            uploaded_at: string;
        };
        BotPackageUpload: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/BotPackageUpload.json
             */
            readonly $schema?: string;
            /** Format: uuid */
            package_id: string;
            upload: components["schemas"]["PresignedBotUpload"];
        };
        BotPackageUploadRequest: {
            /** @description The zip's file name, such as island-battles-bot.zip. It must end in .zip. */
            file_name: string;
        };
        BotPackages: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/BotPackages.json
             */
            readonly $schema?: string;
            /** @description Every checked package of the lobby, newest first, at most one per member. */
            packages: components["schemas"]["BotPackage"][];
        };
        /** @description Island Battles seat_settings for a bot seat that the host adds with POST …/seats: exactly one of bot_level, bot_localhost_port, bot_url and bot_package_id. {"bot_level": N}, with a level that GET /bots/defaults lists, is a default bot that the backend plays, named "Bot (level N)". {"bot_localhost_port": N, "bot_relay_player_id": "<your player ID>"}, a port from 1024 to 65535, is a localhost bot on your machine as the host, which island-battles-bot serve --port N runs and only your game client relays, named "Localhost bot (port N)": you can seat several, each on its own port. bot_relay_player_id records who added the seat, since the lobby doesn't, and must be the host's own player ID. A later host never relays it: a start with a localhost bot seat whose adder left the lobby fails with game_requirement of type urn:island-battles:problem:localhost-seat-relay-gone, until the host removes the seat, and can add it again from their own page. A port that your own localhost bot or another localhost bot seat of yours already uses is invalid_seat_settings, unless the seat is already there with these settings, so a seat request sent again after a lost answer finds its own seat. A start where someone would relay two localhost bots on one port, after they changed their own port or added the same seat twice, fails with game_requirement of type urn:island-battles:problem:localhost-port-taken. When GET /bots/defaults says server_bots, also {"bot_url": "https://…"}, a hosted bot under the rules of MemberBot's url, named "Hosted bot", or {"bot_package_id": "…"}, any ready bot package of the lobby, named after its owner, such as "Alex's bot"; the server plays both. A seat whose package is gone by the start stops it with game_requirement of type urn:island-battles:problem:bot-package-gone. */
        BotSeatSettings: {
            /**
             * Format: uuid
             * @description A bot name that the leaderboard lists, by its competitor_id (GET /leaderboard), whoever owns it, which plays at the name's URL as it is at the start. The seat is named after the bot and its owner, such as "Kraken (Dana)". It plays away: in a rated lobby the bot is the seat's competitor, and the daily caps and the pause apply to it; in a lobby that isn't rated it rates nothing. Only when GET /bots/defaults says server_bots.
             */
            bot_competitor_id?: string;
            /**
             * Format: int64
             * @description A default bot of this level, one that GET /bots/defaults lists, which the backend plays. The seat is named "Bot (level N)".
             */
            bot_level?: number;
            /** @description A localhost bot on the machine of the host who adds the seat, which island-battles-bot serve --port N runs on this port of 127.0.0.1; that host's game client delivers each turn to it, as to their own localhost bot. A string of the port's digits, which the lobby page's text field sends, is read the same. The port must differ from that host's own localhost bot's and from every other localhost bot seat's that they added. Needs bot_relay_player_id. The seat is named "Localhost bot (port N)". */
            bot_localhost_port?: number | string;
            /**
             * Format: uuid
             * @description Any member's bot package in this lobby, which the server runs. The seat is named after its owner, such as "Alex's bot". Only when GET /bots/defaults says server_bots.
             */
            bot_package_id?: string;
            /**
             * Format: uuid
             * @description With bot_localhost_port only, and required with it: the player ID of the host who adds the seat, whose game client alone relays it. It must be the lobby's host. When that player is no longer in the lobby, the start is refused until the seat is removed.
             */
            bot_relay_player_id?: string;
            /** @description A hosted bot at this URL, under the rules of a member's hosted bot; the server sends it each turn. The seat is named "Hosted bot". Only when GET /bots/defaults says server_bots. */
            bot_url?: string;
        };
        /** @description What a bot is given to play one turn. A bot is a stateless function: whatever it needs to keep between turns goes in the memory it returns, which the server stores per player and game. */
        BotTurn: {
            /** Format: int64 */
            game_id: number;
            generation: components["schemas"]["Generation"];
            /**
             * Format: int64
             * @description The game's last generation, the same for the whole game. The turn that creates generation max_generations ends the game, if nothing has ended it before, so turn max_generations - 1 is the last one a bot plays: max_generations - turn_number turns are left, this one included.
             */
            max_generations: number;
            /** @description The memory the bot returned with its previous turn in this game, or null on its first turn and after a turn that returned null. */
            memory: {
                [key: string]: unknown;
            } | null;
            /** Format: int64 */
            team_color: number;
            /**
             * Format: date-time
             * @description When the turn ends, GenerationState.turn_deadline_at, or null when turns have no time limit. In a game that waits for every player, a bot that hasn't answered by then makes its whole team lose. In a real-time game, where a player plays by hand, the server plays the turn then without the bot's answer: the bot passes, and a later answer is refused as a stale generation.
             */
            turn_deadline_at: string | null;
            /**
             * Format: int64
             * @description This turn's number in the game, from 1: the game's first generation is turn 1, and every turn adds one, the n of the replay's Turn n / m. generation.id is not the turn number: it is the server's ID of the generation, which increases with every turn but doesn't start at 1.
             */
            turn_number: number;
            /** @description The upgrade cost table, the same list GET /game/upgrades returns as its upgrades. It is the same for the whole game. Reaching a level costs its soldier_cost. */
            upgrades: components["schemas"]["Upgrade"][];
        };
        /** @description A bot's answer to a BotTurn. Its commands, logs and memory are sent as one completing batch of POST /generation/commands. */
        BotTurnResult: {
            commands: components["schemas"]["PublicGenerationCommand"][];
            /** @description Game log lines for this turn, of at most 500 characters each. */
            logs: components["schemas"]["GenerationLogLine"][];
            /** @description Any JSON object of at most 64 KiB as compact JSON, given back as BotTurn.memory next turn, or null for none. */
            memory: {
                [key: string]: unknown;
            } | null;
        };
        BotUploadToken: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/BotUploadToken.json
             */
            readonly $schema?: string;
            /** Format: date-time */
            expires_at: string;
            /** @description An upload token, ibut1.…: send it as Authorization: Bearer <token> to the two upload endpoints of this lobby, in place of a session. */
            token: string;
        };
        BurstCastleCommand: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            command_type: "burst_castle";
            parameters: components["schemas"]["BurstCastleParameters"];
        };
        BurstCastleParameters: {
            /**
             * Format: int64
             * @description Castle your team owns, with no burst_debt and no other burst this turn.
             */
            castle_id: number;
            /**
             * Format: int64
             * @description Soldiers the burst adds, at most the castle's burst_potential value from GET /game/upgrades, counting the turn's upgrades: every upgrade of its batch, before or after it, and of the turn's earlier batches. The castle gets them in the next generation, on top of its production, and repays them from its later production.
             */
            soldier_amount: number;
        };
        Castle: {
            /**
             * Format: int64
             * @description Soldiers the castle still owes for its last burst. A burst arrives in the generation right after the turn that orders it, on top of that generation's production, and becomes the debt. Each later generation's production repays the debt first, and only what is left over grows the castle. A castle with debt can't burst. A capture clears it.
             */
            burst_debt: number;
            /**
             * Format: int64
             * @description Castle ID, stable for the whole game.
             */
            id: number;
            /** Format: int64 */
            soldiers: number;
            /**
             * Format: int64
             * @description Owning team; 0 is neutral.
             */
            team_color: number;
            /** @description Every upgrade type's level. GET /game/upgrades lists the levels and what the next one costs. */
            upgrades: {
                [key: string]: number;
            };
            /** Format: double */
            x: number;
            /** Format: double */
            y: number;
        };
        CastleTemplate: {
            /**
             * Format: int64
             * @description Soldiers in the castle when a game starts. Defaults to neutral_starting_soldier_amount.
             */
            starting_soldier_amount?: number;
            /**
             * Format: int64
             * @description Owning team colour, from 1 to player_amount; 0 is neutral.
             */
            team_color: number;
            /** @description Starting level of each upgrade type. Omitted types start at level 1. */
            upgrades?: {
                [key: string]: number;
            };
            /**
             * Format: double
             * @description Distance from the centre of the plane, between -plane_size_x/2 and plane_size_x/2.
             */
            x: number;
            /**
             * Format: double
             * @description Distance from the centre of the plane, between -plane_size_y/2 and plane_size_y/2.
             */
            y: number;
        };
        Clash: {
            /**
             * Format: int64
             * @description Identifies the clash within its generation.
             */
            id: number;
            participants: components["schemas"]["ClashParticipant"][];
            /**
             * Format: double
             * @description Fraction of the turn at which contact happened.
             */
            time: number;
        };
        ClashParticipant: {
            /** Format: double */
            clash_radius: number;
            /**
             * Format: int64
             * @description Soldier group ID; the group may no longer exist.
             */
            group_id: number;
            /**
             * Format: int64
             * @description Soldiers immediately after this contact; 0 means the group was defeated.
             */
            soldiers_after: number;
            /**
             * Format: int64
             * @description Soldiers immediately before this contact.
             */
            soldiers_before: number;
            /** Format: int64 */
            team_color: number;
            /** Format: double */
            x: number;
            /** Format: double */
            y: number;
        };
        Competitor: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/Competitor.json
             */
            readonly $schema?: string;
            /** @description For a bot name that missed a turn while it played away, in a rated game of someone else's lobby: it plays away in no rated game until its owner resumes it with DELETE /account/bot-names/{competitor_id}/away-pause. Its owner's own games with it are unaffected. Absent otherwise. */
            away_paused?: components["schemas"]["AwayPause"];
            /** Format: int64 */
            draws: number;
            /**
             * Format: int64
             * @description Rated games played.
             */
            games: number;
            /** Format: uuid */
            id: string;
            /**
             * @description bot: a bot name that an account registered. manual: an account's hand play, one per account. house: a default bot level.
             * @enum {string}
             */
            kind: "bot" | "manual" | "house";
            /**
             * Format: date-time
             * @description When its latest rated game ended, or null before the first.
             */
            last_played_at: string | null;
            /** Format: int64 */
            losses: number;
            /** @description The name it is shown by: a bot's registered name, the account's display name for its hand play, or Bot (level N) for a house bot. */
            name: string;
            /**
             * Format: double
             * @description The Elo rating, 1500 before the first rated game. Shown rounded.
             */
            rating: number;
            /** @description For a bot name, its hosted bot's URL, where it plays in a rated lobby. It becomes public on the leaderboard once the name has played a rated game. Null for hand play, and for a bot name registered before names had URLs, which plays in no rated lobby until its owner sets one with PUT /account/bot-names/{competitor_id}. */
            url: string | null;
            /** Format: int64 */
            wins: number;
        };
        CompetitorResult: {
            /** @description Whether the competitor played away: a bot name in a seat of someone else's lobby, or chosen by a member of another account. Its owner wasn't there. */
            away: boolean;
            /**
             * Format: date-time
             * @description When the game ended.
             */
            ended_at: string;
            /** @description The game's other teams' competitors, in colour order. */
            opponents: components["schemas"]["CompetitorResultOpponent"][];
            /**
             * Format: int64
             * @description The competitor's rating after the game, rounded.
             */
            rating_after: number;
            /**
             * Format: int64
             * @description The competitor's rating before the game, rounded.
             */
            rating_before: number;
            /** @enum {string} */
            result: "win" | "draw" | "loss";
        };
        CompetitorResultOpponent: {
            /**
             * Format: uuid
             * @description Null for a competitor that was deleted since, or whose account is hidden.
             */
            competitor_id: string | null;
            /** @description The name it is shown by now, as on the leaderboard; Deleted bot or Deleted player for a competitor that was deleted since, or whose account is hidden. */
            name: string;
            /** @description The display name of the account that a bot name belongs to; null otherwise, and for a deleted or hidden competitor. */
            owner: string | null;
            /**
             * Format: int64
             * @description Its rating before the game, rounded.
             */
            rating_before: number;
        };
        ConnectionToken: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/ConnectionToken.json
             */
            readonly $schema?: string;
            /** Format: date-time */
            expires_at: string;
            token: string;
            /** Format: uri */
            websocket_url: string;
        };
        ConnectionTokenInputBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/ConnectionTokenInputBody.json
             */
            readonly $schema?: string;
        };
        DefaultBot: {
            /** @description How the bot plays, in a sentence or two. */
            description: string;
            /**
             * Format: int64
             * @description The bot's level. Higher levels play better.
             */
            level: number;
            /** @description A short name to show in a bot picker. */
            name: string;
        };
        Demo: {
            /**
             * @description Marks the file as an Island Battles demo.
             * @enum {string}
             */
            format: "island-battles-demo";
            /** @description Every generation played when the demo was saved, in order. Each is the board view that players received. */
            frames: components["schemas"]["Generation"][];
            /** Format: int64 */
            game_id: number;
            /** @description One entry per frame, in the same order, with the game log lines its saver could see when saving: while the game runs only the saver's team's, once it has ended every team's. hidden_team_colors names the teams left out. */
            logs: components["schemas"]["GenerationLogs"][];
            /** @description How the game ended, or null when the demo was saved while the game was still running. */
            outcome: components["schemas"]["GameOutcome"] | null;
            /**
             * Format: date-time
             * @description When the demo was saved.
             */
            saved_at: string;
            /**
             * Format: int64
             * @description The demo format's version. The demo viewer opens only the current version and rejects a demo saved by another version of the game.
             * @enum {integer}
             */
            schema_version: 2;
            /** @description The teams that played, with their lobby names and the display names their players had at the start. */
            teams: components["schemas"]["GameTeam"][];
        };
        DemoLink: {
            /**
             * Format: date-time
             * @description When the link stops working: when game retention deletes the game, 7 days after its final generation by default. Download the demo before then.
             */
            expires_at: string;
            /**
             * Format: uri
             * @description GET /game/{id}/demo on the game's public URL, with a read-only demo token in its token query parameter, so a plain GET downloads the Demo file. The token lets its bearer download this game's demo as this player sees it, and nothing else; keep the link private.
             */
            url: string;
        };
        Departure: {
            /**
             * @description resigned: its player resigned. left: the server didn't hear from its player for the game's turn limit (or one turn plus 5 seconds, when that is longer) by the time a turn ended.
             * @enum {string}
             */
            kind: "resigned" | "left";
            /** @description The display name the player who resigned or went silent had at the start. */
            players: string[];
            /** Format: int64 */
            team_color: number;
            /**
             * Format: int64
             * @description The turn number of the first generation in which the team is out: its castles turned neutral, keeping their soldiers, and its soldier groups were removed. Null only in an abandoned game's outcome, for a resignation that was still waiting for its turn to play.
             */
            turn_number: number | null;
        };
        ErrorDetail: {
            /** @description Where the error occurred, e.g. 'body.items[3].tags' or 'path.thing-id' */
            location?: string;
            /** @description Error message text */
            message?: string;
            /** @description The value at the given location */
            value?: unknown;
        };
        ErrorModel: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/ErrorModel.json
             */
            readonly $schema?: string;
            /**
             * @description A human-readable explanation specific to this occurrence of the problem.
             * @example Property foo is required but is missing.
             */
            detail?: string;
            /** @description Optional list of individual error details */
            errors?: components["schemas"]["ErrorDetail"][] | null;
            /**
             * Format: uri
             * @description A URI reference that identifies the specific occurrence of the problem.
             * @example https://example.com/error-log/abc123
             */
            instance?: string;
            /**
             * Format: int64
             * @description HTTP status code
             * @example 400
             */
            status?: number;
            /**
             * @description A short, human-readable summary of the problem type. This value should not change between occurrences of the error.
             * @example Bad Request
             */
            title?: string;
            /**
             * Format: uri
             * @description A URI reference to human-readable documentation for the error.
             * @default about:blank
             * @example https://example.com/errors/example
             */
            type: string;
        };
        /** @description The end of a game, delivered to a bot that runs outside the backend, once the game has an outcome, however it ended and whatever became of the bot's team: the outcome, the final generation as the bot's team sees it, its last memory, and demo.url, a self-contained link that downloads the game's demo. It carries no submit token. The server POSTs it to a hosted bot's URL with the last path segment replaced by game-ended (https://bot.example.com/turn gives https://bot.example.com/game-ended), and offers it to an uploaded bot's worker; a game client relays it to its localhost bot at POST /game-ended, from GET /game/from-lobby/{lobby_id}/game-ended-delivery. The server doesn't wait for the answer or retry it: a 2xx is expected, and anything else changes nothing. */
        GameEndedDelivery: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/GameEndedDelivery.json
             */
            readonly $schema?: string;
            /** @description Where to download the game's demo. */
            demo: components["schemas"]["DemoLink"];
            /** Format: int64 */
            game_id: number;
            /** @description The final generation, the board the outcome was decided on, as a BotTurn's generation shows it to the bot's team. */
            generation: components["schemas"]["Generation"];
            /** @description The memory the bot sent with its latest completed turn, as the next BotTurn.memory would have been, or null. */
            memory: {
                [key: string]: unknown;
            } | null;
            /** @description How the game ended, as GenerationState.outcome says it. */
            outcome: components["schemas"]["GameOutcome"];
            /**
             * Format: int64
             * @description The bot's team, also when it was eliminated or left before the end.
             */
            team_color: number;
            /**
             * Format: int64
             * @description The final generation's turn number, as BotTurn.turn_number counts it.
             */
            turn_number: number;
        };
        GameEndedEvent: {
            /**
             * Format: int64
             * @enum {integer}
             */
            schema_version: 1;
            /** @description The final generation, with its outcome. */
            state: components["schemas"]["GenerationState"];
            /** @enum {string} */
            type: "game.ended";
        };
        GameOutcome: {
            /** @description The teams that resigned or left a real-time game, in the order they left. Always present, and empty for most games; a demo saved before this field existed has none, which reads as empty. */
            departures?: components["schemas"]["Departure"][];
            /** @description How the game changed each team's competitor's Elo rating, one per team in colour order, for a rated game only: absent when the game isn't rated. The winner gains and every other team loses, each against the winner alone. Without a winner, the teams still in the game when it ended draw each other, and each team that was eliminated, timed out, resigned or left loses to each of them; when no team was left, every pair of teams counts as a draw. */
            ratings?: components["schemas"]["RatingChange"][];
            /**
             * @description last_team_standing: one team was left after every other team lost all its castles and soldier groups, or resigned or left the game. all_eliminated: the last teams were eliminated, or left, in the same turn, a draw. generation_cap: the game reached its last generation; the team with the most castles won, and equal counts are a draw. turn_timeout: in a game that waits for every player, a turn passed its deadline; every team with a player who hadn't completed it lost, and among the other teams still standing the team with the most castles won, while equal counts, or no team left, are a draw. abandoned: nobody played a real-time game for its turn limit, so it ended on its open generation; among the teams still in it, except one whose resignation was waiting for the turn, the team with the most castles won, and equal counts, or no team left, are a draw.
             * @enum {string}
             */
            reason: "last_team_standing" | "all_eliminated" | "generation_cap" | "turn_timeout" | "abandoned";
            /** @description The outcome in one sentence, naming the winning team, the teams that resigned or left a real-time game, and for turn_timeout the teams that lost and their players who didn't answer. */
            summary: string;
            /**
             * Format: int64
             * @description The winning team, or null for a draw.
             */
            winner_team_color: number | null;
        };
        GameTeam: {
            /**
             * Format: int64
             * @description The team colour that castles and soldier groups carry as team_color.
             */
            color: number;
            name: string;
            /** @description The display names the team's players had at the start. */
            players: string[];
        };
        Generation: {
            castles: components["schemas"]["Castle"][];
            clashes: components["schemas"]["Clash"][];
            /**
             * Format: int64
             * @description The server's ID of the generation. It increases with every turn but is not the turn number, which BotTurn.turn_number and GenerationState.turn_number give: it doesn't start at 1.
             */
            id: number;
            soldier_groups: components["schemas"]["SoldierGroup"][];
        };
        GenerationEvent: {
            /**
             * Format: int64
             * @enum {integer}
             */
            schema_version: 1;
            state: components["schemas"]["GenerationState"];
            /** @enum {string} */
            type: "game.next_generation";
        };
        GenerationLogLine: {
            /** @enum {string} */
            level: "debug" | "info" | "warning" | "error";
            /** @description The name of the logger that wrote the line, such as island_battles.player. */
            logger: string;
            /** @description The line, of 1 to 500 Unicode code points. A client cuts a longer line to fit and says so at its end. */
            message: string;
        };
        GenerationLogs: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/GenerationLogs.json
             */
            readonly $schema?: string;
            /** Format: int64 */
            generation_id: number;
            /** @description Teams whose logs you can't see yet. While the game runs you see only your own team's logs; once it has an outcome you see every team's. */
            hidden_team_colors: number[];
            /** @description Every player of the teams you can see, ordered by team colour and then display name, with their lines in the order they were written. */
            players: components["schemas"]["PlayerGenerationLogs"][];
        };
        GenerationState: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/GenerationState.json
             */
            readonly $schema?: string;
            /** @description Your bot in this game, the one you chose in the lobby, copied when the game started. For kind localhost your client delivers each turn to your bot, on bot.port, which submits its answer. For kind default the backend plays your turns itself as soon as each generation is created. For kinds hosted and uploaded the server plays your turns with your bot, without your client. For kind manual you play by hand in the 3D client, which sends your commands; nothing plays or relays you. Only for kind localhost, and for relayed_bots, does your client relay anything. */
            bot: components["schemas"]["BotChoice"];
            /** @description Your personal Centrifugo channel, which receives GenerationEvent after each turn and GameEndedEvent after the last one. */
            channel: string;
            /** @description The teams that have resigned or left this real-time game so far, in the order they left, from the turn that took them out. Empty in every other game. A resignation shows here once the turn that applies it has played; before that, the answer of POST /game/from-lobby/{lobby_id}/resign is its only sign. */
            departures: components["schemas"]["Departure"][];
            /** Format: int64 */
            game_id: number;
            generation: components["schemas"]["Generation"];
            /** Format: int64 */
            generation_id: number;
            /** Format: uuid */
            lobby_id: string;
            /**
             * Format: int64
             * @description The lobby round that the game plays. A lobby plays one game per round, and the /game/from-lobby/{lobby_id} routes serve its newest game: compare this with the lobby snapshot's lobby.round to tell whether the game is the one the lobby started last.
             */
            lobby_round: number;
            /**
             * Format: int64
             * @description How many game log lines you have stored for this generation, out of the 100 a player may store per generation. 0 when there are none. A client that restarts during a turn continues counting from it.
             */
            log_lines_stored: number;
            /**
             * Format: int64
             * @description The game's last generation, the same for the whole game. The turn that creates generation max_generations ends the game, if nothing has ended it before, so turn max_generations - 1 is the last one players play. BotTurn.max_generations is the same number.
             */
            max_generations: number;
            /** @description The memory sent with your latest completed turn of this game, for your bot's next BotTurn.memory. Null before your first completed turn, and after a completed turn without memory. */
            memory: {
                [key: string]: unknown;
            } | null;
            /** @description How the game ended. Absent while the game runs. A finished game keeps its final generation and rejects every command. */
            outcome?: components["schemas"]["GameOutcome"];
            /** Format: uuid */
            player_id: string;
            /** @description Whether the game is rated: its lobby's rated setting at the start. Every team of a rated game is played by a competitor, a signed-in player's hand play or bot name, a bot from the leaderboard or a house bot, and bot.name names the bot name that plays. */
            rated: boolean;
            /** @description The localhost bot seats that you added to the lobby as its host, which only you relay: your client reads each one's turn with GET /game/from-lobby/{lobby_id}/turn-delivery?player_id= and delivers it to 127.0.0.1 on the seat's port, as it does for your own localhost bot. Empty for everyone else. */
            relayed_bots: components["schemas"]["RelayedBot"][];
            /** @description Whether you completed this generation's turn. */
            submitted: boolean;
            /** Format: int64 */
            team_color: number;
            /**
             * Format: date-time
             * @description When this generation's turn ends. In a real-time game (turn_pace_milliseconds isn't null) it is the generation's creation time plus the turn pace: the server then plays the turn with the commands that arrived, and a player who sent nothing passes. Otherwise it is the creation time plus the lobby's turn_timeout_seconds, when the turn times out: if a player of a team hasn't completed it by then, the team loses and the game ends. Null when turns have no time limit, and once the game has ended. A client waiting for the turn to end calls POST /game/from-lobby/{lobby_id}/deadline once it has passed, because the server runs no timers.
             */
            turn_deadline_at: string | null;
            /**
             * Format: int64
             * @description This generation's turn number in the game, from 1: the game's first generation is turn 1, and every turn adds one, the n of the replay's Turn n / m. generation_id is not the turn number: it is the server's ID of the generation, which increases with every turn but doesn't start at 1. BotTurn.turn_number is the same number.
             */
            turn_number: number;
            /**
             * Format: int64
             * @description The game's turn pace in milliseconds when the game is real-time, because a player on its roster plays by hand: each turn ends this long after its generation was created, at turn_deadline_at, and plays with the commands that arrived, without waiting for anyone. It still plays early once every player completed it. Null when the game waits for every player to complete each turn.
             */
            turn_pace_milliseconds: number | null;
        };
        IslandAccount: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/IslandAccount.json
             */
            readonly $schema?: string;
            /**
             * Format: uuid
             * @description The lobby's account ID.
             */
            account_id: string;
            /** @description The account's bot names, oldest first. */
            bot_names: components["schemas"]["Competitor"][];
            /** @description The account's display name, from the player token. Renaming the account in the lobby reaches the game with the next player token. */
            display_name: string;
            /** @description The account's hand play, a manual competitor shown under the account's display name. */
            hand: components["schemas"]["Competitor"];
            /**
             * Format: int64
             * @description How many bot names an account may register: 5.
             */
            max_bot_names: number;
        };
        Leaderboard: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/Leaderboard.json
             */
            readonly $schema?: string;
            /** @description The page's competitors, in the leaderboard's order: rating, highest first, then more rated games first, then the older competitor. */
            entries: components["schemas"]["LeaderboardEntry"][];
            /**
             * Format: int64
             * @description How many competitors the leaderboard lists, of the kind asked for.
             */
            total: number;
        };
        LeaderboardCompetitor: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/LeaderboardCompetitor.json
             */
            readonly $schema?: string;
            /** Format: uuid */
            competitor_id: string;
            /** Format: int64 */
            draws: number;
            /**
             * Format: int64
             * @description Rated games played: a competitor is listed after its first.
             */
            games: number;
            /** @description The competitor's latest 50 rated results, newest first. */
            history: components["schemas"]["CompetitorResult"][];
            /**
             * @description bot: a bot name that an account registered. manual: an account's hand play. house: a default bot level, which the backend plays.
             * @enum {string}
             */
            kind: "bot" | "manual" | "house";
            /**
             * Format: date-time
             * @description When its latest rated game ended.
             */
            last_played_at: string;
            /** Format: int64 */
            losses: number;
            /** @description The name it is shown by: a bot's name, such as Kraken, the account's display name for its hand play, or Bot (level N) for a house bot. */
            name: string;
            /** @description The display name of the account that a bot name belongs to, as in "Kraken, by Dana"; null for hand play and house bots. */
            owner: string | null;
            /**
             * Format: int64
             * @description The place in the leaderboard's order, from 1: equal ratings, as shown, get the same rank, and the next rank skips as many places (1, 2, 2, 4). GET /leaderboard ranks within the kind it lists; a competitor's page among all kinds.
             */
            rank: number;
            /**
             * Format: int64
             * @description The Elo rating, rounded to a whole number.
             */
            rating: number;
            /** @description For a bot name, its hosted bot's URL, which its owner published by playing it in a rated game: anyone may play against it in an unrated lobby of their own, as a hosted bot seat at this URL, which changes no rating. Null for hand play and house bots, and for a bot name without a URL, which was registered before names had URLs. */
            url: string | null;
            /** Format: int64 */
            wins: number;
        };
        LeaderboardEntry: {
            /** Format: uuid */
            competitor_id: string;
            /** Format: int64 */
            draws: number;
            /**
             * Format: int64
             * @description Rated games played: a competitor is listed after its first.
             */
            games: number;
            /**
             * @description bot: a bot name that an account registered. manual: an account's hand play. house: a default bot level, which the backend plays.
             * @enum {string}
             */
            kind: "bot" | "manual" | "house";
            /**
             * Format: date-time
             * @description When its latest rated game ended.
             */
            last_played_at: string;
            /** Format: int64 */
            losses: number;
            /** @description The name it is shown by: a bot's name, such as Kraken, the account's display name for its hand play, or Bot (level N) for a house bot. */
            name: string;
            /** @description The display name of the account that a bot name belongs to, as in "Kraken, by Dana"; null for hand play and house bots. */
            owner: string | null;
            /**
             * Format: int64
             * @description The place in the leaderboard's order, from 1: equal ratings, as shown, get the same rank, and the next rank skips as many places (1, 2, 2, 4). GET /leaderboard ranks within the kind it lists; a competitor's page among all kinds.
             */
            rank: number;
            /**
             * Format: int64
             * @description The Elo rating, rounded to a whole number.
             */
            rating: number;
            /** @description For a bot name, its hosted bot's URL, which its owner published by playing it in a rated game: anyone may play against it in an unrated lobby of their own, as a hosted bot seat at this URL, which changes no rating. Null for hand play and house bots, and for a bot name without a URL, which was registered before names had URLs. */
            url: string | null;
            /** Format: int64 */
            wins: number;
        };
        ListDefaultBotsResponseBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/ListDefaultBotsResponseBody.json
             */
            readonly $schema?: string;
            bots: components["schemas"]["DefaultBot"][];
            /** @description Whether this server offers the bots that it plays without a player's tab: kind hosted, a player's own bot server at a URL, and kind uploaded, a bot package uploaded to the lobby, as a player's own bot in their member settings and for a bot seat. When false, only the localhost bot and the default bots can be chosen. */
            server_bots: boolean;
        };
        ListMapTemplatesResponseBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/ListMapTemplatesResponseBody.json
             */
            readonly $schema?: string;
            maps: components["schemas"]["MapTemplateSummary"][];
        };
        ListUpgradesResponseBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/ListUpgradesResponseBody.json
             */
            readonly $schema?: string;
            upgrades: components["schemas"]["Upgrade"][];
        };
        /** @description Island Battles game_settings for a lobby create request. Choose exactly one map: map_template_id, a default map from GET /map/templates, or map, a map file; settings without a map are refused. The map must be made for exactly the lobby's teams: player_amount equal to the team count, and castle team colours exactly 1 to that count, in team order, with 0 for neutral castles. turn_timeout_seconds sets how long each turn lasts, from 5 to 600 seconds, 30 when left out, or null for no limit. max_generations sets the most turns the game may last, a whole number from 50 to 1200, 300 when left out. turn_pace_milliseconds sets how long each turn of a real-time game lasts, a whole number of milliseconds from 500 to 5000, 1000 when left out; a game is real-time when a player on its roster plays by hand (bot kind manual), and then no turn times out. rated, false when left out, makes the game rated: it then starts only when every member is signed in and plays by hand or with a bot under one of their bot names, no two teams belong to one account, and every bot seat is a default bot level, each level once; the ready gate refuses anything else with game_requirement of type urn:island-battles:problem:rated-sign-in-required, urn:island-battles:problem:rated-bot-name-required, urn:island-battles:problem:rated-bot-name-gone, urn:island-battles:problem:rated-same-account or urn:island-battles:problem:rated-seat, and the game records each team's competitor. In a rated lobby a bot seat that isn't a default bot level, or a level already seated, is invalid_seat_settings, and turning rated on while such a seat is there is invalid_game_settings. An invalid choice fails lobby creation with invalid_game_settings, and its detail names each invalid field, such as game_settings.map.castles[0].x. The host can change them while the lobby waits, with the lobby's changeLobbySettings (PUT …/settings), which checks them as on create: send one team per colour of the new map. The start uses the map, turn timeout, game length, turn pace and rated set last, and checks them again. */
        LobbySettings: {
            /** @description Start on this map file. Give this or map_template_id, not both. */
            map?: components["schemas"]["MapTemplate"];
            /**
             * Format: int64
             * @description Start on this default map, a map_template_id from GET /map/templates. Give this or map, not both.
             */
            map_template_id?: number;
            /**
             * Format: int64
             * @description The most turns the game may last, a whole number from 50 to 1200; 300 when left out. The turn that creates generation max_generations ends the game, if nothing has ended it before, and the team with the most castles wins.
             * @default 300
             */
            max_generations: number;
            /**
             * @description Whether the game is rated, false when left out. A rated lobby starts only when every member is signed in and plays by hand, with one of their bot names or with a bot from the leaderboard, no two teams belong to one account, every bot seat is a default bot level, each level once, or a bot from the leaderboard, each once, and the bots that play away have rated games left today. The game then records the competitor of each team, and its end changes their Elo ratings.
             * @default false
             */
            rated: boolean;
            /**
             * Format: int64
             * @description How long each turn lasts in a real-time game, in whole milliseconds from 500 to 5000; 1000 when left out. A game is real-time when a player on its roster plays by hand: each turn then ends this long after its generation was created, and the server plays it with the commands that arrived, so a player who sent nothing passes. A game of bots only doesn't use it and waits for every player.
             * @default 1000
             */
            turn_pace_milliseconds: number;
            /**
             * Format: int64
             * @description How long each turn lasts, in whole seconds from 5 to 600, counted from when its generation was created; 30 when left out. null means no limit, for example while debugging a bot. When a turn's deadline passes before every player has completed it, each team with a player who hadn't loses, and the game ends. In a real-time game, see turn_pace_milliseconds, no turn times out; instead a player the server hasn't heard from for this long, or for one turn plus 5 seconds when that is longer, by the end of a turn has left the game and their team is out, and a game that no request reaches for this long after its open turn's end is abandoned: the team with the most castles wins. null turns both rules off.
             * @default 30
             */
            turn_timeout_seconds: number | null;
        };
        MapTemplate: {
            /** @description Each castle's position in the list is its castle ID in every game built from the map. */
            castles: components["schemas"]["CastleTemplate"][];
            /** @description Name of the map, unique among default maps. */
            map_name: string;
            /**
             * Format: int64
             * @description Soldiers in every castle, owned or neutral, that sets no starting_soldier_amount.
             */
            neutral_starting_soldier_amount: number;
            /**
             * Format: int64
             * @description Width of the plane, which is centred on the origin.
             */
            plane_size_x: number;
            /**
             * Format: int64
             * @description Height of the plane, which is centred on the origin.
             */
            plane_size_y: number;
            /**
             * Format: int64
             * @description Teams the map is made for, from 2 to 16, one player each. Castle team colours run from 1 to player_amount, and every one of them owns at least one castle.
             */
            player_amount: number;
        };
        MapTemplateSummary: {
            /**
             * Format: int64
             * @description Castles on the map, team and neutral.
             */
            castle_count: number;
            map_name: string;
            /**
             * Format: int64
             * @description Choose the map by passing this ID as game_settings.map_template_id when creating a lobby.
             */
            map_template_id: number;
            /** Format: int64 */
            plane_size_x: number;
            /** Format: int64 */
            plane_size_y: number;
            /**
             * Format: int64
             * @description Teams the map is made for, its player_amount. A lobby on this map has exactly this many teams, in team colour order.
             */
            team_count: number;
        };
        /** @description The bot you choose for yourself. Send only the field of its kind: level for default, url for hosted, package_id for uploaded, and port, or none, for localhost; manual takes no other field. localhost, hosted and uploaded may also take name, one of your bot names. A localhost bot's port is from 1024 to 65535, 41414 when left out. A hosted bot's URL is https://, at most 2048 characters, without credentials, on port 443 or none, and names a host, not an IP address, whose addresses are all public: not loopback, private, link-local or unique-local. The server resolves it when you choose it, and again before it sends each turn. An uploaded bot is one of your ready packages in the lobby; if you replace or delete it, choose it again before getting ready. */
        MemberBot: {
            /**
             * Format: uuid
             * @description For kind hosted only, instead of url and name: a bot name by its competitor_id, one that the leaderboard lists (GET /leaderboard, with a url), or one of your own bot names. It plays at the name's URL as it is at the start. Another account's bot plays away: in a rated lobby it is that bot that plays for your team, your own hand play isn't in the game, and the daily caps and the pause apply to it. One of your own plays as if you named it. In a lobby that isn't rated it rates nothing.
             */
            competitor_id?: string;
            /**
             * @description localhost: your own bot, which island-battles-bot serve runs on your machine and your game client relays each turn to. default: a default bot of GET /bots/defaults, which the backend plays. hosted: your own bot server at url, which the server sends each turn to. uploaded: your own bot package in this lobby, package_id, which the server runs. manual: you play by hand in the 3D client; no bot plays for you. hosted and uploaded are offered only when GET /bots/defaults says server_bots.
             * @enum {string}
             */
            kind: "localhost" | "default" | "hosted" | "uploaded" | "manual";
            /**
             * Format: int64
             * @description For kind default only: a level that GET /bots/defaults lists.
             */
            level?: number;
            /** @description For kinds localhost, hosted and uploaded only: one of the bot names of your account (GET /account), compared without regard to case, that the bot plays as. Only a signed-in player has bot names. A rated lobby takes a bot only as a hosted bot under a bot name, which plays at the name's URL, and records the game under the name. Elsewhere the name changes nothing, except that a hosted bot without a url plays at its name's URL. */
            name?: string;
            /**
             * Format: uuid
             * @description For kind uploaded only: a package of yours in this lobby, from GET /game/lobbies/{lobby_id}/bot-packages.
             */
            package_id?: string;
            /**
             * Format: int64
             * @description For kind localhost only: the port on 127.0.0.1 where your bot serves (island-battles-bot serve --port N), where your game client delivers each turn. 41414 when absent.
             */
            port?: number;
            /** @description For kind hosted only: where the server POSTs each TurnDelivery. https:// on port 443, without credentials, on a host name (not an IP address) whose addresses are all public. Required unless the bot has a name or a competitor_id, which then plays at its own URL (GET /account). In a rated lobby a named hosted bot always plays at its name's URL, and url is ignored. */
            url?: string;
        };
        /** @description Island Battles member_settings for setLobbyMemberSettings: the bot that plays for you. {"bot": {"kind": "manual"}} is you, playing by hand in the 3D client: no bot plays or relays for you, and your client sends your commands with POST /generation/commands. {"bot": {"kind": "localhost"}} is your own bot, which island-battles-bot serve runs on your machine and your game client relays each turn to, on port 41414 or on the port you add, {"bot": {"kind": "localhost", "port": 41415}}, and {"bot": {"kind": "default", "level": N}} a default bot of a level that GET /bots/defaults lists, which the backend plays itself. When GET /bots/defaults says server_bots, also {"bot": {"kind": "hosted", "url": "https://…"}}, your own bot server at a public URL, or {"bot": {"kind": "uploaded", "package_id": "…"}}, one of your bot packages in the lobby; the server plays both, without your game client. A localhost, hosted or uploaded bot may add "name", one of the bot names of your account (GET /account), compared without regard to case: {"bot": {"kind": "localhost", "name": "Kraken"}}. A name that isn't one of yours, or any name from a guest, is invalid_member_settings; it is accepted in every lobby, but only a rated lobby, where a bot needs one, records the game under it. Anything else is invalid_member_settings. You choose a bot before getting ready, and can change it while the lobby waits: getting ready without one is refused with game_requirement of type urn:island-battles:problem:bot-not-chosen, and an uploaded bot that is no longer a ready package of yours in the lobby, replaced, deleted or someone else's, with type urn:island-battles:problem:bot-package-gone. The start checks every roster player and bot seat the same way. It then initializes one playable generation, which waits for every frozen roster player to complete their turn, and copies each roster player's bot, and each bot seat's, which GenerationState.bot reports from then on. */
        MemberSettings: {
            bot: components["schemas"]["MemberBot"];
        };
        PlayerGenerationLogs: {
            display_name: string;
            lines: components["schemas"]["GenerationLogLine"][];
            /** Format: int64 */
            team_color: number;
        };
        PresignedBotUpload: {
            /** @description The form fields to send before the file, unchanged. */
            fields: {
                [key: string]: string;
            };
            /** @description POST a multipart/form-data body here: every field of fields, then the zip as the last field, named file. */
            url: string;
        };
        Problem: {
            /** @enum {string} */
            code: "unauthenticated" | "not_found" | "not_host" | "lobby_full" | "team_full" | "phase_conflict" | "not_ready" | "empty_teams" | "stale_membership" | "idempotency_conflict" | "invalid_input" | "invalid_game_settings" | "invalid_seat_settings" | "invalid_member_settings" | "game_requirement" | "game_running" | "game_unavailable" | "watcher_kicked" | "watchers_full" | "sign_in_required" | "display_name_required" | "display_name_taken" | "unavailable" | "internal_error";
            detail: string;
            /** Format: int64 */
            status: number;
            /** @description The teams the error is about. For empty_teams, the teams that still need a player, in team order. */
            team_ids?: string[] | null;
            title: string;
            /** @description For game_requirement, the game's own problem type, a URI that the game documents, such as one meaning a player has yet to choose something in the game's API. Absent for every other code. */
            type?: string;
        };
        PublicGenerationCommand: components["schemas"]["SendSoldierGroupCommand"] | components["schemas"]["UpgradeCastleCommand"] | components["schemas"]["BurstCastleCommand"];
        RatingChange: {
            /**
             * Format: double
             * @description The competitor's rating after the game, a decimal: show it rounded.
             */
            after: number;
            /**
             * Format: double
             * @description The competitor's rating before the game, a decimal: show it rounded.
             */
            before: number;
            /** Format: uuid */
            competitor_id: string;
            /**
             * @description bot: a bot name that an account registered. manual: an account's hand play. house: a default bot level.
             * @enum {string}
             */
            kind: "bot" | "manual" | "house";
            /** @description The name the competitor is shown by: a bot's name, such as Kraken, the account's display name for its hand play, or Bot (level N) for a house bot. */
            name: string;
            /** @description The display name of the account that the bot name belongs to, for kind bot only. */
            owner?: string;
            /** Format: int64 */
            team_color: number;
        };
        RegisterBotNameRequest: {
            /** @description The bot's name: 3 to 24 characters after trimming, of letters, digits, spaces, - and _, not starting with Bot (level. It is unique across all accounts without regard to case. */
            name: string;
            /** @description The bot's hosted bot URL, where the server sends it each turn in a rated lobby: https:// on port 443, without credentials, on a host name (not an IP address) whose addresses are all public, as a hosted bot's. It becomes public on the leaderboard once the name has played a rated game, so that others can play against it. */
            url: string;
        };
        RegisterGenerationCommands: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/RegisterGenerationCommands.json
             */
            readonly $schema?: string;
            commands: components["schemas"]["PublicGenerationCommand"][];
            /** @description Seals your turn after this batch. Defaults to true. */
            complete?: boolean;
            /** Format: int64 */
            game_id: number;
            /** Format: int64 */
            generation_id: number;
            /** @description Game log lines written since your previous batch for this generation, appended in order after the lines already stored. At most 100 lines per player per generation, each of at most 500 characters; a batch that would break either limit is rejected whole with 422 and problem type urn:island-battles:problem:game-log-limit. */
            logs?: components["schemas"]["GenerationLogLine"][] | null;
            /** @description Your bot's memory, the BotTurnResult.memory it returned: any JSON object of at most 64 KiB as compact JSON. Only the batch with complete true saves it, replacing your previous memory, and an absent or null memory clears it; other batches ignore it. GenerationState.memory returns it. Larger memory is rejected whole with 422 and problem type urn:island-battles:problem:bot-memory-too-large. */
            memory?: {
                [key: string]: unknown;
            } | null;
            /**
             * Format: uuid
             * @description Identifies this batch. Retry a lost request with the same UUID and body; use a new UUID for every new batch.
             */
            request_id: string;
        };
        RelayedBot: {
            /** @description The seat's name in the lobby, such as "Localhost bot (port 41415)". */
            display_name: string;
            /**
             * Format: uuid
             * @description The seat's player ID on the roster, which GET /game/from-lobby/{lobby_id}/turn-delivery?player_id= takes.
             */
            player_id: string;
            /**
             * Format: int64
             * @description The port on 127.0.0.1 where your client delivers the seat's turns.
             */
            port: number;
            /** Format: int64 */
            team_color: number;
        };
        SendSoldierGroupCommand: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            command_type: "send_soldier_group";
            parameters: components["schemas"]["SendSoldierGroupParameters"];
        };
        SendSoldierGroupParameters: {
            /**
             * Format: int64
             * @description Any other castle.
             */
            destination_castle_id: number;
            /**
             * Format: int64
             * @description Castle your team owns.
             */
            origin_castle_id: number;
            /**
             * Format: int64
             * @description Soldiers to send; counts against soldiers already reserved by your team this turn.
             */
            soldier_amount: number;
        };
        SetBotNameURLRequest: {
            /** @description The bot's hosted bot URL, where the server sends it each turn in a rated lobby: https:// on port 443, without credentials, on a host name (not an IP address) whose addresses are all public, as a hosted bot's. It becomes public on the leaderboard once the name has played a rated game, so that others can play against it. */
            url: string;
        };
        SoldierGroup: {
            /**
             * Format: double
             * @description Enemies clash when their centers are within the larger of their radii.
             */
            clash_radius: number;
            /** Format: int64 */
            destination_castle_id: number;
            /**
             * Format: int64
             * @description Soldier group ID, never reassigned within a game.
             */
            id: number;
            /** Format: int64 */
            origin_castle_id: number;
            /** Format: int64 */
            soldiers: number;
            /**
             * Format: int64
             * @description Owning team; 0 is neutral.
             */
            team_color: number;
            /** @description Every upgrade type's level, copied from the origin castle when the group left. */
            upgrades: {
                [key: string]: number;
            };
            /** Format: double */
            x: number;
            /** Format: double */
            y: number;
        };
        /** @description One turn delivered to a bot that runs outside the backend, such as the player's own bot server: the BotTurn to play, and where and with which submit token to submit the answer. A bot server answers the delivery with 202 at once and submits its BotTurnResult itself as one completing batch. */
        TurnDelivery: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/TurnDelivery.json
             */
            readonly $schema?: string;
            submit: components["schemas"]["TurnSubmit"];
            turn: components["schemas"]["BotTurn"];
        };
        TurnSubmit: {
            /**
             * Format: date-time
             * @description When the turn ends, the BotTurn's turn_deadline_at, or null when turns have no time limit. An answer must be submitted before it: in a real-time game a later one is refused as a stale generation, and the bot passes the turn.
             */
            deadline_at: string | null;
            /** @description The submit token, sent as Authorization: Bearer <token>. It lets the bearer submit this player's turn of this generation and nothing else, and expires at the turn's deadline plus 30 seconds, or 24 hours after the generation when turns have no time limit. Keep it secret until the turn is over. */
            token: string;
            /**
             * Format: uri
             * @description Where to submit the answer: POST /generation/commands on the game's server, as an absolute URL.
             */
            url: string;
        };
        Upgrade: {
            /** @description What the levels' values measure. */
            description: string;
            /** @description Every level from 1 to max_level, in order. */
            levels: components["schemas"]["UpgradeLevel"][];
            /**
             * Format: int64
             * @description The highest level. Every castle starts at level 1.
             */
            max_level: number;
            type: components["schemas"]["UpgradeType"];
        };
        UpgradeCastleCommand: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            command_type: "upgrade_castle";
            parameters: components["schemas"]["UpgradeCastleParameters"];
        };
        UpgradeCastleParameters: {
            /**
             * Format: int64
             * @description Castle your team owns. It pays the level's soldier_cost from GET /game/upgrades.
             */
            castle_id: number;
            /** @description The type to raise by one level. Several upgrades of a castle in one turn each buy the next level. */
            upgrade_type: components["schemas"]["UpgradeType"];
        };
        UpgradeLevel: {
            /** Format: int64 */
            level: number;
            /**
             * Format: int64
             * @description Soldiers the castle pays to reach this level from the level below. Level 1 is free.
             */
            soldier_cost: number;
            /**
             * Format: double
             * @description The level's effect, in the unit its type's description names.
             */
            value: number;
        };
        /**
         * @description A castle upgrade. GET /game/upgrades lists each type's levels and costs.
         * @enum {string}
         */
        UpgradeType: "burst_potential" | "clash_stop_generations" | "generation_amount" | "speed";
        WatchGameEndedEvent: {
            /**
             * Format: int64
             * @enum {integer}
             */
            schema_version: 1;
            /** @description The final generation, with its outcome. */
            state: components["schemas"]["WatchState"];
            /** @enum {string} */
            type: "game.ended";
        };
        WatchGenerationEvent: {
            /**
             * Format: int64
             * @enum {integer}
             */
            schema_version: 1;
            state: components["schemas"]["WatchState"];
            /** @enum {string} */
            type: "game.next_generation";
        };
        WatchState: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/WatchState.json
             */
            readonly $schema?: string;
            /** @description The lobby's watch channel, watch:island-battles:<lobby_id>, which receives WatchGenerationEvent after each turn and WatchGameEndedEvent after the last one. */
            channel: string;
            /** @description The teams that have resigned or left this real-time game so far, as in GenerationState. */
            departures: components["schemas"]["Departure"][];
            /** Format: int64 */
            game_id: number;
            generation: components["schemas"]["Generation"];
            /** Format: int64 */
            generation_id: number;
            /** Format: uuid */
            lobby_id: string;
            /**
             * Format: int64
             * @description The lobby round that the game plays. A lobby plays one game per round, and the /game/from-lobby/{lobby_id} routes serve its newest game: compare this with the lobby snapshot's lobby.round to tell whether the game is the one the lobby started last.
             */
            lobby_round: number;
            /** @description How the game ended. Absent while the game runs. */
            outcome?: components["schemas"]["GameOutcome"];
            /** @description The teams that play, with their lobby names and the display names their players had at the start. */
            teams: components["schemas"]["GameTeam"][];
            /**
             * Format: date-time
             * @description When this generation's turn ends, as in GenerationState: after the turn pace in a real-time game, otherwise when it times out. Null when turns have no time limit, and once the game has ended. Watchers never apply a deadline themselves.
             */
            turn_deadline_at: string | null;
            /**
             * Format: int64
             * @description The game's turn pace in milliseconds when the game is real-time, as in GenerationState; null when the game waits for every player.
             */
            turn_pace_milliseconds: number | null;
        };
    };
    responses: never;
    parameters: never;
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    getIslandAccount: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["IslandAccount"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    deleteIslandAccount: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    registerIslandBotName: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["RegisterBotNameRequest"];
            };
        };
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Competitor"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    setIslandBotNameURL: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                competitor_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SetBotNameURLRequest"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Competitor"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    deleteIslandBotName: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                competitor_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    resumeIslandBotNameAway: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                competitor_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Competitor"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listDefaultBots: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ListDefaultBotsResponseBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getBotCompute: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BotCompute"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
        };
    };
    applyIslandLobbyTurnDeadline: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GenerationState"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getIslandLobbyDemo: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The demo file. */
            200: {
                headers: {
                    /** @description attachment, with the demo's file name. */
                    "Content-Disposition"?: string;
                    /** @description gzip when the request's Accept-Encoding allows it; absent otherwise. */
                    "Content-Encoding"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Demo"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getIslandLobbyGameEndedDelivery: {
        parameters: {
            query?: {
                /** @description A localhost bot seat that you relay, one of GenerationState.relayed_bots, to read its delivery instead of yours. Absent for your own. */
                player_id?: string;
            };
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GameEndedDelivery"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getIslandLobbyGeneration: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GenerationState"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getIslandLobbyGenerationLogs: {
        parameters: {
            query: {
                /** @description The generation whose lines to read, from Generation.id. */
                generation_id: number;
            };
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GenerationLogs"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    resignIslandLobbyGame: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GenerationState"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getIslandLobbyTurnDelivery: {
        parameters: {
            query?: {
                /** @description A localhost bot seat that you relay, one of GenerationState.relayed_bots, to read its turn instead of yours. Absent for your own turn. */
                player_id?: string;
            };
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TurnDelivery"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    watchIslandLobbyGeneration: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WatchState"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    watchIslandLobbyDemo: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The demo file. */
            200: {
                headers: {
                    /** @description attachment, with the demo's file name. */
                    "Content-Disposition"?: string;
                    /** @description gzip when the request's Accept-Encoding allows it; absent otherwise. */
                    "Content-Encoding"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Demo"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    watchIslandLobbyGenerationLogs: {
        parameters: {
            query: {
                /** @description The generation whose lines to read, from Generation.id. */
                generation_id: number;
            };
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GenerationLogs"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listIslandBotPackages: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BotPackages"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    startIslandBotPackageUpload: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["BotPackageUploadRequest"];
            };
        };
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BotPackageUpload"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    createIslandBotUploadToken: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BotUploadToken"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    deleteIslandBotPackage: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
                package_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description No Content */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    completeIslandBotPackageUpload: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path: {
                lobby_id: string;
                package_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BotPackage"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listUpgrades: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ListUpgradesResponseBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getIslandGameDemo: {
        parameters: {
            query?: {
                /** @description A demo token, ibdt1.…, as GameEndedDelivery.demo.url carries it. Absent when the Authorization header carries the demo token or a player token instead. */
                token?: string;
            };
            header?: {
                Authorization?: string;
            };
            path: {
                id: number;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The demo file. */
            200: {
                headers: {
                    /** @description attachment, with the demo's file name. */
                    "Content-Disposition"?: string;
                    /** @description gzip when the request's Accept-Encoding allows it; absent otherwise. */
                    "Content-Encoding"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Demo"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    registerGenerationCommand: {
        parameters: {
            query?: never;
            header?: {
                Authorization?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["RegisterGenerationCommands"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GenerationState"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    listIslandLeaderboard: {
        parameters: {
            query?: {
                /** @description Lists only competitors of this kind, ranked among themselves: bot, a registered bot name; manual, an account's hand play; house, a default bot level. Absent, every kind. */
                kind?: "bot" | "manual" | "house";
                /** @description How many competitors to return, 1 to 100. */
                limit?: number;
                /** @description How many competitors to skip, in the leaderboard's order. */
                offset?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    "Cache-Control"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Leaderboard"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    getIslandCompetitor: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                competitor_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    "Cache-Control"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["LeaderboardCompetitor"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
            /** @description Unprocessable Entity */
            422: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
    createLobbyConnectionToken: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ConnectionTokenInputBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConnectionToken"];
                };
            };
            /** @description Unauthorized */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
            /** @description Forbidden */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
            /** @description Not Found */
            404: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
            /** @description Conflict */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
            /** @description Unprocessable Entity */
            422: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
            /** @description Internal Server Error */
            500: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
            /** @description Service Unavailable */
            503: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
        };
    };
    listMapTemplates: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ListMapTemplatesResponseBody"];
                };
            };
            /** @description Error */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ErrorModel"];
                };
            };
        };
    };
}
