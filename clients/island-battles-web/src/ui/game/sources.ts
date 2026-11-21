// Where the live view gets a game from: a roster player's channel and reads, or a watcher's.
// The view itself (`live.ts`) is the same for both.

import { createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import type { BotChoice, RelayedBot } from "../../bots/kinds";
import type { Realtime, RealtimeProblem } from "../../realtime";
import type { Demo } from "./demoFile";
import type { Departure, GameOutcome, Generation } from "./history";
import type { GenerationLogs } from "./logs";
import { type ResignResult, requestResign } from "./resign";
import { type DemoDownload, downloadDemo } from "./saveDemo";

/** One turn as it arrives live, from a player's or a watcher's state. */
export interface LiveTurn {
  game_id: number;
  generation: Generation;
  outcome?: GameOutcome | null;
  /** A roster player's team and the bot that plays their seat; a watcher's turns have neither. */
  team_color?: number;
  bot?: BotChoice;
  /** The localhost bot seats that the player added as the host, which only they relay. */
  relayed_bots?: RelayedBot[];
  /** The teams out of the game so far (resigned or left), as the state lists them. */
  departures?: Departure[];
  /** The game's turn pace in a real-time game, null when it waits for every player. */
  turn_pace_milliseconds?: number | null;
  /**
   * Whether the game is rated, from a roster player's state; a watcher's state doesn't say, and
   * learns it from the outcome's `ratings` at the end.
   */
  rated?: boolean;
}

export interface LiveSource {
  /** The screen's title before the game is known. */
  title: string;
  /** "Back to lobby" for a player; a watcher has no lobby to go back to. */
  backLabel?: string;
  /** A watcher belongs to no team, so they see no logs until the game ends. */
  watching: boolean;
  /** The realtime problem scope that ends this source. */
  problemScope: RealtimeProblem["scope"];
  /** Subscribes to the game's channel. */
  follow(): void;
  /** Stops following. */
  leave(): void;
  current(): LiveTurn | null;
  subscribe(listener: (turn: LiveTurn | null) => void): () => void;
  /** The history so far, in the demo format. */
  readHistory(): Promise<Demo>;
  /** The demo file of the game so far, ready to save. */
  saveDemo(): Promise<DemoDownload>;
  /**
   * Reads one turn's logs: a player's own team's while the game runs, and every team's once
   * it has ended. A watcher's read has no lines until then.
   */
  readLogs(generationId: number): Promise<GenerationLogs>;
  /** The lobby's join code, for "Ask your AI agent", when it is known. */
  lobbyCode(): string | undefined;
  /**
   * Resigns the player's team and installs the server's answer. Only a roster player's source
   * has it: a watcher cannot resign.
   */
  resign?(): Promise<ResignResult>;
}

function required<T>(data: T | undefined): T {
  if (data === undefined) {
    throw new ApiError({ kind: "http", status: 0, detail: "empty" });
  }
  return data;
}

const gameApiOf = (realtime: Realtime) =>
  createApiClients({
    getAccessToken: () => realtime.session.get()?.accessToken,
  }).gameApi;

/**
 * A roster player's game of a lobby round: the player channel, `/demo` and `/logs`. Those
 * routes serve the lobby's newest game. The app mounts the view once this round's game exists
 * and replaces it as soon as the next round starts, so they serve this round's game while it
 * shows.
 */
export function playerSource(
  realtime: Realtime,
  lobbyId: string,
  round: number,
): LiveSource {
  const gameApi = gameApiOf(realtime);
  return {
    title: "Game",
    watching: false,
    problemScope: "game",
    follow: () => realtime.followGame(lobbyId, round),
    leave: () => realtime.leaveGame(),
    current: () => realtime.game.get(),
    subscribe: (listener) => realtime.game.subscribe(listener),
    async readHistory() {
      const { data } = await gameApi.GET("/game/from-lobby/{lobby_id}/demo", {
        params: { path: { lobby_id: lobbyId } },
      });
      return required(data);
    },
    saveDemo: () => downloadDemo(gameApi, lobbyId, false),
    async readLogs(generationId) {
      const { data } = await gameApi.GET("/game/from-lobby/{lobby_id}/logs", {
        params: {
          path: { lobby_id: lobbyId },
          query: { generation_id: generationId },
        },
      });
      return required(data);
    },
    lobbyCode() {
      const snapshot = realtime.lobby.get();
      return snapshot?.lobby.id === lobbyId
        ? snapshot.lobby.join_code
        : undefined;
    },
    async resign() {
      const result = await requestResign({
        call: async () => {
          const { data } = await gameApi.POST(
            "/game/from-lobby/{lobby_id}/resign",
            { params: { path: { lobby_id: lobbyId } } },
          );
          return required(data);
        },
        sleep: (milliseconds) =>
          new Promise((resolve) => setTimeout(resolve, milliseconds)),
      });
      // The answer is the next state when the turn played in the request; otherwise the view
      // learns of the team's exit from the turn that plays it.
      if (result.kind === "resigned") realtime.adoptGame(result.state);
      // The game ended or the team is out already: read what the view hasn't received.
      if (result.kind === "already") realtime.refreshGame();
      return result;
    },
  };
}

/**
 * A watcher's game, for someone outside the roster: the lobby's watch channel, `/watch/demo`
 * and `/watch/logs`. Nothing here plays, heartbeats or applies a deadline.
 */
export function watchSource(
  realtime: Realtime,
  target: { lobbyId: string; channel: string; joinCode?: string },
): LiveSource {
  const gameApi = gameApiOf(realtime);
  return {
    title: "Watching",
    backLabel: "Stop watching",
    watching: true,
    problemScope: "watch",
    follow: () => realtime.followWatch(target),
    leave: () => realtime.leaveWatch(),
    current: () => realtime.watch.get(),
    subscribe: (listener) => realtime.watch.subscribe(listener),
    async readHistory() {
      const { data } = await gameApi.GET(
        "/game/from-lobby/{lobby_id}/watch/demo",
        { params: { path: { lobby_id: target.lobbyId } } },
      );
      return required(data);
    },
    saveDemo: () => downloadDemo(gameApi, target.lobbyId, true),
    async readLogs(generationId) {
      const { data } = await gameApi.GET(
        "/game/from-lobby/{lobby_id}/watch/logs",
        {
          params: {
            path: { lobby_id: target.lobbyId },
            query: { generation_id: generationId },
          },
        },
      );
      return required(data);
    },
    lobbyCode: () => target.joinCode,
  };
}
