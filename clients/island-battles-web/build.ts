import { rm } from "node:fs/promises";
import { devServers, productionServers } from "./desktopServers";

// The browser build is served at /play/ in production, so asset URLs carry that prefix.
// It reads the lobby's URL from config.json beside the page at run time (src/platform.ts).
// `--desktop` builds the same code for the Tauri app (src-tauri/): the window serves it
// from its own root, and the build is told which game and lobby it talks to, the
// official ones (desktopServers.ts). `--dev` points them at the dev server and the lobby
// on this machine (`./scripts/dev.sh`).
const desktop = process.argv.includes("--desktop");
const dev = process.argv.includes("--dev");
const servers = dev ? devServers : productionServers;
if (dev && !desktop) {
  console.error("--dev only applies to --desktop");
  process.exit(1);
}

const outdir = desktop ? "dist-desktop" : "dist";
await rm(outdir, { recursive: true, force: true });
const result = await Bun.build({
  entrypoints: ["./index.html"],
  outdir,
  publicPath: desktop ? "/" : "/play/",
  minify: true,
  splitting: true,
  define: {
    __DESKTOP_API_BASE__: desktop ? JSON.stringify(servers.game) : "undefined",
    __DESKTOP_LOBBY_BASE__: desktop
      ? JSON.stringify(servers.lobby)
      : "undefined",
  },
  // The page keeps a stable name; everything it loads is hashed for caching.
  naming: {
    entry: "[name].[ext]",
    chunk: "assets/[name]-[hash].[ext]",
    asset: "assets/[name]-[hash].[ext]",
  },
});

if (!result.success) {
  for (const log of result.logs) console.error(log);
  process.exit(1);
}
for (const output of result.outputs) console.log(output.path);
