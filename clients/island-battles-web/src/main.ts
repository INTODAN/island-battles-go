import { createRealtime, type Realtime } from "./realtime";

declare global {
  interface Window {
    /** The session behind the page, for browser tests. */
    islandBattles?: { realtime: Realtime };
  }
}

// The desktop build sends its requests through Tauri's HTTP plugin (src/desktop.ts).
// The define folds this to false in the browser build, which then carries no desktop code.
declare const __DESKTOP_API_BASE__: string | undefined;
if (typeof __DESKTOP_API_BASE__ === "string") {
  const { installDesktopFetch } = await import("./desktop");
  installDesktopFetch();
}

const canvas = document.getElementById("scene");
if (!(canvas instanceof HTMLCanvasElement)) {
  throw new Error("index.html has no #scene canvas");
}
const root = document.getElementById("app");
if (!root) {
  throw new Error("index.html has no #app element");
}

// The dev page for the 3D board: /?fixture=<file> draws a saved game from test/fixtures, no server.
const fixture = new URLSearchParams(location.search).get("fixture");

// Dev entry points for the game view (src/ui/game/devpage.ts): they run beside the app shell.
const gameLobby = new URLSearchParams(location.search).get("game");
const demoFile = new URLSearchParams(location.search).get("demo");

if (gameLobby !== null) {
  const { startGamePage } = await import("./ui/game/devpage");
  startGamePage(gameLobby);
} else if (demoFile !== null) {
  const { startDemoPage } = await import("./ui/game/devpage");
  await startDemoPage(demoFile);
} else if (fixture !== null) {
  const { startFixturePage } = await import("./scene/devpage");
  await startFixturePage(canvas, fixture);
} else {
  // The app shell: the lobby screens over an ocean, then the game view.
  const { mountLobbyApp } = await import("./ui/lobby/app");
  const realtime = createRealtime();
  window.islandBattles = { realtime };
  mountLobbyApp({ root, canvas, realtime });
}
