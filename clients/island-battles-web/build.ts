import { rm } from "node:fs/promises";

// The browser build is served at /play/ in production, so asset URLs carry that prefix.
// `--desktop` builds the same code for the Tauri app (src-tauri/): the window serves it
// from its own root, and the build is told which game server it talks to. `--dev` points
// that at a backend on this machine (`./scripts/dev.sh`).
const desktop = process.argv.includes("--desktop");
const dev = process.argv.includes("--dev");
const productionServer = "https://island-battles.elidan.org";
const localServer = "http://localhost:8888";
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
  define: {
    __DESKTOP_API_BASE__: desktop
      ? JSON.stringify(dev ? localServer : productionServer)
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
