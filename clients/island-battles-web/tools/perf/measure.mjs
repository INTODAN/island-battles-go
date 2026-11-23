// Measures the 3D client's CPU, frames, memory and garbage (#403), for comparing a change with
// `main`. Run `bun run dev` first, then, from clients/island-battles-web:
//   node tools/perf/measure.mjs [http://localhost:3000] [--fx=1] [--size=800x500] [--turns=6]
//     [--only=board,view,lobby,cycles] [--games=5] [--fixtures=six-teams.json,busy-turn.json]
// Needs Playwright with a Chromium (`require("playwright")`); no backend, because the dev page
// (`/?fixture=`) and the demo view (`/?demo=`) read the fixtures, and the lobby's screens draw
// their backdrop without one. It runs headless with software WebGL (SwiftShader), so GPU-side
// costs are exaggerated: compare two runs on one machine rather than reading absolute numbers.
//
// What it prints, as JSON lines and then a table:
// - board, per fixture (`six-teams.json`, `busy-turn.json`): main-thread busy time per second
//   (CDP `Performance.getMetrics`, `TaskDuration` and `ScriptDuration`) and frames drawn per
//   second, with the board idle (10 s, no turn playing) and with a turn playing (the first 10 s
//   of playing the fixture on); over the playback of its first `--turns=6` turns the average and worst time between
//   drawn frames, the bytes allocated (a sampling heap profile, with the functions that allocate
//   most) and the garbage collections (a trace's MinorGC and MajorGC events); the JS heap after a
//   forced GC and Babylon's counts (textures in the engine's cache, meshes, materials) after the
//   board loads and after the playback.
// - game view: busy time and frames per second of the demo view (`/?demo=six-teams.json`), the
//   game screen itself, over 10 s idle and the first 10 s of playing it.
// - lobby: busy time and frames per second over 10 s of the lobby's first screen with its ocean.
// - cycles: 5 games in a row in one game view (`/?demo=six-teams.json`, then Open demo file 4
//   times, which leaves the game shown and starts the next on a new engine), each played for 3 s:
//   the JS heap after a forced GC with each game shown, how many WebGL contexts and canvases are
//   still reachable then, the WebGL textures, buffers and programs not deleted in those contexts
//   that are not lost, and how long each game after the first took to load its art (`loadMs`,
//   from Open demo file until its loading screen starts to fade).
//
// A "frame drawn" is an animation frame callback that issued a WebGL draw call: the script wraps
// `requestAnimationFrame` and the draw calls before the page runs, the same way on any version.
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const { chromium } = require("playwright");

const args = process.argv.slice(2);
const base =
  args.find((arg) => !arg.startsWith("--")) ?? "http://localhost:3000";
const option = (name, fallback) =>
  args.find((arg) => arg.startsWith(`--${name}=`))?.split("=")[1] ?? fallback;
const fx = option("fx", "1");
const only = option("only", "board,view,lobby,cycles").split(",");
const fixtures = option("fixtures", "six-teams.json,busy-turn.json").split(",");
const turns = Number(option("turns", "6"));
const games = Number(option("games", "5"));
const [width, height] = option("size", "800x500").split("x").map(Number);
const here = path.dirname(fileURLToPath(import.meta.url));
const fixtureDir = path.resolve(here, "../../test/fixtures");

/** Runs in the page before its own scripts: counts drawn frames, WebGL objects and contexts. */
function instrument() {
  const perf = {
    frames: [],
    drew: false,
    contexts: [],
    canvases: [],
    /** WebGL objects made and not deleted, per context. */
    objects: new WeakMap(),
    /** The WebGL objects not deleted in the contexts still reachable and not lost. */
    live() {
      const total = { contexts: 0, textures: 0, buffers: 0, programs: 0 };
      for (const ref of perf.contexts) {
        const context = ref.deref();
        if (!context) continue;
        total.contexts++;
        if (context.isContextLost()) continue;
        const counts = perf.objects.get(context) ?? {};
        for (const key of ["textures", "buffers", "programs"]) {
          total[key] += counts[key] ?? 0;
        }
      }
      return total;
    },
  };
  window.__perf = perf;
  const raf = window.requestAnimationFrame.bind(window);
  window.requestAnimationFrame = (callback) =>
    raf((time) => {
      const start = performance.now();
      perf.drew = false;
      callback(time);
      if (perf.drew) perf.frames.push(start);
    });
  for (const type of [WebGLRenderingContext, WebGL2RenderingContext]) {
    const proto = type.prototype;
    for (const name of [
      "drawElements",
      "drawArrays",
      "drawElementsInstanced",
      "drawArraysInstanced",
    ]) {
      const original = proto[name];
      if (!original) continue;
      proto[name] = function (...rest) {
        perf.drew = true;
        return original.apply(this, rest);
      };
    }
    const count = (create, remove, key) => {
      const made = proto[create];
      const gone = proto[remove];
      const counts = (context) => {
        let entry = perf.objects.get(context);
        if (!entry) {
          entry = {};
          perf.objects.set(context, entry);
        }
        return entry;
      };
      proto[create] = function (...rest) {
        const entry = counts(this);
        entry[key] = (entry[key] ?? 0) + 1;
        return made.apply(this, rest);
      };
      proto[remove] = function (...rest) {
        if (rest[0]) counts(this)[key]--;
        return gone.apply(this, rest);
      };
    };
    count("createTexture", "deleteTexture", "textures");
    count("createBuffer", "deleteBuffer", "buffers");
    count("createProgram", "deleteProgram", "programs");
  }
  const getContext = HTMLCanvasElement.prototype.getContext;
  HTMLCanvasElement.prototype.getContext = function (kind, ...rest) {
    const context = getContext.call(this, kind, ...rest);
    if (context && String(kind).startsWith("webgl")) {
      if (!perf.contexts.some((ref) => ref.deref() === context)) {
        perf.contexts.push(new WeakRef(context));
        perf.canvases.push(new WeakRef(this));
      }
    }
    return context;
  };
}

const browser = await chromium.launch({
  args: [
    "--use-gl=angle",
    "--use-angle=swiftshader",
    "--enable-unsafe-swiftshader",
    "--ignore-gpu-blocklist",
  ],
});

async function openPage(url) {
  const page = await browser.newPage({ viewport: { width, height } });
  page.on("pageerror", (error) => console.error("page error:", error.message));
  await page.addInitScript(instrument);
  // The game view and the lobby read the Effects setting from storage; the dev page from `&fx=`.
  await page.addInitScript(
    (value) => {
      localStorage.setItem("island-battles-web.graphics", value);
    },
    fx === "1" ? "high" : "low",
  );
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Performance.enable");
  await page.goto(url);
  return { page, cdp };
}

async function metrics(cdp) {
  const { metrics: list } = await cdp.send("Performance.getMetrics");
  return Object.fromEntries(list.map(({ name, value }) => [name, value]));
}

async function heapAfterGc(page, cdp) {
  for (let i = 0; i < 3; i++) await cdp.send("HeapProfiler.collectGarbage");
  await page.waitForTimeout(200);
  return (await metrics(cdp)).JSHeapUsedSize;
}

/** Busy time and frames per second over `seconds`, with the drawn frames' timestamps. */
async function sample(page, cdp, seconds) {
  const before = await metrics(cdp);
  const from = await page.evaluate(() => window.__perf.frames.length);
  await page.waitForTimeout(seconds * 1000);
  const after = await metrics(cdp);
  const frames = await page.evaluate(
    (start) => window.__perf.frames.slice(start),
    from,
  );
  const span = after.Timestamp - before.Timestamp;
  return {
    taskMsPerS: ((after.TaskDuration - before.TaskDuration) * 1000) / span,
    scriptMsPerS:
      ((after.ScriptDuration - before.ScriptDuration) * 1000) / span,
    fps: frames.length / span,
    frames,
  };
}

function gaps(frames) {
  const between = [];
  for (let i = 1; i < frames.length; i++)
    between.push(frames[i] - frames[i - 1]);
  if (between.length === 0) return { avg: null, worst: null };
  const avg = between.reduce((sum, value) => sum + value, 0) / between.length;
  return { avg, worst: Math.max(...between) };
}

async function babylonCounts(page) {
  return page.evaluate(() => {
    const scene = window.battlefieldDebug.field.scene;
    return {
      textures: scene.getEngine().getLoadedTexturesCache().length,
      meshes: scene.meshes.length,
      materials: scene.materials.length,
    };
  });
}

async function gcEvents(page, run) {
  await browser.startTracing(page, {
    categories: [
      "devtools.timeline",
      "v8",
      "disabled-by-default-devtools.timeline",
    ],
  });
  const result = await run();
  const trace = JSON.parse((await browser.stopTracing()).toString());
  const events = trace.traceEvents ?? trace;
  const count = (name) =>
    events.filter((event) => event.name === name && event.ph !== "E").length;
  return { result, minorGc: count("MinorGC"), majorGc: count("MajorGC") };
}

/** The functions that allocated most, each with its two callers: "f < caller < its caller". */
function topAllocators(profile, limit = 12) {
  const self = new Map();
  const sizeOf = new Map();
  for (const sample of profile.samples) {
    sizeOf.set(sample.nodeId, (sizeOf.get(sample.nodeId) ?? 0) + sample.size);
  }
  const name = (node) => node.callFrame.functionName || "(anonymous)";
  const walk = (node, callers) => {
    const size = sizeOf.get(node.id) ?? 0;
    if (size > 0) {
      const key = [name(node), ...callers.slice(-2).reverse()].join(" < ");
      self.set(key, (self.get(key) ?? 0) + size);
    }
    for (const child of node.children ?? [])
      walk(child, [...callers, name(node)]);
  };
  walk(profile.head, []);
  return [...self.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, limit)
    .map(([where, bytes]) => `${(bytes / 1024).toFixed(0)} KB ${where}`);
}

const results = {};

async function measureBoard(fixture) {
  const { page, cdp } = await openPage(`${base}/?fixture=${fixture}&fx=${fx}`);
  await page.waitForFunction(() => window.battlefieldDebug);
  await page.evaluate(() => window.battlefieldDebug.assets);
  // Let the shaders, the shadows and the particle pool build.
  await page.waitForTimeout(6000);
  const loadedHeap = await heapAfterGc(page, cdp);
  const loadedCounts = await babylonCounts(page);
  const idle = await sample(page, cdp, 10);
  await cdp.send("HeapProfiler.enable");
  await cdp.send("HeapProfiler.startSampling", {
    samplingInterval: 16384,
    includeObjectsCollectedByMajorGC: true,
    includeObjectsCollectedByMinorGC: true,
  });
  const { result, minorGc, majorGc } = await gcEvents(page, async () => {
    const from = await page.evaluate(() => window.__perf.frames.length);
    await page.evaluate(() => window.battlefieldDebug.setPlaying(true));
    const playing = await sample(page, cdp, 10);
    // The first `turns` turns: the slider shows the frame the player has reached.
    await page.waitForFunction(
      (last) =>
        Number(document.querySelector('input[aria-label="Frame"]')?.value) >=
        last,
      turns,
      { timeout: 300000, polling: 100 },
    );
    await page.evaluate(() => window.battlefieldDebug.setPlaying(false));
    await page.waitForFunction(
      () => !window.battlefieldDebug.player.busy,
      undefined,
      {
        timeout: 60000,
        polling: 100,
      },
    );
    const frames = await page.evaluate(
      (start) => window.__perf.frames.slice(start),
      from,
    );
    return { playing, frames };
  });
  const { profile } = await cdp.send("HeapProfiler.stopSampling");
  const allocated = profile.samples.reduce(
    (sum, sample) => sum + sample.size,
    0,
  );
  const playback = gaps(result.frames);
  await page.waitForTimeout(3000);
  const afterHeap = await heapAfterGc(page, cdp);
  const afterCounts = await babylonCounts(page);
  const idleAfter = await sample(page, cdp, 10);
  const row = {
    idle: {
      taskMsPerS: idle.taskMsPerS,
      scriptMsPerS: idle.scriptMsPerS,
      fps: idle.fps,
    },
    idleAfterPlayback: {
      taskMsPerS: idleAfter.taskMsPerS,
      scriptMsPerS: idleAfter.scriptMsPerS,
      fps: idleAfter.fps,
    },
    playing: {
      taskMsPerS: result.playing.taskMsPerS,
      scriptMsPerS: result.playing.scriptMsPerS,
      fps: result.playing.fps,
    },
    playback: {
      frames: result.frames.length,
      avgFrameMs: playback.avg,
      worstFrameMs: playback.worst,
      allocatedMB: allocated / 1048576,
      minorGc,
      majorGc,
      top: topAllocators(profile),
    },
    heapMB: {
      loaded: loadedHeap / 1048576,
      afterPlayback: afterHeap / 1048576,
    },
    counts: { loaded: loadedCounts, afterPlayback: afterCounts },
  };
  results[fixture] = row;
  console.log(JSON.stringify({ fixture, ...row }));
  await page.close();
}

async function measureLobby() {
  const { page, cdp } = await openPage(`${base}/`);
  // The ocean fades in once the art has loaded.
  await page.waitForFunction(() => {
    const canvas = document.getElementById("scene");
    return canvas && !canvas.classList.contains("ocean-loading");
  });
  await page.waitForTimeout(5000);
  const lobby = await sample(page, cdp, 10);
  results.lobby = {
    taskMsPerS: lobby.taskMsPerS,
    scriptMsPerS: lobby.scriptMsPerS,
    fps: lobby.fps,
  };
  console.log(JSON.stringify({ lobby: results.lobby }));
  await page.close();
}

async function measureView() {
  const { page, cdp } = await openPage(`${base}/?demo=six-teams.json`);
  await page.waitForSelector(".gv-canvas");
  await page.waitForFunction(
    () => !document.querySelector(".board-loading"),
    undefined,
    {
      timeout: 60000,
    },
  );
  await page.waitForTimeout(5000);
  const idle = await sample(page, cdp, 10);
  await page.evaluate(() =>
    document.querySelector('button[aria-label="Play"]')?.click(),
  );
  const playing = await sample(page, cdp, 10);
  const between = gaps(playing.frames);
  const row = (entry) => ({
    taskMsPerS: entry.taskMsPerS,
    scriptMsPerS: entry.scriptMsPerS,
    fps: entry.fps,
  });
  results.view = {
    idle: row(idle),
    playing: {
      ...row(playing),
      avgFrameMs: between.avg,
      worstFrameMs: between.worst,
    },
  };
  console.log(JSON.stringify({ view: results.view }));
  await page.close();
}

async function measureCycles(times = 5) {
  const fixture = "six-teams.json";
  const { page, cdp } = await openPage(`${base}/?demo=${fixture}`);
  const heaps = [];
  const live = [];
  for (let cycle = 1; cycle <= times; cycle++) {
    const started = Date.now();
    if (cycle > 1) {
      // Open demo file: the screen leaves the game it shows and starts the new one, whose
      // loading screen stays until the art is in, and 400 ms more to fade.
      await page.setInputFiles(
        'input[aria-label="Demo file"]',
        path.join(fixtureDir, fixture),
      );
      await page.waitForSelector(".board-loading", { state: "attached" });
    }
    await page.waitForSelector(".gv-canvas");
    await page.waitForFunction(
      () => !document.querySelector(".board-loading"),
      undefined,
      {
        timeout: 60000,
      },
    );
    const loadMs = cycle > 1 ? Date.now() - started - 400 : null;
    await page.waitForTimeout(2000);
    const play = () =>
      page.evaluate(() =>
        document
          .querySelector(
            'button[aria-label="Play"], button[aria-label="Pause"]',
          )
          ?.click(),
      );
    await play();
    await page.waitForTimeout(3000);
    await play();
    heaps.push((await heapAfterGc(page, cdp)) / 1048576);
    live.push(
      await page.evaluate(
        (ms) => ({
          loadMs: ms,
          ...window.__perf.live(),
          canvases: window.__perf.canvases.filter((ref) => ref.deref()).length,
        }),
        loadMs,
      ),
    );
  }
  results.cycles = { heapMB: heaps, live };
  console.log(JSON.stringify({ cycles: results.cycles }));
  await page.close();
}

const fmt = (value, digits = 1) =>
  value === null || value === undefined ? "-" : value.toFixed(digits);

try {
  if (only.includes("board"))
    for (const fixture of fixtures) await measureBoard(fixture);
  if (only.includes("view")) await measureView();
  if (only.includes("lobby")) await measureLobby();
  if (only.includes("cycles")) await measureCycles(games);
} finally {
  await browser.close();
}

console.log(
  `\n${base}, Effects ${fx === "1" ? "on" : "off"}, ${width}x${height}`,
);
for (const fixture of fixtures) {
  const row = results[fixture];
  if (!row) continue;
  console.log(
    [
      `${fixture}:`,
      `  idle: ${fmt(row.idle.taskMsPerS)} ms/s busy (script ${fmt(row.idle.scriptMsPerS)}), ${fmt(row.idle.fps)} frames/s`,
      `  idle after the playback: ${fmt(row.idleAfterPlayback.taskMsPerS)} ms/s busy (script ${fmt(row.idleAfterPlayback.scriptMsPerS)}), ${fmt(row.idleAfterPlayback.fps)} frames/s`,
      `  turn playing: ${fmt(row.playing.taskMsPerS)} ms/s busy (script ${fmt(row.playing.scriptMsPerS)}), ${fmt(row.playing.fps)} frames/s`,
      `  ${turns} turns played: ${row.playback.frames} frames, average ${fmt(row.playback.avgFrameMs)} ms, worst ${fmt(row.playback.worstFrameMs)} ms; ${fmt(row.playback.allocatedMB)} MB allocated, ${row.playback.minorGc} minor and ${row.playback.majorGc} major GCs`,
      `  heap after GC: ${fmt(row.heapMB.loaded)} MB loaded, ${fmt(row.heapMB.afterPlayback)} MB after the playback`,
      `  Babylon: loaded ${JSON.stringify(row.counts.loaded)}, after the playback ${JSON.stringify(row.counts.afterPlayback)}`,
      "  most allocated:",
      ...row.playback.top.map((line) => `    ${line}`),
    ].join("\n"),
  );
}
if (results.view) {
  const { idle, playing } = results.view;
  console.log(
    `game view (/?demo=six-teams.json): idle ${fmt(idle.taskMsPerS)} ms/s busy (script ${fmt(idle.scriptMsPerS)}), ${fmt(idle.fps)} frames/s; ` +
      `playing ${fmt(playing.taskMsPerS)} ms/s busy (script ${fmt(playing.scriptMsPerS)}), ${fmt(playing.fps)} frames/s, frames ${fmt(playing.avgFrameMs)} ms on average, worst ${fmt(playing.worstFrameMs)} ms`,
  );
}
if (results.lobby) {
  const lobby = results.lobby;
  console.log(
    `lobby: ${fmt(lobby.taskMsPerS)} ms/s busy (script ${fmt(lobby.scriptMsPerS)}), ${fmt(lobby.fps)} frames/s`,
  );
}
if (results.cycles) {
  const { heapMB, live } = results.cycles;
  console.log(
    `${games} games in a row: heap with each ${heapMB.map((mb) => fmt(mb)).join(", ")} MB`,
  );
  console.log(
    `  reachable with each: ${live.map((entry) => JSON.stringify(entry)).join(" ")}`,
  );
}
