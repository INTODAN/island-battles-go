// The live game view: the history of a game the player is on, then each new turn as it arrives.
//
// Order matters. The view subscribes to the player channel first (`followGame`), then loads the
// history from `/demo`, then merges the live generations into it by `id`, ignoring ones it
// already holds. A turn that arrives while the history loads is kept and merged after it.

import { createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import type { GenerationState, Realtime } from "../../realtime";
import type { GameController } from "./controller";
import { type Demo, DemoFileError, readDemoFile } from "./demoFile";
import { mergeGenerations } from "./history";
import { LogBook } from "./logs";
import { GameScreen } from "./screen";

export interface LiveOptions {
  lobbyId: string;
  realtime: Realtime;
  onBack: () => void;
  /** A demo file the player opened was valid: show it in place of this view. */
  onDemo: (demo: Demo) => void;
}

const RESYNC_RETRY_MS = 3000;

/** Why the history could not be read, in words for the player. */
export function loadFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 404) {
      return "This game is not available. Games are deleted a while after their last turn, and a game that has not started has no history yet.";
    }
    if (error.status === 403) {
      return "Your guest session has no place in this game.";
    }
    if (error.status === 401) return "Your session has ended. Sign in again.";
    return `The game could not be loaded: ${error.message}`;
  }
  return "The game could not be loaded.";
}

export function mountLive(
  container: HTMLElement,
  options: LiveOptions,
): { dispose(): void } {
  const { realtime, lobbyId } = options;
  const logs = new LogBook();
  const { gameApi } = createApiClients({
    getAccessToken: () => realtime.session.get()?.accessToken,
  });
  const screen = new GameScreen(container, {
    live: true,
    logs,
    onBack: options.onBack,
    onOpenFile: (file) => void openFile(file),
    requestLogs: (generationId, ended) => void fetchLogs(generationId, ended),
  });
  screen.setTitle("Game");

  let disposed = false;
  let controller: GameController | undefined;
  let gameId: number | undefined;
  /** States that arrived before the history was loaded. */
  let pending: GenerationState[] = [];
  let loading = false;
  let wasConnected = false;
  let resyncTimer: ReturnType<typeof setTimeout> | undefined;
  const requested = new Set<string>();

  async function openFile(file: File): Promise<void> {
    try {
      const demo = await readDemoFile(file);
      if (!disposed) options.onDemo(demo);
    } catch (error) {
      screen.setNotice(
        `${error instanceof DemoFileError ? error.message : "The file could not be read."} The game shown before stays open.`,
      );
    }
  }

  async function fetchLogs(generationId: number, ended: boolean) {
    const key = `${generationId}:${ended}`;
    if (requested.has(key)) return;
    requested.add(key);
    try {
      const { data } = await gameApi.GET("/game/from-lobby/{lobby_id}/logs", {
        params: {
          path: { lobby_id: lobbyId },
          query: { generation_id: generationId },
        },
      });
      if (disposed || !data) return;
      if (logs.add(data)) screen.refreshLogs();
    } catch {
      // Try again later, not at once: the panel asks again whenever the view redraws.
      setTimeout(() => requested.delete(key), RESYNC_RETRY_MS);
    }
  }

  /** The history read from the server. */
  async function readHistory(): Promise<Demo> {
    const { data } = await gameApi.GET("/game/from-lobby/{lobby_id}/demo", {
      params: { path: { lobby_id: lobbyId } },
    });
    if (!data) throw new ApiError({ kind: "http", status: 0, detail: "empty" });
    return data;
  }

  function installState(state: GenerationState): void {
    if (gameId === undefined) {
      pending.push(state);
      return;
    }
    if (state.game_id !== gameId) return;
    controller?.merge([state.generation], state.outcome ?? null);
  }

  async function load(): Promise<void> {
    if (loading || disposed) return;
    loading = true;
    screen.showLoading("Loading the game so far…");
    try {
      const demo = await readHistory();
      if (disposed) return;
      logs.addAll(demo.logs);
      const states = [...pending];
      const held = realtime.game.get();
      if (held) states.push(held);
      const live = states.filter((state) => state.game_id === demo.game_id);
      const frames = mergeGenerations(
        demo.frames,
        live.map((state) => state.generation),
      );
      const outcome =
        demo.outcome ?? live.find((state) => state.outcome)?.outcome ?? null;
      pending = [];
      gameId = demo.game_id;
      screen.setTitle(`Game ${demo.game_id}`);
      controller = screen.start(demo.teams, frames, outcome);
      screen.hideOverlay();
    } catch (error) {
      if (disposed) return;
      screen.showFailure(loadFailureText(error), () => void load());
    } finally {
      loading = false;
    }
  }

  /** After a reconnect the channel may have skipped turns: read the history again and merge it. */
  async function resync(): Promise<void> {
    clearTimeout(resyncTimer);
    try {
      const demo = await readHistory();
      if (disposed || !controller || demo.game_id !== gameId) return;
      logs.addAll(demo.logs);
      controller.merge(demo.frames, demo.outcome);
      screen.refreshLogs();
    } catch {
      if (!disposed && realtime.connection.get() === "connected") {
        resyncTimer = setTimeout(() => void resync(), RESYNC_RETRY_MS);
      }
    }
  }

  if (!realtime.session.get()) {
    screen.showFailure("Sign in to watch the game.");
    return {
      dispose() {
        disposed = true;
        screen.dispose();
      },
    };
  }
  // 1. Subscribe to the player channel first.
  realtime.followGame(lobbyId);
  const stops = [
    realtime.game.subscribe((state) => {
      if (state) installState(state);
    }),
    realtime.connection.subscribe((state) => {
      screen.setConnection(state);
      if (state === "connected") {
        if (controller && !wasConnected) void resync();
        wasConnected = true;
      } else {
        wasConnected = false;
        clearTimeout(resyncTimer);
      }
    }),
    realtime.problem.subscribe((problem) => {
      if (
        problem &&
        (problem.scope === "game" || problem.scope === "session")
      ) {
        screen.setNotice(loadFailureText(problem.error));
      }
    }),
  ];
  screen.setConnection(realtime.connection.get());
  wasConnected = realtime.connection.get() === "connected";
  // 2. Then load the history.
  void load();

  return {
    dispose() {
      if (disposed) return;
      disposed = true;
      clearTimeout(resyncTimer);
      for (const stop of stops) stop();
      realtime.leaveGame();
      screen.dispose();
    },
  };
}
