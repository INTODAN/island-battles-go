/** Where the backend (`./scripts/dev.sh`) listens. */
export const backendOrigin = "http://localhost:8888";

/** Where the lobby, which `./scripts/dev.sh` runs as its own process, listens. */
export const lobbyOrigin = "http://localhost:8890";

/** Paths the dev server forwards to the lobby: its API and its page, /lobby/ui/. */
const lobbyPrefixes = ["/lobby/"];

/** Paths the dev server forwards to the backend. */
const backendPrefixes = ["/game/", "/generation/", "/map/", "/bots/"];
const backendPaths = ["/healthz", "/openapi.json"];

/**
 * The origin the dev server forwards a path to, so the browser stays on one origin, or undefined
 * for the client's own paths.
 */
export function proxyOrigin(pathname: string): string | undefined {
  if (lobbyPrefixes.some((prefix) => pathname.startsWith(prefix))) {
    return lobbyOrigin;
  }
  if (
    backendPaths.includes(pathname) ||
    backendPrefixes.some((prefix) => pathname.startsWith(prefix))
  ) {
    return backendOrigin;
  }
  return undefined;
}
