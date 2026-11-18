// Measures whether a hand player's tab keeps calling the server while it is hidden (#342): the gaps
// between its `POST …/deadline` calls, beside a plain `setTimeout` chain in the same tab that the
// browser does slow down. Run `./scripts/dev.sh` and `bun run dev` first, then, from
// clients/island-battles-web:
//   node tools/handplay/hidden-tab.mjs [--minutes 8] [--pace 1000] [--chrome /usr/bin/google-chrome]
// On Linux without a display, run it under `xvfb-run -a`. It needs Chrome (not Playwright, which
// keeps every page visible): it starts Chrome with a debugging port and talks CDP to it.
//
// Two guest sessions play by hand in one lobby; the second never orders and never calls, so the
// game ends only at its last turn and every turn is ended by the tab being measured. The tab
// opens the app with the first session, follows the game, and then a second tab is brought in
// front, which hides it (`document.visibilityState` is `hidden`). Chrome slows a hidden tab's
// timers to once a second, and after five minutes to once a minute; the control chain shows that.

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const option = (name, fallback) => {
  const index = process.argv.indexOf(`--${name}`);
  return index >= 0 ? process.argv[index + 1] : fallback;
};
const minutes = Number(option("minutes", "8"));
const pace = Number(option("pace", "1000"));
const chromePath = option("chrome", "/usr/bin/google-chrome");
const app = option("app", "http://localhost:3000");
const lobby = option("lobby", "http://localhost:8890");
const port = Number(option("port", "9333"));
const sleep = (ms) => new Promise((done) => setTimeout(done, ms));

async function call(method, path, token, body) {
  const response = await fetch(lobby + path, {
    method,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok)
    throw new Error(`${method} ${path}: ${response.status} ${text}`);
  return text ? JSON.parse(text) : undefined;
}

// --- The game: two hand players, nobody taken out for silence ---

const session = async (name) => {
  const created = await call("POST", "/lobby/v1/sessions", undefined, {
    display_name: name,
  });
  return { ...created, name };
};
const alice = await session("Hidden tab");
const bob = await session("Silent player");
const room = await call("POST", "/lobby/v1/lobbies", alice.access_token, {
  game_key: "island-battles",
  name: "Hidden tab",
  request_id: crypto.randomUUID(),
  client_instance_id: crypto.randomUUID(),
  teams: [{ name: "Red" }, { name: "Blue" }],
  game_settings: {
    map: {
      map_name: "Far apart",
      player_amount: 2,
      plane_size_x: 26,
      plane_size_y: 10,
      neutral_starting_soldier_amount: 5,
      castles: [
        { team_color: 1, x: -10, y: 0, starting_soldier_amount: 30 },
        { team_color: 2, x: 10, y: 0, starting_soldier_amount: 30 },
      ],
    },
    turn_pace_milliseconds: pace,
    max_generations: 1200,
    turn_timeout_seconds: null,
  },
});
const lobbyId = room.lobby.id;
const base = `/lobby/v1/games/island-battles/lobbies/${lobbyId}`;
await call("POST", "/lobby/v1/memberships", bob.access_token, {
  game_key: "island-battles",
  join_code: room.lobby.join_code,
  client_instance_id: crypto.randomUUID(),
});
for (const [player, team] of [
  [alice, room.teams[0]],
  [bob, room.teams[1]],
]) {
  await call("PUT", `${base}/team`, player.access_token, { team_id: team.id });
  await call("PUT", `${base}/member-settings`, player.access_token, {
    member_settings: { bot: { kind: "manual" } },
  });
  await call("PUT", `${base}/ready`, player.access_token, { ready: true });
}
await call("POST", `${base}/start`, alice.access_token, {});
console.log(`lobby ${lobbyId} started, pace ${pace} ms`);

// --- Chrome, driven over CDP ---

const profile = mkdtempSync(join(tmpdir(), "hidden-tab-"));
const args = [
  `--remote-debugging-port=${port}`,
  `--user-data-dir=${profile}`,
  "--no-first-run",
  "--no-default-browser-check",
  "--use-angle=swiftshader",
  "--enable-unsafe-swiftshader",
  "about:blank",
];
const chrome = spawn(chromePath, args, { stdio: "ignore" });
const devtools = `http://127.0.0.1:${port}`;
let version;
for (let i = 0; i < 100 && !version; i++) {
  try {
    version = await (await fetch(`${devtools}/json/version`)).json();
  } catch {
    await sleep(100);
  }
}

async function connect(webSocketUrl, onEvent) {
  const socket = new WebSocket(webSocketUrl);
  await new Promise((resolve, reject) => {
    socket.onopen = resolve;
    socket.onerror = reject;
  });
  let id = 0;
  const waiting = new Map();
  socket.onmessage = (message) => {
    const data = JSON.parse(message.data);
    if (data.id && waiting.has(data.id)) {
      waiting.get(data.id)(data);
      waiting.delete(data.id);
    } else if (data.method) {
      onEvent(data);
    }
  };
  return {
    send(method, params = {}) {
      const sent = ++id;
      socket.send(JSON.stringify({ id: sent, method, params }));
      return new Promise((resolve) => waiting.set(sent, resolve));
    },
    close: () => socket.close(),
  };
}

const opened = await (
  await fetch(`${devtools}/json/new?${app}/config.json`, { method: "PUT" })
).json();
// Deadline calls and lobby heartbeats, by Chrome's wall clock.
const requests = new Map();
const deadlines = [];
const heartbeats = [];
const page = await connect(
  opened.webSocketDebuggerUrl,
  ({ method, params }) => {
    if (
      method === "Network.requestWillBeSent" &&
      params.request.method === "POST"
    ) {
      const kind = params.request.url.endsWith("/deadline")
        ? "deadline"
        : params.request.url.endsWith("/heartbeat")
          ? "heartbeat"
          : undefined;
      if (kind)
        requests.set(params.requestId, { kind, at: params.wallTime * 1000 });
    } else if (method === "Network.responseReceived") {
      const request = requests.get(params.requestId);
      if (request) {
        (request.kind === "deadline" ? deadlines : heartbeats).push({
          at: request.at,
          status: params.response.status,
        });
      }
    }
  },
);
const evaluate = async (expression) =>
  (
    await page.send("Runtime.evaluate", {
      expression,
      returnByValue: true,
      awaitPromise: true,
    })
  ).result?.result?.value;
await page.send("Network.enable");
await sleep(500);
await evaluate(
  `sessionStorage.setItem("island-battles-web.session", ${JSON.stringify(
    JSON.stringify({
      playerId: alice.player_id,
      accessToken: alice.access_token,
      expiresAt: Date.parse(alice.expires_at),
      displayName: alice.name,
    }),
  )}); sessionStorage.setItem("island-battles-web.room", ${JSON.stringify(
    JSON.stringify({
      gameKey: "island-battles",
      lobbyId,
      joinCode: room.lobby.join_code,
    }),
  )})`,
);
await page.send("Page.navigate", { url: `${app}/` });
for (let i = 0; i < 300; i++) {
  const kind = await evaluate(
    "window.islandBattles?.realtime.game.get()?.bot.kind ?? null",
  );
  if (kind === "manual" && deadlines.length > 3) break;
  await sleep(200);
}
console.log(`following the game; ${deadlines.length} deadline calls so far`);

// The control: a plain 1 s setTimeout chain in the same tab, started before it is hidden.
await evaluate(`(() => {
  window.controlTicks = [];
  const tick = () => { window.controlTicks.push(Date.now()); setTimeout(tick, 1000); };
  tick();
})()`);
const before = await evaluate("document.visibilityState");
await fetch(`${devtools}/json/new?about:blank`, { method: "PUT" });
await sleep(500);
const hiddenAt = Date.now();
const hidden = await evaluate("document.visibilityState");
console.log(
  `visibility: ${before} before, ${hidden} once another tab is in front`,
);
for (let minute = 1; minute <= minutes; minute++) {
  await sleep(60_000);
  console.log(`minute ${minute}: ${deadlines.length} deadline calls`);
}
const hiddenUntil = Date.now();
const after = await evaluate(
  "({ visibility: document.visibilityState, discarded: document.wasDiscarded, control: window.controlTicks, turn: window.islandBattles?.realtime.game.get()?.turn_number })",
);

const gapsOf = (times) =>
  times.slice(1).map((time, index) => time - times[index]);
const during = (list) =>
  list
    .filter((entry) => entry.at >= hiddenAt && entry.at <= hiddenUntil)
    .map((entry) => entry.at);
const largest = (gaps) => (gaps.length ? Math.max(...gaps) : Number.NaN);
const callsHidden = during(deadlines);
const lastMinute = (times) =>
  times.filter((time) => time >= hiddenUntil - 60_000);
const statuses = {};
for (const entry of deadlines)
  statuses[entry.status] = (statuses[entry.status] ?? 0) + 1;
const control = after.control.filter((time) => time >= hiddenAt);
const summary = {
  chrome: version?.Browser,
  command: [chromePath, ...args].join(" "),
  pace,
  hiddenMinutes: (hiddenUntil - hiddenAt) / 60_000,
  visibility: {
    before,
    hidden,
    end: after.visibility,
    discarded: after.discarded,
  },
  deadlineCallsWhileHidden: callsHidden.length,
  deadlineStatuses: statuses,
  largestDeadlineGapMs: largest(gapsOf(callsHidden)),
  largestDeadlineGapLastMinuteMs: largest(gapsOf(lastMinute(callsHidden))),
  controlTicksWhileHidden: control.length,
  largestControlGapMs: largest(gapsOf(control)),
  largestControlGapLastMinuteMs: largest(gapsOf(lastMinute(control))),
  heartbeatsWhileHidden: during(heartbeats).length,
  largestHeartbeatGapMs: largest(gapsOf(during(heartbeats))),
  turnAtEnd: after.turn,
};
console.log(JSON.stringify(summary, null, 2));
page.close();
chrome.kill();
await sleep(500);
rmSync(profile, { recursive: true, force: true });
process.exit(0);
