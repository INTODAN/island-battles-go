import { fetch as tauriFetch } from "@tauri-apps/plugin-http";
import { desktopBaseUrl, resolveAgainstBase } from "./platform";

/**
 * Desktop build only. The web view's origin is `tauri://localhost`, and the
 * game server sends no CORS headers, so every request goes through Tauri's HTTP
 * plugin, which sends it from Rust. Its scope (`src-tauri/capabilities/`) is
 * the game's hostname and the localhost bot port, so a request to any other
 * host is refused. Code calls `globalThis.fetch` at call time, so replacing it
 * once here covers the API clients, the realtime session and the bot relay.
 */
export function installDesktopFetch(): void {
  const base = desktopBaseUrl;
  if (base === undefined) return;
  globalThis.fetch = ((input, init) =>
    tauriFetch(
      resolveAgainstBase(input, base),
      init as Parameters<typeof tauriFetch>[1],
    )) as typeof fetch;
}
