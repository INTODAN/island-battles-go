import { fetch as tauriFetch } from "@tauri-apps/plugin-http";
import {
  desktopBaseUrl,
  resolveAgainstBase,
  setDesktopFetch,
} from "./platform";

/**
 * Desktop build only. The web view's origin is `tauri://localhost`, and the
 * game server sends no CORS headers, so every request goes through Tauri's HTTP
 * plugin, which sends it from Rust. Its scope (`src-tauri/capabilities/`) is
 * the game's hostname and the localhost bot port, so a request to any other
 * host is refused. The client's calls use `platformFetch`, so installing it once
 * here covers the API clients, the realtime session, the lease and the bot relay.
 * `globalThis.fetch` stays untouched on purpose: Tauri's IPC calls it.
 */
export function installDesktopFetch(): void {
  const base = desktopBaseUrl;
  if (base === undefined) return;
  setDesktopFetch(((input, init) =>
    tauriFetch(
      resolveAgainstBase(input, base),
      init as Parameters<typeof tauriFetch>[1],
    )) as typeof fetch);
}
