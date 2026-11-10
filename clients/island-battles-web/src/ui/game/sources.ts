// Where the live view gets a game from: a roster player's channel and reads, or a watcher's.
// The view itself (`live.ts`) is the same for both.

import { createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import type { Realtime, RealtimeProblem } from "../../realtime";
import type { Demo } from "./demoFile";
import type { GameOutcome, Generation } from "./history";
import type { GenerationLogs } from "./logs";
import { type DemoDownload, downloadDemo } from "./saveDemo";

/** One turn as it arrives live, from a player's or a watcher's state. */
export interface LiveTurn {
  game_id: number;
  generation: Generation;
  outcome?: GameOutcome | null;
}

export interface LiveSource {
  /** The screen's title before the game is known. */
  title: string;
  /** "Back to lobby" for a player; a watcher has no lobby to go back to. */
  backLabel?: string;
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
   * Reads one turn's logs. Absent for a watcher, who sees no logs until the game ends and
   * then gets every turn's logs with the history.
   */
  readLogs?(generationId: number): Promise<GenerationLogs>;
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

/** A roster player's game: the player channel, `/demo` and `/logs`. */
export function playerSource(realtime: Realtime, lobbyId: string): LiveSource {
  const gameApi = gameApiOf(realtime);
  return {
    title: "Game",
    problemScope: "game",
    follow: () => realtime.followGame(lobbyId),
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
  };
}

/**
 * A watcher's game, for someone outside the roster: the lobby's watch channel and
 * `/watch/demo`. Nothing here plays, heartbeats or applies a deadline.
 */
export function watchSource(
  realtime: Realtime,
  target: { lobbyId: string; channel: string },
): LiveSource {
  const gameApi = gameApiOf(realtime);
  return {
    title: "Watching",
    backLabel: "Stop watching",
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
  };
}
