// The game view. `mountGameView` shows a lobby's game live; `mountDemoView` opens a demo file.
// Both are entry points for the app shell (#98), which supplies the container and what
// "Back to lobby" does. #103 reuses the same screen for spectators.

import type { Realtime } from "../../realtime";
import { mountDemo, mountDemoFile } from "./demo";
import { mountLive } from "./live";

export { DemoFileError, readDemo, readDemoFile } from "./demoFile";

export interface GameViewOptions {
  lobbyId: string;
  realtime: Realtime;
  onBack: () => void;
}

export interface DemoViewOptions {
  file: File;
  onBack: () => void;
}

/**
 * Shows the game of `lobbyId` in `container`: the whole history, then each turn live. The
 * caller signs in and the view follows the game on `realtime` until `dispose()`, which also
 * stops following it. Opening a demo file from the view shows the demo in its place, with a
 * button back to the game.
 */
export function mountGameView(
  container: HTMLElement,
  options: GameViewOptions,
): { dispose(): void } {
  let current: { dispose(): void } | undefined;
  let disposed = false;

  const showLive = () => {
    current?.dispose();
    current = mountLive(container, {
      lobbyId: options.lobbyId,
      realtime: options.realtime,
      onBack: options.onBack,
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
