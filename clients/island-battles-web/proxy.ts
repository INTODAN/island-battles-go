/** Where the backend (`./scripts/dev.sh`) listens. */
export const backendOrigin = "http://localhost:8888";

/** Paths the dev server forwards to the backend, so the browser stays on one origin. */
const proxiedPrefixes = ["/lobby/v1/", "/game/", "/generation/", "/map/"];
const proxiedPaths = ["/healthz", "/openapi.json"];

export function isProxiedPath(pathname: string): boolean {
  return (
    proxiedPaths.includes(pathname) ||
    proxiedPrefixes.some((prefix) => pathname.startsWith(prefix))
  );
}
