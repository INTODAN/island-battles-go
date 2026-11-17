// Measures the draw calls and triangles of one frame of a fixture on the dev page, for the README's
// Budgets table. Run `bun run dev` first, then, from clients/island-battles-web:
//   node tools/budgets/measure.mjs [fixture.json] [http://localhost:3000]
// Needs Playwright with a Chromium (`require("playwright")`). It opens the dev page headless, with
// `&fx=0` ("low", Effects off) and `&fx=1` ("high", Effects on), and reads
// `window.battlefieldDebug.stats()` (Babylon's per-frame draw call counter and the active indices
// of `scene.render()`). "At rest" shows every frame with no turn playing; "playing" plays each turn
// and keeps the busiest sample (every 120 ms) of that turn, so it includes the particle systems.
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { chromium } = require("playwright");

const fixture = process.argv[2] ?? "six-teams.json";
const base = process.argv[3] ?? "http://localhost:3000";
const range = (values) => {
  const low = Math.min(...values);
  const high = Math.max(...values);
  return low === high ? `${low}` : `${low} to ${high}`;
};

const browser = await chromium.launch({
  args: [
    "--use-gl=angle",
    "--use-angle=swiftshader",
    "--enable-unsafe-swiftshader",
    "--ignore-gpu-blocklist",
  ],
});
try {
  for (const fx of [0, 1]) {
    const page = await browser.newPage({
      viewport: { width: 1280, height: 800 },
    });
    page.on("pageerror", (error) =>
      console.error("page error:", error.message),
    );
    await page.goto(`${base}/?fixture=${fixture}&fx=${fx}&still=1`);
    await page.waitForFunction(() => window.battlefieldDebug);
    await page.evaluate(() => window.battlefieldDebug.assets);
    const frames = await page.evaluate(
      () => window.battlefieldDebug.frameCount,
    );
    // Let the pipeline, the shadows and the particle pool build before the first sample.
    await page.waitForTimeout(fx === 1 ? 6000 : 1500);
    const rest = [];
    for (let frame = 0; frame < frames; frame++) {
      await page.evaluate(
        (index) => window.battlefieldDebug.showFrame(index),
        frame,
      );
      await page.waitForTimeout(500);
      rest.push(await page.evaluate(() => window.battlefieldDebug.stats()));
    }
    const playing = [];
    for (let frame = 1; frame < frames; frame++) {
      await page.evaluate(
        (index) => window.battlefieldDebug.showFrame(index - 1),
        frame,
      );
      await page.waitForTimeout(400);
      await page.evaluate(
        (index) => window.battlefieldDebug.playTurn(index),
        frame,
      );
      let best = null;
      for (let sample = 0; sample < 14; sample++) {
        await page.waitForTimeout(120);
        const now = await page.evaluate(() => window.battlefieldDebug.stats());
        if (!best || now.drawCalls > best.drawCalls) best = now;
      }
      playing.push(best);
    }
    const column = (rows, key) => range(rows.map((row) => row[key]));
    console.log(
      `${fixture} Effects ${fx ? "on" : "off"} (${frames} frames): ` +
        `at rest: draw calls ${column(rest, "drawCalls")}, triangles ${column(rest, "triangles")}, particle systems ${column(rest, "particleSystems")}; ` +
        `playing: draw calls ${column(playing, "drawCalls")}, triangles ${column(playing, "triangles")}`,
    );
    await page.close();
  }
} finally {
  await browser.close();
}
