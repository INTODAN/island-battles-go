import { createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import type { components as GameComponents } from "../../api/generated/game";
import type { components as LobbyComponents } from "../../api/generated/lobby";
import { platformFetch } from "../../platform";
import type { Realtime, Snapshot, WatchAccess } from "../../realtime";
import {
  type ChangeSettingsBody,
  type CreateLobbyBody,
  gameKey,
  type MapTemplate,
} from "./form";

export type BotChoice = GameComponents["schemas"]["BotChoice"];
/** The bot in a person's member settings, `{"bot": ...}`. */
export type MemberBot = GameComponents["schemas"]["MemberBot"];
/** A bot seat's `seat_settings`: one of `bot_level`, `bot_url` and `bot_package_id`. */
export type BotSeatSettings = GameComponents["schemas"]["BotSeatSettings"];
export type DefaultBot = GameComponents["schemas"]["DefaultBot"];
export type BotPackage = GameComponents["schemas"]["BotPackage"];
export type BotPackageUpload = GameComponents["schemas"]["BotPackageUpload"];
export type BotUploadToken = GameComponents["schemas"]["BotUploadToken"];
export type AddSeatBody = LobbyComponents["schemas"]["AddSeatBody"];
export type Watcher = LobbyComponents["schemas"]["Watcher"];
export type MapTemplateSummary =
  GameComponents["schemas"]["MapTemplateSummary"];

export interface MapValidation {
  valid: boolean;
  errors: { location: string; message: string }[];
}

/** Runs `attempt` again, unchanged, after a transport failure: up to `tries` times in all. */
export async function retryTransport<T>(
  attempt: () => Promise<T>,
  options: {
    tries?: number;
    delayMs?: number;
    sleep?: (ms: number) => Promise<void>;
  } = {},
): Promise<T> {
  const tries = options.tries ?? 3;
  const sleep =
    options.sleep ??
    ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)));
  for (let made = 1; ; made++) {
    try {
      return await attempt();
    } catch (error) {
      if (
        !(error instanceof ApiError) ||
        error.kind !== "transport" ||
        made >= tries
      ) {
        throw error;
      }
      await sleep((options.delayMs ?? 1000) * made);
    }
  }
}

function required<T>(data: T | undefined): T {
  if (data === undefined) {
    throw new ApiError({ kind: "http", status: 0, detail: "empty response" });
  }
  return data;
}

/** The calls the lobby screens make. Their results reach the screens through the realtime stores. */
export function createLobbyActions(options: {
  realtime: Realtime;
  clientInstanceId: string;
  fetch?: typeof fetch;
}) {
  const { realtime, clientInstanceId } = options;
  const doFetch = options.fetch ?? platformFetch;
  const { lobbyApi, gameApi } = createApiClients({
    getAccessToken: () => realtime.session.get()?.accessToken,
    fetch: doFetch,
  });

  const room = () => {
    const snapshot = realtime.lobby.get();
    if (!snapshot) {
      throw new ApiError({
        kind: "problem",
        status: 404,
        code: "not_found",
        detail: "You are not in a lobby.",
      });
    }
    return {
      snapshot,
      path: {
        game_key: snapshot.lobby.game_key,
        lobby_id: snapshot.lobby.id,
      },
    };
  };

  /** A snapshot an HTTP call returned goes to the session, which shows it. */
  const adopt = (snapshot: Snapshot | undefined) => {
    realtime.adoptSnapshot(required(snapshot));
  };

  return {
    async create(
      body: Omit<CreateLobbyBody, "client_instance_id" | "game_key">,
    ) {
      const { data } = await retryTransport(() =>
        lobbyApi.POST("/lobby/v1/lobbies", {
          body: {
            ...body,
            game_key: gameKey,
            client_instance_id: clientInstanceId,
          },
        }),
      );
      adopt(data);
    },

    async join(joinCode: string) {
      const { data } = await lobbyApi.POST("/lobby/v1/memberships", {
        body: {
          game_key: gameKey,
          join_code: joinCode,
          client_instance_id: clientInstanceId,
        },
      });
      adopt(data);
    },

    async selectTeam(teamId: string | null) {
      const { path } = room();
      const { data } = await lobbyApi.PUT(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/team",
        { params: { path }, body: { team_id: teamId } },
      );
      adopt(data);
    },

    async setReady(ready: boolean) {
      const { path } = room();
      const { data } = await lobbyApi.PUT(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/ready",
        { params: { path }, body: { ready } },
      );
      adopt(data);
    },

    async start() {
      const { path } = room();
      const { data } = await lobbyApi.POST(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/start",
        { params: { path }, body: {} },
      );
      adopt(data);
    },

    /**
     * Brings the lobby back to waiting once its game has ended, for the next
     * game. Calling it again, or after another member did, changes nothing.
     */
    async reopen() {
      const { path } = room();
      const { data } = await lobbyApi.POST(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/reopen",
        { params: { path } },
      );
      adopt(data);
    },

    /** The host replaces the waiting lobby's map, turn limit and teams. */
    async changeSettings(body: ChangeSettingsBody) {
      const { path } = room();
      const { data } = await lobbyApi.PUT(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/settings",
        { params: { path }, body },
      );
      adopt(data);
    },

    /** Adds a bot seat; the body carries its request ID, so a resend after a lost reply adds nothing twice. */
    async addSeat(body: AddSeatBody) {
      const { path } = room();
      const { data } = await lobbyApi.POST(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/seats",
        { params: { path }, body },
      );
      adopt(data);
    },

    async removeSeat(seatId: string) {
      const { path } = room();
      const { data } = await lobbyApi.DELETE(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/seats/{seat_id}",
        { params: { path: { ...path, seat_id: seatId } } },
      );
      adopt(data);
    },

    async heartbeat() {
      const { path, snapshot } = room();
      await lobbyApi.POST(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/heartbeat",
        {
          params: { path },
          body: {
            client_instance_id: clientInstanceId,
            membership_generation: snapshot.membership_generation,
          },
        },
      );
    },

    async leave() {
      const { path } = room();
      await lobbyApi.DELETE(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/membership",
        { params: { path } },
      );
    },

    /** Watches a lobby's game with its join code, without taking a seat. */
    async watch(joinCode: string): Promise<WatchAccess> {
      const { data } = await retryTransport(() =>
        lobbyApi.POST("/lobby/v1/watches", {
          body: { game_key: gameKey, join_code: joinCode },
        }),
      );
      return required(data);
    },

    async unwatch(lobbyId: string) {
      await lobbyApi.DELETE(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/watch",
        { params: { path: { game_key: gameKey, lobby_id: lobbyId } } },
      );
    },

    /** The lobby's watchers, for its members. */
    async listWatchers(): Promise<Watcher[]> {
      const { path } = room();
      const { data } = await lobbyApi.GET(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/watchers",
        { params: { path } },
      );
      return required(data).watchers;
    },

    /** The host removes a watcher, who can't watch again with the code. */
    async kickWatcher(playerId: string) {
      const { path } = room();
      await lobbyApi.DELETE(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/watchers/{player_id}",
        { params: { path: { ...path, player_id: playerId } } },
      );
    },

    /**
     * Saves your bot in your lobby member settings, `{"bot": ...}`, while the
     * lobby waits. A new bot clears your ready; every member sees it in the
     * snapshot.
     */
    async chooseBot(choice: MemberBot) {
      const { path } = room();
      const { data } = await lobbyApi.PUT(
        "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/member-settings",
        { params: { path }, body: { member_settings: { bot: choice } } },
      );
      adopt(data);
    },

    /** The lobby's bot packages, newest first. */
    async listBotPackages(): Promise<BotPackage[]> {
      const { path } = room();
      const { data } = await gameApi.GET(
        "/game/lobbies/{lobby_id}/bot-packages",
        { params: { path: { lobby_id: path.lobby_id } } },
      );
      return required(data).packages;
    },

    /** Starts an upload: a pending package and its presigned POST. */
    async startBotPackageUpload(fileName: string): Promise<BotPackageUpload> {
      const { path } = room();
      const { data } = await gameApi.POST(
        "/game/lobbies/{lobby_id}/bot-packages",
        {
          params: { path: { lobby_id: path.lobby_id } },
          body: { file_name: fileName },
        },
      );
      return required(data);
    },

    /** Checks an uploaded zip; a bad package rejects with the game's reason. */
    async completeBotPackageUpload(packageId: string): Promise<BotPackage> {
      const { path } = room();
      const { data } = await gameApi.POST(
        "/game/lobbies/{lobby_id}/bot-packages/{package_id}/complete",
        {
          params: { path: { lobby_id: path.lobby_id, package_id: packageId } },
        },
      );
      return required(data);
    },

    /** Deletes your package. */
    async deleteBotPackage(packageId: string) {
      const { path } = room();
      await gameApi.DELETE(
        "/game/lobbies/{lobby_id}/bot-packages/{package_id}",
        {
          params: { path: { lobby_id: path.lobby_id, package_id: packageId } },
        },
      );
    },

    /** An upload token for the command line, valid for 15 minutes. */
    async botUploadToken(): Promise<BotUploadToken> {
      const { path } = room();
      const { data } = await gameApi.POST(
        "/game/lobbies/{lobby_id}/bot-packages/upload-token",
        { params: { path: { lobby_id: path.lobby_id } } },
      );
      return required(data);
    },

    /**
     * The default bots, and whether the server offers hosted and uploaded
     * bots (`ISLAND_BATTLES_SERVER_BOTS`).
     */
    async botOptions(): Promise<{ bots: DefaultBot[]; serverBots: boolean }> {
      const { data } = await gameApi.GET("/bots/defaults");
      const body = required(data);
      return { bots: body.bots, serverBots: body.server_bots };
    },

    async mapTemplates(): Promise<MapTemplateSummary[]> {
      const { data } = await gameApi.GET("/map/templates");
      return required(data).maps;
    },

    /** Asks the server to check a map file, which it does without storing it. */
    async validateMap(map: MapTemplate): Promise<MapValidation> {
      let response: Response;
      try {
        response = await doFetch("/map/validate", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Accept: "application/json",
          },
          body: JSON.stringify(map),
        });
      } catch (cause) {
        throw new ApiError({
          kind: "transport",
          status: 0,
          detail: "could not reach the map checker",
          cause,
        });
      }
      if (!response.ok) {
        throw new ApiError({ kind: "http", status: response.status });
      }
      const body = (await response.json()) as Partial<MapValidation>;
      return {
        valid: body.valid === true,
        errors: (body.errors ?? []).map((item) => ({
          location: item.location ?? "",
          message: item.message ?? "",
        })),
      };
    },
  };
}

export type LobbyActions = ReturnType<typeof createLobbyActions>;
