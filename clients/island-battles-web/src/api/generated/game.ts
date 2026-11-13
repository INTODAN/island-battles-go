// Generated from backend/api/openapi.json by tools/codegen/generate.ts. Do not edit; run `bun run generate`.

export interface paths {
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
         * Apply the current turn's deadline for a roster player
         * @description Requires a place on the game's frozen roster; the lobby is not consulted. The server runs no timers, so a turn times out when a request applies its deadline, GenerationState.turn_deadline_at. Every game request does that first, and a client that is waiting for other players calls this once about a second after the deadline. Before the deadline, and for a game that has ended or has no turn timeout, it changes nothing. After it, every team with a player who hasn't completed the turn loses, the game ends with reason turn_timeout, and among the other teams still standing the team with the most castles wins; equal counts, or no team left, are a draw. The open generation stays the final one, game.ended is pushed to every roster player, and later commands conflict (409) with problem type urn:island-battles:problem:game-ended. Calling it again is safe and ends the game once. Returns your current state, as GET /game/from-lobby/{lobby_id}/generation does. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
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
    "/game/from-lobby/{lobby_id}/generation": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read the current generation for a roster player
         * @description Requires a place on the game's frozen roster. The game does not consult the lobby, so access continues after the lobby is deleted. Returns castle and soldier group IDs, which are never reassigned within a game, troop state, every castle's and soldier group's upgrade levels, recorded clashes with per-participant before/after counts, your turn completion flag, the turn's deadline, and the outcome once the game has ended. Like every game request, it first applies the turn's deadline once it has passed, which ends the game. A finished game stays readable until the game is deleted, 7 days after its last generation by default; then this returns 404. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
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
    "/game/from-lobby/{lobby_id}/turn-delivery": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Read your turn as a delivery for your bot, with a one-turn submit token
         * @description Requires a place on the game's frozen roster; the lobby is not consulted. Returns the caller's turn of the current generation as a TurnDelivery: the BotTurn the server builds for their bot (the board, their team, the upgrade cost table, the memory their latest completed turn sent and the deadline), and how to submit the answer: submit.url, POST /generation/commands on the game's public URL, and submit.token, a submit token that submits exactly this player's turn of this generation. A game client relays it to the player's bot server, island-battles-bot serve, which answers 202 at once, plays the turn and submits its commands, logs and memory itself, with complete true and Authorization: Bearer <token>; the client submits nothing. Each call issues a new token for the same turn. Like every game request, it first applies the turn's deadline once it has passed. When the caller has nothing to play it returns 409: the game has ended (problem type urn:island-battles:problem:game-ended), their team has been eliminated, or they have already completed the turn. A lobby can play several games, one per round (lobby.round, raised by POST /lobby/v1/games/island-battles/lobbies/{lobby_id}/reopen once a game has ended); this route serves the lobby's newest game and its frozen roster, and an older game can't be reached once the next one starts.
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
         * @description Lists every upgrade type, sorted by type, with what its levels measure and, for each level from 1 to max_level, its effect and the soldiers a castle pays to reach it from the level below. Every castle starts at level 1. The table is static and needs no authentication. Castles and soldier groups carry their current levels, and upgrade_castle in POST /generation/commands buys the next one.
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
         * @description Requires a player token of a player on the game's frozen roster (PlayerToken), or a submit token: Authorization: Bearer ibst1.… from GET /game/from-lobby/{lobby_id}/turn-delivery stands for its player for the batch of its game and generation only, and anything else (another game or generation, an expired or altered token, a player no longer on the roster) is 401. Either way the lobby is not consulted, and the rules below are the same. Players send send_soldier_group and upgrade_castle commands for castles their team owns; other command types are forbidden (403). upgrade_castle buys the next level of one upgrade type and pays that level's soldier_cost, listed by GET /game/upgrades, from the castle's soldiers. A castle may be upgraded several times in one turn, and each upgrade buys the next level. Every command is checked against the generation and every command already accepted this turn, including your own earlier batches, so an accepted command cannot fail when the turn runs. An invalid command rejects the whole batch, and nothing of it is stored. Malformed parameters are 422. Rejections that clients tell apart carry a problem type: a castle that is not in the generation is 422 urn:island-battles:problem:castle-not-found, an unknown upgrade_type is 422 urn:island-battles:problem:unknown-upgrade-type, a castle your team does not own is 403 urn:island-battles:problem:castle-not-owned, an upgrade past its type's max_level, counting this turn's upgrades, is 409 urn:island-battles:problem:upgrade-max-level, and soldiers or an upgrade cost beyond what the castle has left after this turn's reservations is 409 urn:island-battles:problem:insufficient-soldiers. A castle may spend all its soldiers. Complete defaults to true; an empty batch can complete a turn. The last frozen roster completion advances exactly once. The game ends after the turn that leaves one team with castles or soldier groups, a draw when the last teams are eliminated in the same turn, or after the turn that creates generation 300, where the team with the most castles wins. That turn's generation is the last, its state carries the outcome, and every later command conflicts (409) with problem type urn:island-battles:problem:game-ended. A turn also ends the game when its deadline, GenerationState.turn_deadline_at, passes before every roster player completed it: each team with a player who hadn't loses (outcome reason turn_timeout), the open generation stays the last, and a batch for it after the deadline conflicts with the same problem type. Retry the same request UUID and payload safely, also after the game ended; changed payloads and stale new requests conflict. A batch may also carry game log lines, which are stored in order with the generation and read with GET /game/from-lobby/{lobby_id}/logs. A player has at most 100 lines per generation, each of at most 500 characters; a batch over either limit is rejected whole (422) with problem type urn:island-battles:problem:game-log-limit. A batch may also carry the memory of the player's bot, any JSON object of at most 64 KiB as compact JSON. The batch with complete true saves it, replacing the player's previous memory, and an absent or null memory clears it; other batches ignore it. GenerationState.memory returns the memory sent with the player's latest completed turn, and the retry hash covers it. Larger memory is rejected whole (422) with problem type urn:island-battles:problem:bot-memory-too-large.
         */
        post: operations["registerGenerationCommand"];
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
        BotChoice: {
            /** @description For kind hosted, the host name of the bot's URL, such as bot.example.com; the rest of the URL is not shown. Absent for the other kinds. */
            host?: string;
            /**
             * @description localhost: the player's own bot, which island-battles-bot serve runs on their machine; their game client relays each turn to it. default: a default bot of GET /bots/defaults, which the backend plays itself, so the player's client submits nothing. hosted: the player's own bot server at a public URL, which the server sends each turn to; no client relays it. uploaded: a bot package uploaded to the lobby, which the server runs; no client relays it.
             * @enum {string}
             */
            kind: "localhost" | "default" | "hosted" | "uploaded";
            /**
             * Format: int64
             * @description For kind default, the level of the default bot, one that GET /bots/defaults lists. Absent for the other kinds.
             */
            level?: number;
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
        /** @description Island Battles seat_settings for a bot seat that the host adds with POST …/seats: exactly one of bot_level, bot_url and bot_package_id. {"bot_level": N}, with a level that GET /bots/defaults lists, is a default bot that the backend plays, named "Bot (level N)". When GET /bots/defaults says server_bots, also {"bot_url": "https://…"}, a hosted bot under the rules of MemberBot's url, named "Hosted bot", or {"bot_package_id": "…"}, any ready bot package of the lobby, named after its owner, such as "Alex's bot"; the server plays both. A seat whose package is gone by the start stops it with game_requirement of type urn:island-battles:problem:bot-package-gone. */
        BotSeatSettings: {
            /**
             * Format: int64
             * @description A default bot of this level, one that GET /bots/defaults lists, which the backend plays. The seat is named "Bot (level N)".
             */
            bot_level?: number;
            /**
             * Format: uuid
             * @description Any member's bot package in this lobby, which the server runs. The seat is named after its owner, such as "Alex's bot". Only when GET /bots/defaults says server_bots.
             */
            bot_package_id?: string;
            /** @description A hosted bot at this URL, under the rules of a member's hosted bot; the server sends it each turn. The seat is named "Hosted bot". Only when GET /bots/defaults says server_bots. */
            bot_url?: string;
        };
        /** @description What a bot is given to play one turn. A bot is a stateless function: whatever it needs to keep between turns goes in the memory it returns, which the server stores per player and game. */
        BotTurn: {
            /** Format: int64 */
            game_id: number;
            generation: components["schemas"]["Generation"];
            /** @description The memory the bot returned with its previous turn in this game, or null on its first turn and after a turn that returned null. */
            memory: {
                [key: string]: unknown;
            } | null;
            /** Format: int64 */
            team_color: number;
            /**
             * Format: date-time
             * @description When the turn times out, GenerationState.turn_deadline_at, or null when turns have no time limit. A bot that hasn't answered by then makes its whole team lose.
             */
            turn_deadline_at: string | null;
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
        Castle: {
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
            schema_version: 1;
            /** @description The teams that played, with their lobby names and the display names their players had at the start. */
            teams: components["schemas"]["GameTeam"][];
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
            /**
             * @description last_team_standing: one team was left after every other team lost all its castles and soldier groups. all_eliminated: the last teams were eliminated in the same turn, a draw. generation_cap: the game reached its last generation; the team with the most castles won, and equal counts are a draw. turn_timeout: a turn passed its deadline; every team with a player who hadn't completed it lost, and among the other teams still standing the team with the most castles won, while equal counts, or no team left, are a draw.
             * @enum {string}
             */
            reason: "last_team_standing" | "all_eliminated" | "generation_cap" | "turn_timeout";
            /** @description The outcome in one sentence, naming the winning team, and for turn_timeout the teams that lost and their players who didn't answer. */
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
             * @description Generation ID; increases with every turn.
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
            /** @description Your bot in this game, the one you chose in the lobby, copied when the game started. For kind localhost your client delivers each turn to your bot, which submits its answer. For kind default the backend plays your turns itself as soon as each generation is created. For kinds hosted and uploaded the server plays your turns with your bot, without your client. Only for kind localhost does your client relay anything. */
            bot: components["schemas"]["BotChoice"];
            /** @description Your personal Centrifugo channel, which receives GenerationEvent after each turn and GameEndedEvent after the last one. */
            channel: string;
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
            /** @description The memory sent with your latest completed turn of this game, for your bot's next BotTurn.memory. Null before your first completed turn, and after a completed turn without memory. */
            memory: {
                [key: string]: unknown;
            } | null;
            /** @description How the game ended. Absent while the game runs. A finished game keeps its final generation and rejects every command. */
            outcome?: components["schemas"]["GameOutcome"];
            /** Format: uuid */
            player_id: string;
            /** @description Whether you completed this generation's turn. */
            submitted: boolean;
            /** Format: int64 */
            team_color: number;
            /**
             * Format: date-time
             * @description When this generation's turn times out: the generation's creation time plus the lobby's turn_timeout_seconds. Null when turns have no time limit, and once the game has ended. If a player of a team hasn't completed the turn by then, the team loses and the game ends. A client waiting for other players calls POST /game/from-lobby/{lobby_id}/deadline about a second after it, because the server runs no timers.
             */
            turn_deadline_at: string | null;
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
        /** @description Island Battles game_settings for a lobby create request. Choose at most one map: map_template_id or map. The map must be made for exactly the lobby's teams: player_amount equal to the team count, and castle team colours exactly 1 to that count, in team order, with 0 for neutral castles. Without a map the game starts on a generated map. turn_timeout_seconds sets how long each turn lasts, from 5 to 600 seconds, 30 when left out, or null for no limit. An invalid choice fails lobby creation with invalid_game_settings, and its detail names each invalid field, such as game_settings.map.castles[0].x. The host can change them while the lobby waits, with the lobby's changeLobbySettings (PUT …/settings), which checks them as on create: send one team per colour of the new map. The start uses the map and turn timeout set last. */
        LobbySettings: {
            /** @description Start on this map file. Cannot be combined with map_template_id. */
            map?: components["schemas"]["MapTemplate"];
            /**
             * Format: int64
             * @description Start on this default map, a map_template_id from GET /map/templates. Cannot be combined with map.
             */
            map_template_id?: number;
            /**
             * Format: int64
             * @description How long each turn lasts, in whole seconds from 5 to 600, counted from when its generation was created; 30 when left out. null means no limit, for example while debugging a bot. When a turn's deadline passes before every player has completed it, each team with a player who hadn't loses, and the game ends.
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
        /** @description The bot you choose for yourself. Send only the field of its kind: level for default, url for hosted, package_id for uploaded, and none for localhost. A hosted bot's URL is https://, at most 2048 characters, without credentials, on port 443 or none, and names a host, not an IP address, whose addresses are all public: not loopback, private, link-local or unique-local. The server resolves it when you choose it, and again before it sends each turn. An uploaded bot is one of your ready packages in the lobby; if you replace or delete it, choose it again before getting ready. */
        MemberBot: {
            /**
             * @description localhost: your own bot, which island-battles-bot serve runs on your machine and your game client relays each turn to. default: a default bot of GET /bots/defaults, which the backend plays. hosted: your own bot server at url, which the server sends each turn to. uploaded: your own bot package in this lobby, package_id, which the server runs. hosted and uploaded are offered only when GET /bots/defaults says server_bots.
             * @enum {string}
             */
            kind: "localhost" | "default" | "hosted" | "uploaded";
            /**
             * Format: int64
             * @description For kind default only: a level that GET /bots/defaults lists.
             */
            level?: number;
            /**
             * Format: uuid
             * @description For kind uploaded only: a package of yours in this lobby, from GET /game/lobbies/{lobby_id}/bot-packages.
             */
            package_id?: string;
            /** @description For kind hosted only: where the server POSTs each TurnDelivery. https:// on port 443, without credentials, on a host name (not an IP address) whose addresses are all public. */
            url?: string;
        };
        /** @description Island Battles member_settings for setLobbyMemberSettings: the bot that plays for you. {"bot": {"kind": "localhost"}} is your own bot, which island-battles-bot serve runs on your machine and your game client relays each turn to, and {"bot": {"kind": "default", "level": N}} a default bot of a level that GET /bots/defaults lists, which the backend plays itself. When GET /bots/defaults says server_bots, also {"bot": {"kind": "hosted", "url": "https://…"}}, your own bot server at a public URL, or {"bot": {"kind": "uploaded", "package_id": "…"}}, one of your bot packages in the lobby; the server plays both, without your game client. Anything else is invalid_member_settings. You choose a bot before getting ready, and can change it while the lobby waits: getting ready without one is refused with game_requirement of type urn:island-battles:problem:bot-not-chosen, and an uploaded bot that is no longer a ready package of yours in the lobby, replaced, deleted or someone else's, with type urn:island-battles:problem:bot-package-gone. The start checks every roster player and bot seat the same way. It then initializes one playable generation, which waits for every frozen roster player to complete their turn, and copies each roster player's bot, and each bot seat's, which GenerationState.bot reports from then on. */
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
            code: "unauthenticated" | "not_found" | "not_host" | "lobby_full" | "team_full" | "phase_conflict" | "not_ready" | "empty_teams" | "stale_membership" | "idempotency_conflict" | "invalid_input" | "invalid_game_settings" | "invalid_seat_settings" | "invalid_member_settings" | "game_requirement" | "game_running" | "game_unavailable" | "watcher_kicked" | "watchers_full" | "unavailable" | "internal_error";
            detail: string;
            /** Format: int64 */
            status: number;
            /** @description The teams the error is about. For empty_teams, the teams that still need a player, in team order. */
            team_ids?: string[] | null;
            title: string;
            /** @description For game_requirement, the game's own problem type, a URI that the game documents, such as one meaning a player has yet to choose something in the game's API. Absent for every other code. */
            type?: string;
        };
        PublicGenerationCommand: components["schemas"]["SendSoldierGroupCommand"] | components["schemas"]["UpgradeCastleCommand"];
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
             * @description When the turn times out, the BotTurn's turn_deadline_at, or null when turns have no time limit. An answer must be submitted before it.
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
        UpgradeType: "clash_stop_generations" | "speed";
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
             * @description When this generation's turn times out, as in GenerationState. Null when turns have no time limit, and once the game has ended. Watchers never apply a deadline themselves.
             */
            turn_deadline_at: string | null;
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
    getIslandLobbyTurnDelivery: {
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
