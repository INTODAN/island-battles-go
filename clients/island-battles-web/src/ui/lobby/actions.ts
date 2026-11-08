import { createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import type { components as GameComponents } from "../../api/generated/game";
import type { Realtime, Snapshot } from "../../realtime";
import { type CreateLobbyBody, gameKey, type MapTemplate } from "./form";

export type BotChoice = GameComponents["schemas"]["BotChoice"];
export type LobbyBots = GameComponents["schemas"]["LobbyBots"];
export type DefaultBot = GameComponents["schemas"]["DefaultBot"];
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
  const doFetch =
    options.fetch ??
    (((...args: Parameters<typeof fetch>) =>
      globalThis.fetch(...args)) as typeof fetch);
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

    async chooseBot(choice: BotChoice): Promise<LobbyBots> {
      const { path } = room();
      const { data } = await gameApi.PUT("/game/lobbies/{lobby_id}/bot", {
        params: { path: { lobby_id: path.lobby_id } },
        body: choice,
      });
      return required(data);
    },

    async listBots(lobbyId: string): Promise<LobbyBots> {
      const { data } = await gameApi.GET("/game/lobbies/{lobby_id}/bots", {
        params: { path: { lobby_id: lobbyId } },
      });
      return required(data);
    },

    async defaultBots(): Promise<DefaultBot[]> {
      const { data } = await gameApi.GET("/bots/defaults");
      return required(data).bots;
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
