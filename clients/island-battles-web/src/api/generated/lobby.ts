// Generated from lobby/api/openapi.json by tools/codegen/generate.ts. Do not edit; run `bun run generate`.

export interface paths {
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
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Get a lobby
         * @description Returns the caller-authorized lobby snapshot.
         */
        get: operations["getLobby"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/heartbeat": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Heartbeat a lease
         * @description Renews one client lease; reconnecting does not create membership.
         */
        post: operations["heartbeatLobby"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/leases/{client_instance_id}": {
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
         * Release a lease
         * @description Releases one client lease without leaving durable membership.
         */
        delete: operations["releaseLobbyLease"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/membership": {
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
         * Leave a lobby
         * @description Leaves durable membership across client instances.
         */
        delete: operations["leaveLobby"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/ready": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /**
         * Set readiness
         * @description ready is required; false explicitly clears readiness. A member needs a team to get ready, or it fails with not_ready. A game can also require something of its own first, such as a choice made in the game's API: then getting ready fails with 409 game_requirement, whose type is the game's problem type and whose detail says what to do.
         */
        put: operations["setLobbyReady"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/seats": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Add a seat
         * @description Adds a seat that no person plays, such as the game's bot, to a team, and returns the snapshot, where the seat is a member of kind seat. Only the host can add one (otherwise not_host), and only while the lobby waits (otherwise phase_conflict). The game checks seat_settings and names the seat; settings it doesn't understand fail with invalid_seat_settings, and a game without seats refuses every one. The seat takes its team, which then holds no other member, and counts toward the lobby's capacity: a team that already has a person or a seat is team_full, and a lobby with as many members as teams is lobby_full. It counts toward the members a start needs, is always ready, never disconnects, and joins the start roster with its own player_id. A start still needs the host, a person. A team_id that isn't one of the lobby's teams is invalid_input. The optional request_id makes a retry after a lost response safe: repeating it with the same team_id and seat_settings returns the current snapshot without a second seat, and with another team or settings fails with idempotency_conflict. Without one, each call adds a seat.
         */
        post: operations["addLobbySeat"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/seats/{seat_id}": {
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
         * Remove a seat
         * @description Removes a seat that the host added, by its member ID, and returns the snapshot. Only the host can remove one, and only while the lobby waits. Removing a seat again changes nothing; an ID that names no seat of the lobby is invalid_input.
         */
        delete: operations["removeLobbySeat"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/settings": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /**
         * Change a lobby's settings
         * @description Replaces the lobby's game_settings and teams, and returns the snapshot. Only the host can change them (otherwise not_host), and only while the lobby waits (otherwise phase_conflict). The body has the rules of a create request: from 2 to 16 teams with unique names (otherwise invalid_input), and an optional game_settings object that the game checks against the new teams (otherwise invalid_game_settings). The teams are rebuilt by position: the lobby's first team keeps its ID and takes the first name, and so on. New teams are added at the end. Teams beyond the new count are removed: a person on one stays in the lobby without a team, and a seat on one is removed, as removeLobbySeat removes it. Every person's ready is cleared, seats stay ready, and the lobby's capacity becomes the new number of teams. A change that would leave more members than teams, people and the seats that keep their team, fails with lobby_full and changes nothing. Settings and teams equal to the lobby's change nothing: the snapshot comes back with the same version and nobody's ready is cleared. Otherwise the change publishes lobby.changed.
         */
        put: operations["changeLobbySettings"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/start": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Start a lobby
         * @description Starts an eligible lobby and freezes its roster. The host can start once at least two members are present and every member has chosen a team, is ready and is connected; otherwise it fails with not_ready. The snapshot's start_rules list the game's extra conditions: with every_team_needs_member, a lobby with an empty team fails with empty_teams, whose team_ids name the empty teams. A game that requires something of each member before they get ready checks the whole roster again, and fails with game_requirement for a member who doesn't meet it.
         */
        post: operations["startLobby"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/team": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /**
         * Select a team
         * @description team_id is required and may be null to clear the selection. A team holds one member: a team that already has a person or a seat is team_full.
         */
        put: operations["selectLobbyTeam"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/watch": {
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
         * Stop watching a lobby
         * @description Removes your watcher row and unsubscribes you from the watch channel. A kicked watcher stays kicked. Without a watcher row it is not_found.
         */
        delete: operations["unwatchLobby"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/watchers": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List a lobby's watchers
         * @description Lists the watchers the host hasn't kicked, with their display names, for the lobby's active members.
         */
        get: operations["listLobbyWatchers"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/watchers/{player_id}": {
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
         * Kick a watcher
         * @description Lets the host remove a watcher, in any phase. The watcher is unsubscribed from the watch channel at once and can't watch again with that session and the join code (watcher_kicked); the kick bars the player ID, so a new guest session can watch again, and in a waiting lobby the kicked player can still join as a member. Kicking them again changes nothing; a player who isn't a watcher of the lobby is not_found.
         */
        delete: operations["kickLobbyWatcher"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/lobbies": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Create a lobby
         * @description Creates a lobby with its teams, from 2 to 16 with unique names. A team holds one member, a person or a seat, so the lobby holds as many members as it has teams; its capacity in the snapshot is that number. The optional game_settings object is checked by the game now; invalid settings fail with invalid_game_settings. The lobby stores the settings without reading them and returns them in every snapshot, including the started one. The host can change them, with the teams, while the lobby waits (PUT …/settings). Repeating the same request_id and payload within the idempotency window, 24 hours by default, returns the committed snapshot. The lobby is deleted, in any phase, once nobody has been connected to it for an hour, and in any case a day after creation, unless its game sets other times.
         */
        post: operations["createLobby"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/memberships": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Join a lobby
         * @description Joins by game key and join code; a lobby with as many members as teams is full (lobby_full). Started lobbies allow only frozen-roster rejoin.
         */
        post: operations["joinLobby"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/sessions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Create a guest session
         * @description Creates one guest identity and bearer credential.
         */
        post: operations["createLobbySession"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/lobby/v1/watches": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Watch a lobby
         * @description Lets anyone with a guest session and the lobby's join code watch it, in any phase: waiting, started or closed while it still exists. Adds your watcher row, or finds the one you have, and returns the lobby and its watch channel, where its game publishes what watchers may see; the game documents its own watcher reads. A watcher takes no seat and counts toward nothing: not the lobby's capacity, the start roster, leases, the grace period or the lobby's retention. Starting to watch changes the snapshot's watcher_count. An active member of the lobby gets no watcher row and keeps their member access (member is true). A wrong code, or a code of another game, is not_found; a watcher the host kicked is refused with watcher_kicked, and a new watcher beyond the lobby's limit of watchers with watchers_full.
         */
        post: operations["watchLobby"];
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
        AddSeatBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/AddSeatBody.json
             */
            readonly $schema?: string;
            /**
             * Format: uuid
             * @description Optional. Repeating a request ID with the same team and settings returns the current snapshot without a second seat; with another team or settings it fails with idempotency_conflict.
             */
            request_id?: string;
            /** @description Settings for a seat that no person plays, as a JSON object that only the game reads, such as the level of the game's bot that plays the seat. The game checks them when the host adds the seat; invalid settings fail with invalid_seat_settings. */
            seat_settings: {
                [key: string]: unknown;
            };
            /**
             * Format: uuid
             * @description The team the seat joins.
             */
            team_id: string;
        };
        ChangeSettingsBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/ChangeSettingsBody.json
             */
            readonly $schema?: string;
            /** @description Settings for the game, as a JSON object that only the game reads. The game checks them when the lobby is created, and again when the host changes them while the lobby waits. The lobby returns them compacted with sorted keys, and omitted settings as an empty object. */
            game_settings?: {
                [key: string]: unknown;
            };
            /** @description The lobby's new teams, in order, with the rules of a create request. They replace the lobby's teams by position: the first keeps the lobby's first team and its ID and takes this name, and so on; teams beyond the lobby's are added, and the lobby's teams beyond these are removed. */
            teams: components["schemas"]["TeamDefinition"][] | null;
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
        CreateLobbyBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/CreateLobbyBody.json
             */
            readonly $schema?: string;
            /** Format: uuid */
            client_instance_id: string;
            game_key: string;
            /** @description Settings for the game, as a JSON object that only the game reads. The game checks them when the lobby is created, and again when the host changes them while the lobby waits. The lobby returns them compacted with sorted keys, and omitted settings as an empty object. */
            game_settings?: {
                [key: string]: unknown;
            };
            name: string;
            /** Format: uuid */
            request_id: string;
            /** @description The lobby's teams, in order. Each holds one member, a person or a seat, so the lobby holds as many members as it has teams. */
            teams: components["schemas"]["TeamDefinition"][] | null;
        };
        CreateSessionBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/CreateSessionBody.json
             */
            readonly $schema?: string;
            display_name: string;
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
        GuestSession: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/GuestSession.json
             */
            readonly $schema?: string;
            access_token: string;
            /** Format: date-time */
            expires_at: string;
            /** Format: uuid */
            player_id: string;
        };
        HeartbeatBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/HeartbeatBody.json
             */
            readonly $schema?: string;
            /** Format: uuid */
            client_instance_id: string;
            /** Format: int64 */
            membership_generation: number;
        };
        HeartbeatResult: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/HeartbeatResult.json
             */
            readonly $schema?: string;
            /** Format: date-time */
            disconnected_until: string | null;
            /** Format: date-time */
            lease_expires_at: string;
            /** Format: int64 */
            membership_generation: number;
            /** Format: int64 */
            version: number;
        };
        JoinLobbyBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/JoinLobbyBody.json
             */
            readonly $schema?: string;
            /** Format: uuid */
            client_instance_id: string;
            game_key: string;
            join_code: string;
        };
        Lobby: {
            /**
             * Format: int64
             * @description The number of members the lobby holds, which is its number of teams, since each team holds one member.
             */
            capacity: number;
            /** Format: date-time */
            created_at: string;
            game_key: string;
            /** @description Settings for the game, as a JSON object that only the game reads. The game checks them when the lobby is created, and again when the host changes them while the lobby waits. The lobby returns them compacted with sorted keys, and omitted settings as an empty object. */
            game_settings: {
                [key: string]: unknown;
            };
            host_player_id: string | null;
            id: string;
            join_code: string;
            name: string;
            phase: string;
            /** Format: int64 */
            version: number;
        };
        LobbyInvalidation: {
            event_id: string;
            game_key: string;
            lobby_id: string;
            /**
             * Format: int64
             * @enum {integer}
             */
            schema_version: 1;
            /** @enum {string} */
            type: "lobby.changed";
            /** Format: int64 */
            version: number;
        };
        Member: {
            active: boolean;
            /** Format: date-time */
            disconnected_until: string | null;
            display_name: string;
            /** Format: int64 */
            generation: number;
            id: string;
            /** Format: int64 */
            join_sequence: number;
            /** Format: date-time */
            joined_at: string;
            /**
             * @description human: a person who joined. seat: a seat that no person plays, such as the game's bot, which the host added to a team; it is always ready, never disconnects or hosts, and the game plays it from seat_settings. Its id is the seat ID for removing it.
             * @enum {string}
             */
            kind: "human" | "seat";
            player_id: string;
            ready: boolean;
            /** @description For a seat, the settings the host added it with, compacted with sorted keys, which only the game reads. Null for a person. */
            seat_settings: {
                [key: string]: unknown;
            } | null;
            team_id: string | null;
        };
        Problem: {
            /** @enum {string} */
            code: "unauthenticated" | "not_found" | "not_host" | "lobby_full" | "team_full" | "phase_conflict" | "not_ready" | "empty_teams" | "stale_membership" | "idempotency_conflict" | "invalid_input" | "invalid_game_settings" | "invalid_seat_settings" | "game_requirement" | "watcher_kicked" | "watchers_full" | "unavailable" | "internal_error";
            detail: string;
            /** Format: int64 */
            status: number;
            /** @description The teams the error is about. For empty_teams, the teams that still need a player, in team order. */
            team_ids?: string[] | null;
            title: string;
            /** @description For game_requirement, the game's own problem type, a URI that the game documents, such as one meaning a player has yet to choose something in the game's API. Absent for every other code. */
            type?: string;
        };
        ReadyBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/ReadyBody.json
             */
            readonly $schema?: string;
            ready: boolean;
        };
        SelectTeamBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/SelectTeamBody.json
             */
            readonly $schema?: string;
            /** Format: uuid */
            team_id: string | null;
        };
        Snapshot: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/Snapshot.json
             */
            readonly $schema?: string;
            channel: string;
            lobby: components["schemas"]["Lobby"];
            members: components["schemas"]["Member"][] | null;
            /** Format: int64 */
            membership_generation: number;
            start_roster: components["schemas"]["StartRosterEntry"][] | null;
            start_rules: components["schemas"]["StartRules"];
            teams: components["schemas"]["Team"][] | null;
            /**
             * Format: int64
             * @description How many people watch the lobby with its join code, kicked watchers left out. Watchers take no seat. A change to it is a lobby change, announced with lobby.changed.
             */
            watcher_count: number;
        };
        StartInputBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/StartInputBody.json
             */
            readonly $schema?: string;
        };
        StartRosterEntry: {
            member_id: string;
            player_id: string;
            team_id: string;
            /** Format: int64 */
            team_order: number;
        };
        StartRules: {
            /** @description Starting fails with empty_teams while a team has no member. */
            every_team_needs_member: boolean;
        };
        Team: {
            id: string;
            name: string;
            /** Format: int64 */
            order: number;
        };
        TeamDefinition: {
            name: string;
        };
        WatchAccess: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/WatchAccess.json
             */
            readonly $schema?: string;
            /** @description The lobby's watch channel, watch:<game_key>:<lobby_id>, where its game publishes what watchers may see. The subscribe proxy allows it while you are a watcher the host hasn't kicked, or an active member. */
            channel: string;
            game_key: string;
            /** Format: uuid */
            lobby_id: string;
            /** @description True when you are an active member of the lobby. You get no watcher row and keep your member access, which includes the watch channel. */
            member: boolean;
        };
        WatchLobbyBody: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/WatchLobbyBody.json
             */
            readonly $schema?: string;
            game_key: string;
            join_code: string;
        };
        Watcher: {
            display_name: string;
            /**
             * Format: date-time
             * @description When they started watching.
             */
            joined_at: string;
            /**
             * Format: uuid
             * @description The watcher's player ID, for kicking them.
             */
            player_id: string;
        };
        WatcherList: {
            /**
             * Format: uri
             * @description A URL to the JSON Schema for this object.
             * @example https://example.com/WatcherList.json
             */
            readonly $schema?: string;
            /** @description The watchers the host hasn't kicked, in the order they started watching. */
            watchers: components["schemas"]["Watcher"][];
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
    getLobby: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
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
                    "application/json": components["schemas"]["Snapshot"];
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
    heartbeatLobby: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["HeartbeatBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["HeartbeatResult"];
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
    releaseLobbyLease: {
        parameters: {
            query: {
                generation: number;
            };
            header?: never;
            path: {
                game_key: string;
                lobby_id: string;
                client_instance_id: string;
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
    leaveLobby: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
                lobby_id: string;
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
    setLobbyReady: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ReadyBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Snapshot"];
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
    addLobbySeat: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["AddSeatBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Snapshot"];
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
    removeLobbySeat: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
                lobby_id: string;
                /** @description The seat's member ID. */
                seat_id: string;
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
                    "application/json": components["schemas"]["Snapshot"];
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
    changeLobbySettings: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ChangeSettingsBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Snapshot"];
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
    startLobby: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["StartInputBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Snapshot"];
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
    selectLobbyTeam: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
                lobby_id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SelectTeamBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Snapshot"];
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
    unwatchLobby: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
                lobby_id: string;
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
    listLobbyWatchers: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
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
                    "application/json": components["schemas"]["WatcherList"];
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
    kickLobbyWatcher: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                game_key: string;
                lobby_id: string;
                /** @description The watcher's player ID. */
                player_id: string;
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
    createLobby: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CreateLobbyBody"];
            };
        };
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Snapshot"];
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
    joinLobby: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["JoinLobbyBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Snapshot"];
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
    createLobbySession: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CreateSessionBody"];
            };
        };
        responses: {
            /** @description Created */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GuestSession"];
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
    watchLobby: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["WatchLobbyBody"];
            };
        };
        responses: {
            /** @description OK */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WatchAccess"];
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
}
