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
    if (!isProxiedPath(url.pathname)) {
      return new Response("Not found", { status: 404 });
    }
    const response = await fetch(
      new Request(backendOrigin + url.pathname + url.search, request),
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
