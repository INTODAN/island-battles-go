import index from "./index.html";
import { backendOrigin, isProxiedPath } from "./proxy";

// The Centrifugo WebSocket is not proxied: the browser connects straight to
// :8000, whose allowed origins already include this server.
const server = Bun.serve({
  port: 3000,
  development: { hmr: true, console: true },
  routes: { "/": index },
  async fetch(request) {
    const url = new URL(request.url);
    if (!isProxiedPath(url.pathname)) {
      return new Response("Not found", { status: 404 });
    }
    return fetch(
      new Request(backendOrigin + url.pathname + url.search, request),
    );
  },
});

console.log(`Island Battles web client: ${server.url}`);
