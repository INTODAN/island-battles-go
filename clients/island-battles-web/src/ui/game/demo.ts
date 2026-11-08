// The demo view: a saved game from a local file, played with no server, lobby or session. The
// file is read in the browser and uploaded nowhere.

import {
  type Demo,
  DemoFileError,
  describeDemo,
  readDemoFile,
} from "./demoFile";
import { LogBook } from "./logs";
import { GameScreen } from "./screen";

export interface DemoShowOptions {
  /** Defaults to "Back to lobby". */
  backLabel?: string;
  onBack: () => void;
}

/** Shows a demo that was read and checked already. */
export function mountDemo(
  container: HTMLElement,
  demo: Demo,
  options: DemoShowOptions,
): { dispose(): void } {
  return mountDemoSource(container, Promise.resolve(demo), options, "");
}

/** Reads the file, then shows it; a wrong file gets a message and the Back button. */
export function mountDemoFile(
  container: HTMLElement,
  file: File,
  options: DemoShowOptions,
): { dispose(): void } {
  return mountDemoSource(container, readDemoFile(file), options, file.name);
}

function mountDemoSource(
  container: HTMLElement,
  source: Promise<Demo>,
  options: DemoShowOptions,
  name: string,
): { dispose(): void } {
  const logs = new LogBook();
  let disposed = false;
  const screen = new GameScreen(container, {
    live: false,
    logs,
    onBack: options.onBack,
    ...(options.backLabel === undefined
      ? {}
      : { backLabel: options.backLabel }),
    onOpenFile: (file) => void open(file),
  });
  screen.setTitle(name ? `Demo · ${name}` : "Demo");

  function show(demo: Demo): void {
    logs.clear();
    logs.addAll(demo.logs);
    screen.setTitle(name ? `Demo · ${name}` : `Demo · game ${demo.game_id}`);
    screen.setSummary(describeDemo(demo));
    screen.setNotice(null);
    screen.start(demo.teams, demo.frames, demo.outcome);
    screen.hideOverlay();
  }

  async function open(file: File): Promise<void> {
    try {
      const demo = await readDemoFile(file);
      if (disposed) return;
      name = file.name;
      show(demo);
    } catch (error) {
      screen.setNotice(`${message(error)} The demo shown before stays open.`);
    }
  }

  screen.showLoading(name ? `Reading ${name}…` : "Opening the demo…");
  source.then(
    (demo) => {
      if (!disposed) show(demo);
    },
    (error) => {
      if (!disposed) screen.showFailure(message(error));
    },
  );

  return {
    dispose() {
      disposed = true;
      screen.dispose();
    },
  };
}

function message(error: unknown): string {
  return error instanceof DemoFileError
    ? error.message
    : "The file could not be read.";
}
