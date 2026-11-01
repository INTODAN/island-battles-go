import { rm } from "node:fs/promises";

// The client is served at /play/ in production, so asset URLs carry that prefix.
await rm("dist", { recursive: true, force: true });
const result = await Bun.build({
  entrypoints: ["./index.html"],
  outdir: "dist",
  publicPath: "/play/",
  minify: true,
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
