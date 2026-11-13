import index from "./index.html";
import { proxyOrigin } from "./proxy";

// The lobby and the backend run as separate processes (./scripts/dev.sh), and
// this server forwards each one's paths to it (proxy.ts), so the browser stays
// on one origin. The Centrifugo WebSocket is not proxied: the browser connects
// straight to :8000, whose allowed origins already include this server.
const server = Bun.serve({
  port: 3000,
  development: { hmr: true, console: true },
  routes: { "/": index },
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
