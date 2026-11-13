import index from "./index.html";
import { lobbyOrigin, proxyOrigin } from "./proxy";

// The lobby and the backend run as separate processes (./scripts/dev.sh). This
// server forwards the game's paths to the backend (proxy.ts), as the game
// serves the client in production, and its config.json sends the client
// straight to the lobby, on its own origin, which allows this one
// (LOBBY_ALLOWED_ORIGINS). ISLAND_BATTLES_WEB_LOBBY_URL names another lobby.
// The Centrifugo WebSocket is not proxied: the browser connects straight to
// :8000, whose allowed origins already include this server.
const runtimeConfig = {
  lobby_url: process.env.ISLAND_BATTLES_WEB_LOBBY_URL || lobbyOrigin,
};
const server = Bun.serve({
  port: 3000,
  development: { hmr: true, console: true },
  routes: { "/": index, "/config.json": Response.json(runtimeConfig) },
  async fetch(request) {
    const url = new URL(request.url);
    // Saved games for the 3D board's dev page, /?fixture=<file>.
    if (url.pathname.startsWith("/fixtures/")) {
      const name = decodeURIComponent(url.pathname.slice("/fixtures/".length));
      if (!/^[\w.-]+\.json$/.test(name)) {
        return new Response("Not found", { status: 404 });
      }
      const file = Bun.file(`${import.meta.dir}/test/fixtures/${name}`);
      return (await file.exists())
        ? new Response(file)
        : new Response("Not found", { status: 404 });
    }
    const origin = proxyOrigin(url.pathname);
    if (origin === undefined) {
      return new Response("Not found", { status: 404 });
    }
    const response = await fetch(
      new Request(origin + url.pathname + url.search, request),
    );
    // Bun's fetch unpacks a gzipped body (the demo is sent gzipped), so passing the headers on
    // unchanged would make the browser unpack it a second time and fail.
    const headers = new Headers(response.headers);
    headers.delete("content-encoding");
    headers.delete("content-length");
    return new Response(response.body, {
      status: response.status,
      statusText: response.statusText,
      headers,
    });
  },
});

console.log(`Island Battles web client: ${server.url}`);
