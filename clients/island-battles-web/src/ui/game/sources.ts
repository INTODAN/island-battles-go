// Where the live view gets a game from: a roster player's channel and reads, or a watcher's.
// The view itself (`live.ts`) is the same for both.

import { createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import type { BotChoice, RelayedBot } from "../../bots/kinds";
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
  /** A roster player's team and the bot that plays their seat; a watcher's turns have neither. */
  team_color?: number;
  bot?: BotChoice;
  /** The localhost bot seats that the player added as the host, which only they relay. */
  relayed_bots?: RelayedBot[];
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
  };
}

/**
 * A watcher's game, for someone outside the roster: the lobby's watch channel, `/watch/demo`
 * and `/watch/logs`. Nothing here plays, heartbeats or applies a deadline.
 */
export function watchSource(
  realtime: Realtime,
  target: { lobbyId: string; channel: string },
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
  };
}
