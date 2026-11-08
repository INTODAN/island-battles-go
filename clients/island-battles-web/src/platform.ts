/**
 * Where the client runs. The browser build is served by the game itself, so it
 * talks to its own origin. The desktop build (Tauri, see `src-tauri/`) is a
 * different build of the same code: `build.ts --desktop` replaces
 * `__DESKTOP_API_BASE__` with the game server it talks to.
 */
declare const __DESKTOP_API_BASE__: string | undefined;

/** The game server of a desktop build, or undefined in the browser. */
export const desktopBaseUrl: string | undefined =
  typeof __DESKTOP_API_BASE__ === "string" ? __DESKTOP_API_BASE__ : undefined;

export const isDesktop = desktopBaseUrl !== undefined;

/** The server origin API calls go to: the desktop build's game, else the page's own origin. */
export const apiBaseUrl = (): string =>
  desktopBaseUrl ?? globalThis.location?.origin ?? "";

/**
 * Where a link that others open in a browser points: the web client of the
 * game server. A desktop window's own address (`tauri://localhost`) is no use
 * to anyone else.
 */
export const webClientUrl = (): string =>
  desktopBaseUrl === undefined
    ? `${location.origin}${location.pathname}`
    : `${desktopBaseUrl}/play/`;

/**
 * Storage for what must survive closing the app within the guest session's 24
 * hours. A browser tab keeps it in `sessionStorage` (a tab is one player); the
 * desktop app has one window and is closed and reopened, so it uses
 * `localStorage`, which the web view keeps in the app's data folder.
 */
export const tabStorage = (): Storage =>
  isDesktop ? globalThis.localStorage : globalThis.sessionStorage;

/** Resolves a path such as `/game/...` against the desktop build's game server. */
export function resolveAgainstBase(
  input: Parameters<typeof fetch>[0],
  base: string,
): Parameters<typeof fetch>[0] {
  return typeof input === "string" && input.startsWith("/")
    ? new URL(input, base).toString()
    : input;
}
