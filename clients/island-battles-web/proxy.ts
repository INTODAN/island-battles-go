/** Where the backend (`./scripts/dev.sh`) listens. */
export const backendOrigin = "http://localhost:8888";

/**
 * Where the lobby, which `./scripts/dev.sh` runs as its own process, listens.
 * The client calls it there, on its own origin, as it calls a lobby on its own
 * site in production, so cross-origin mistakes show up locally: the dev
 * server forwards nothing to it, and its `config.json` names it.
 */
export const lobbyOrigin = "http://localhost:8890";

/** Paths the dev server forwards to the backend. */
const backendPrefixes = ["/game/", "/generation/", "/map/", "/bots/"];
const backendPaths = ["/healthz", "/openapi.json"];

/**
 * The origin the dev server forwards a path to, so the client and the game are on one origin, as
 * the game serves the client at /play/ in production, or undefined for the client's own paths.
 */
export function proxyOrigin(pathname: string): string | undefined {
  if (
    backendPaths.includes(pathname) ||
    backendPrefixes.some((prefix) => pathname.startsWith(prefix))
  ) {
    return backendOrigin;
  }
  return undefined;
}
