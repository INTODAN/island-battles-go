/**
 * Where the client runs, and the two servers it talks to: the lobby, for
 * `/lobby/v1/*`, player tokens and the Centrifugo connection token, and the
 * game, for `/game/*` and `/generation/*`. They can be separate origins.
 *
 * The browser build is served by the game itself, at `/play/`, and reads both
 * URLs from its runtime config, `config.json` beside the page (`loadServerUrls`);
 * without one, both are the page's own origin. The desktop build (Tauri, see
 * `src-tauri/`) is a different build of the same code: `build.ts --desktop`
 * replaces `__DESKTOP_API_BASE__` with the game server it talks to and
 * `__DESKTOP_LOBBY_BASE__` with the lobby.
 */
declare const __DESKTOP_API_BASE__: string | undefined;
declare const __DESKTOP_LOBBY_BASE__: string | undefined;

/** The game server of a desktop build, or undefined in the browser. */
export const desktopBaseUrl: string | undefined =
  typeof __DESKTOP_API_BASE__ === "string" ? __DESKTOP_API_BASE__ : undefined;

/** The lobby of a desktop build, or undefined in the browser. */
export const desktopLobbyUrl: string | undefined =
  typeof __DESKTOP_LOBBY_BASE__ === "string"
    ? __DESKTOP_LOBBY_BASE__
    : undefined;

export const isDesktop = desktopBaseUrl !== undefined;

/** The browser build's runtime config: `config.json` beside the page. */
export interface RuntimeConfig {
  /** The lobby's URL, such as https://lobby.example.org. */
  lobby_url?: string;
  /** The game's URL, such as https://island-battles.example.org. */
  game_url?: string;
}

let configured: { lobby?: string; game?: string } = {};

/** An http(s) URL as an origin without a trailing slash, or undefined. */
function serverUrl(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:"
      ? url.origin
      : undefined;
  } catch {
    return undefined;
  }
}

/** Uses a runtime config's URLs; an absent or invalid one keeps the default. Tests pass `{}` to undo it. */
export function setServerUrls(config: RuntimeConfig): void {
  const lobby = serverUrl(config.lobby_url);
  const game = serverUrl(config.game_url);
  configured = {
    ...(lobby === undefined ? {} : { lobby }),
    ...(game === undefined ? {} : { game }),
  };
}

/**
 * Reads the browser build's runtime config, `config.json` beside the page,
 * before the app makes its API clients. A missing or unreadable file leaves
 * both servers at the page's own origin, as a deployment that serves the lobby
 * with the game has them. The desktop build has its URLs built in.
 */
export async function loadServerUrls(
  fetchConfig: typeof fetch = globalThis.fetch,
): Promise<void> {
  if (isDesktop || globalThis.document === undefined) return;
  try {
    const response = await fetchConfig(
      new URL("config.json", document.baseURI),
      { headers: { Accept: "application/json" }, cache: "no-store" },
    );
    if (!response.ok) return;
    const config: unknown = await response.json();
    if (config !== null && typeof config === "object") {
      setServerUrls(config as RuntimeConfig);
    }
  } catch {
    // Without a config, the page's origin serves both.
  }
}

/** Where the game's API calls go: the runtime config's game, the desktop build's, else the page's own origin. */
export const gameBaseUrl = (): string =>
  configured.game ?? desktopBaseUrl ?? globalThis.location?.origin ?? "";

/** Where the lobby's API calls go: the runtime config's lobby, the desktop build's, else the page's own origin. */
export const lobbyBaseUrl = (): string =>
  configured.lobby ??
  desktopLobbyUrl ??
  desktopBaseUrl ??
  globalThis.location?.origin ??
  "";

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

let desktopFetch: typeof fetch | undefined;

/** Desktop build only (or a test, which passes undefined to undo it): what `platformFetch` sends requests through (see `src/desktop.ts`). */
export function setDesktopFetch(replacement: typeof fetch | undefined): void {
  desktopFetch = replacement;
}

/**
 * The `fetch` the client's own calls use: the desktop build's HTTP plugin once
 * installed, else the page's `fetch`, looked up per call so a later replacement
 * (tests) is honoured. Not `globalThis.fetch` itself: Tauri's own IPC calls it,
 * so replacing it with something that uses IPC never returns.
 */
export const platformFetch = ((...args: Parameters<typeof fetch>) =>
  (desktopFetch ?? globalThis.fetch)(...args)) as typeof fetch;

/** Resolves a path such as `/game/...` against the desktop build's game server. */
export function resolveAgainstBase(
  input: Parameters<typeof fetch>[0],
  base: string,
): Parameters<typeof fetch>[0] {
  return typeof input === "string" && input.startsWith("/")
    ? new URL(input, base).toString()
    : input;
}
