// The game view. `mountGameView` shows a lobby's game live to a roster player;
// `mountWatchView` shows it read-only to a watcher outside the roster; `mountDemoView` opens a
// demo file. They are entry points for the app shell (#98), which supplies the container and
// what the back button does.

import type { Realtime } from "../../realtime";
import { mountDemo, mountDemoFile } from "./demo";
import { mountLive } from "./live";
import { type LiveSource, playerSource, watchSource } from "./sources";

export { DemoFileError, readDemoFile } from "./demoFile";
export type { LiveSource } from "./sources";

export interface GameViewOptions {
  lobbyId: string;
  /** The lobby round whose game to show; the lobby plays one game per round. */
  round: number;
  realtime: Realtime;
  onBack: () => void;
}

export interface WatchViewOptions {
  lobbyId: string;
  /** The watch channel `POST /lobby/v1/watches` returned. */
  channel: string;
  realtime: Realtime;
  onBack: () => void;
}

export interface DemoViewOptions {
  file: File;
  onBack: () => void;
}

/**
 * Shows the game of a lobby round in `container`: the whole history, then each turn live. The
 * caller signs in and the view follows the game on `realtime` until `dispose()`, which also
 * stops following it. Opening a demo file from the view shows the demo in its place, with a
 * button back to the game.
 */
export function mountGameView(
  container: HTMLElement,
  options: GameViewOptions,
): { dispose(): void } {
  return mountSource(
    container,
    options.realtime,
    playerSource(options.realtime, options.lobbyId, options.round),
    options.onBack,
  );
}

/**
 * Shows a watched game read-only: the watch channel and the watcher reads, with no lease,
 * heartbeat, relay or deadline call. It says so when the host kicks the watcher.
 */
export function mountWatchView(
  container: HTMLElement,
  options: WatchViewOptions,
): { dispose(): void } {
  return mountSource(
    container,
    options.realtime,
    watchSource(options.realtime, {
      lobbyId: options.lobbyId,
      channel: options.channel,
    }),
    options.onBack,
  );
}

function mountSource(
  container: HTMLElement,
  realtime: Realtime,
  source: LiveSource,
  onBack: () => void,
): { dispose(): void } {
  let current: { dispose(): void } | undefined;
  let disposed = false;

  const showLive = () => {
    current?.dispose();
    current = mountLive(container, {
      realtime,
      source,
      onBack,
      onDemo: (demo) => {
        current?.dispose();
        current = mountDemo(container, demo, {
          backLabel: "Back to game",
          onBack: showLive,
        });
      },
    });
  };
  showLive();

  return {
    dispose() {
      if (disposed) return;
      disposed = true;
      current?.dispose();
      current = undefined;
    },
  };
}

/** Shows a demo file in `container`, with no server. Nothing is uploaded. */
export function mountDemoView(
  container: HTMLElement,
  options: DemoViewOptions,
): { dispose(): void } {
  return mountDemoFile(container, options.file, { onBack: options.onBack });
}
