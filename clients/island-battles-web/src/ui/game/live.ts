// The live game view: the history of a game, then each new turn as it arrives. A roster
// player and a watcher outside the roster see the same view, from different sources
// (`sources.ts`).
//
// Order matters. The view subscribes to the game's channel first (`source.follow`), then loads
// the history, then merges the live generations into it by `id`, ignoring ones it already
// holds. A turn that arrives while the history loads is kept and merged after it.

import { ApiError } from "../../api/errors";
import type { Realtime } from "../../realtime";
import type { GameController } from "./controller";
import { type Demo, DemoFileError, readDemoFile } from "./demoFile";
import { mergeGenerations } from "./history";
import { LogBook } from "./logs";
import { saveTextFile } from "./saveDemo";
import { GameScreen } from "./screen";
import type { LiveSource, LiveTurn } from "./sources";

export interface LiveOptions {
  realtime: Realtime;
  source: LiveSource;
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

/** Why a watcher's view stopped or could not load, in words for them. */
export function watchFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403) {
      return "The host removed you from this game's watchers.";
    }
    if (error.status === 404) {
      return "This game has not started yet, or it is gone. It appears here when its first turn is played.";
    }
  }
  return loadFailureText(error);
}

export function mountLive(
  container: HTMLElement,
  options: LiveOptions,
): { dispose(): void } {
  const { realtime, source } = options;
  const watching = source.readLogs === undefined;
  const failureText = watching ? watchFailureText : loadFailureText;
  const logs = new LogBook();
  const screen = new GameScreen(container, {
    live: true,
    watching,
    logs,
    onBack: options.onBack,
    ...(source.backLabel === undefined ? {} : { backLabel: source.backLabel }),
    onOpenFile: (file) => void openFile(file),
    onSaveDemo: async () => saveTextFile(await source.saveDemo()),
    requestLogs: (generationId, ended) => void fetchLogs(generationId, ended),
  });
  screen.setTitle(source.title);

  let disposed = false;
  let controller: GameController | undefined;
  let gameId: number | undefined;
  let teamColors: number[] = [];
  /** Turns that arrived before the history was loaded. */
  let pending: LiveTurn[] = [];
  let loading = false;
  let wasConnected = false;
  let resyncTimer: ReturnType<typeof setTimeout> | undefined;
  let endedResync = false;
  /** The host removed this watcher: no more turns arrive. */
  let stopped = false;
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
    if (!source.readLogs) {
      // A watcher sees no logs while the game runs: every team is hidden. Once it has ended,
      // the history carries every turn's logs, so it is read once more.
      if (!ended) {
        if (
          logs.add({
            generation_id: generationId,
            players: [],
            hidden_team_colors: teamColors,
          })
        ) {
          // The panel asked while drawing: draw again once it has finished.
          queueMicrotask(() => {
            if (!disposed) screen.refreshLogs();
          });
        }
      } else if (!endedResync && controller) {
        endedResync = true;
        void resync();
      }
      return;
    }
    const key = `${generationId}:${ended}`;
    if (requested.has(key)) return;
    requested.add(key);
    try {
      const data = await source.readLogs(generationId);
      if (disposed) return;
      if (logs.add(data)) screen.refreshLogs();
    } catch {
      // Try again later, not at once: the panel asks again whenever the view redraws.
      setTimeout(() => requested.delete(key), RESYNC_RETRY_MS);
    }
  }

  function installTurn(turn: LiveTurn): void {
    if (gameId === undefined) {
      pending.push(turn);
      return;
    }
    if (turn.game_id !== gameId) return;
    controller?.merge([turn.generation], turn.outcome ?? null);
  }

  async function load(): Promise<void> {
    if (loading || disposed) return;
    loading = true;
    screen.showLoading("Loading the game so far…");
    try {
      const demo = await source.readHistory();
      if (disposed) return;
      logs.addAll(demo.logs);
      const turns = [...pending];
      const held = source.current();
      if (held) turns.push(held);
      const live = turns.filter((turn) => turn.game_id === demo.game_id);
      const frames = mergeGenerations(
        demo.frames,
        live.map((turn) => turn.generation),
      );
      const outcome =
        demo.outcome ?? live.find((turn) => turn.outcome)?.outcome ?? null;
      pending = [];
      gameId = demo.game_id;
      teamColors = demo.teams.map((team) => team.color);
      screen.setTitle(
        watching ? `Watching game ${demo.game_id}` : `Game ${demo.game_id}`,
      );
      controller = screen.start(demo.teams, frames, outcome);
      endedResync = demo.outcome !== null;
      screen.hideOverlay();
    } catch (error) {
      if (disposed) return;
      if (watching && error instanceof ApiError && error.status === 404) {
        // Not started yet: the first turn on the channel loads it.
        screen.showLoading(watchFailureText(error));
        return;
      }
      screen.showFailure(failureText(error), () => void load());
    } finally {
      loading = false;
    }
  }

  /** After a reconnect the channel may have skipped turns: read the history again and merge it. */
  async function resync(): Promise<void> {
    clearTimeout(resyncTimer);
    try {
      const demo = await source.readHistory();
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
  // 1. Subscribe to the game's channel first.
  source.follow();
  const stops = [
    source.subscribe((turn) => {
      if (!turn) return;
      installTurn(turn);
      // A watcher may arrive before the game starts: its first turn loads the view.
      if (gameId === undefined && !loading) void load();
    }),
    realtime.connection.subscribe((state) => {
      screen.setConnection(stopped ? "stopped" : state);
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
        (problem.scope === source.problemScope || problem.scope === "session")
      ) {
        screen.setNotice(failureText(problem.error));
        if (problem.scope === "watch") {
          stopped = true;
          screen.setConnection("stopped");
        }
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
      source.leave();
      screen.dispose();
    },
  };
}
