import { lobbyOrigin } from "./proxy";

/** The game's and the lobby's URLs that a desktop build talks to. */
export interface DesktopServers {
  /** The game, for `/game/*` and `/generation/*`. */
  game: string;
  /** The lobby, for `/lobby/v1/*`, player tokens and the Centrifugo connection token. */
  lobby: string;
}

/**
 * The servers `build.ts --desktop` builds in: the official game and its lobby,
 * on its own subdomain since #232. The window may reach only what
 * `src-tauri/tauri.conf.json` (CSP) and `src-tauri/capabilities/main.json`
 * (the HTTP plugin's scope) allow, so they list both, which
 * `desktopServers.test.ts` checks.
 */
export const productionServers: DesktopServers = {
  game: "https://island-battles.elidan.org",
  lobby: "https://lobby.elidan.org",
};

/**
 * The servers of `build.ts --desktop --dev`: the dev server (`bun run dev`),
 * which forwards the game's paths to the backend that `./scripts/dev.sh` runs,
 * and the lobby, which it runs on its own origin. `src-tauri/tauri.dev.conf.json`
 * and `src-tauri/capabilities/dev.json` allow them.
 */
export const devServers: DesktopServers = {
  game: "http://localhost:3000",
  lobby: lobbyOrigin,
};
