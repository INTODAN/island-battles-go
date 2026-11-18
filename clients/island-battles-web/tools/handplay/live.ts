// Drives hand play (src/play/) for a player who plays by hand against a level 2 bot seat, on the
// local stack, with no browser and no other client: the module's own deadline calls end the turns.
// Run `./scripts/dev.sh` first, then, from clients/island-battles-web:
//   bun run tools/handplay/live.ts [--pace 1000] [--turns 30] [--lobby http://localhost:8890] [--game http://localhost:8888]
// It creates a guest session and a lobby (a long map, so the bot doesn't win at once), sets the
// player's bot to manual, adds a level 2 bot seat and starts. On turn 2 it sends half of its
// castle's soldiers to the nearest neutral castle and buys Speed 2, and checks that both are in
// the next generation. Then it gives no orders for `--turns` turns and prints how far apart they
// came. It exits 1 when a check fails.

import { createApiClients } from "../../src/api/client";
import { setServerUrls } from "../../src/platform";
import { startHandPlay } from "../../src/play";
import { createRealtime, type GenerationState } from "../../src/realtime";

function option(name: string, fallback: string): string {
  const index = process.argv.indexOf(`--${name}`);
  return index >= 0 ? (process.argv[index + 1] ?? fallback) : fallback;
}

const pace = Number(option("pace", "1000"));
const turns = Number(option("turns", "30"));
const lobbyUrl = option("lobby", "http://localhost:8890");
const gameUrl = option("game", "http://localhost:8888");
setServerUrls({ lobby_url: lobbyUrl, game_url: gameUrl });

const failures: string[] = [];
const check = (ok: boolean, what: string) => {
  console.log(`${ok ? "ok  " : "FAIL"} ${what}`);
  if (!ok) failures.push(what);
};

// A long map: the bot's castle is 20 units away, 40 turns at Speed 1.
const map = {
  map_name: "Hand play check",
  player_amount: 2,
  plane_size_x: 26,
  plane_size_y: 10,
  neutral_starting_soldier_amount: 5,
  castles: [
    { team_color: 1, x: -10, y: 0, starting_soldier_amount: 30 },
    { team_color: 2, x: 10, y: 0, starting_soldier_amount: 30 },
    { team_color: 0, x: -7, y: 2 },
    { team_color: 0, x: 7, y: -2 },
  ],
};

const realtime = createRealtime({ storage: null });
await realtime.signIn("Hand player");
const { lobbyApi } = createApiClients({
  getAccessToken: () => realtime.session.get()?.accessToken,
});
const { data: created } = await lobbyApi.POST("/lobby/v1/lobbies", {
  body: {
    game_key: "island-battles",
    name: "Hand play check",
    request_id: crypto.randomUUID(),
    client_instance_id: crypto.randomUUID(),
    teams: [{ name: "Red" }, { name: "Blue" }],
    game_settings: {
      map,
      turn_pace_milliseconds: pace,
      max_generations: 1200,
    },
  },
});
if (!created) throw new Error("no lobby");
const lobbyId = created.lobby.id;
const path = { game_key: "island-battles", lobby_id: lobbyId };
const [red, blue] = created.teams;
if (!red || !blue) throw new Error("the lobby has no two teams");
await lobbyApi.POST("/lobby/v1/games/{game_key}/lobbies/{lobby_id}/seats", {
  params: { path },
  body: { team_id: blue.id, seat_settings: { bot_level: 2 } },
});
await lobbyApi.PUT("/lobby/v1/games/{game_key}/lobbies/{lobby_id}/team", {
  params: { path },
  body: { team_id: red.id },
});
await lobbyApi.PUT(
  "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/member-settings",
  { params: { path }, body: { member_settings: { bot: { kind: "manual" } } } },
);
await lobbyApi.PUT("/lobby/v1/games/{game_key}/lobbies/{lobby_id}/ready", {
  params: { path },
  body: { ready: true },
});
const { data: started } = await lobbyApi.POST(
  "/lobby/v1/games/{game_key}/lobbies/{lobby_id}/start",
  { params: { path }, body: {} },
);
console.log(`lobby ${lobbyId}, round ${started?.lobby.round}, pace ${pace} ms`);

// Arrival times of each generation, by this process's clock.
const arrivals: { generation: number; turn: number; at: number }[] = [];
realtime.game.subscribe((state) => {
  if (state && arrivals.at(-1)?.generation !== state.generation_id) {
    arrivals.push({
      generation: state.generation_id,
      turn: state.turn_number,
      at: performance.now(),
    });
  }
});
realtime.followGame(lobbyId, started?.lobby.round ?? 1);
realtime.refreshGame();
// Bun has no Worker-throttling to avoid: plain timers.
const play = startHandPlay({
  realtime,
  lobbyId,
  schedule(run, ms) {
    const timer = setTimeout(run, ms);
    return () => clearTimeout(timer);
  },
});

const sleep = (ms: number) => new Promise((done) => setTimeout(done, ms));
async function until<T>(
  read: () => T | undefined | null | false,
  what: string,
  ms = 15_000,
): Promise<T> {
  const end = performance.now() + ms;
  for (;;) {
    const value = read();
    if (value) return value;
    if (performance.now() > end)
      throw new Error(`timed out waiting for ${what}`);
    await sleep(20);
  }
}

const atTurn = (turn: number) =>
  until(() => {
    const state = realtime.game.get();
    return state && state.turn_number >= turn ? state : null;
  }, `turn ${turn}`);

await until(
  () => play.state.get().status === "playing" && play.state.get().upgrades,
  "playing",
);
const second = await atTurn(2);
const team = second.team_color;
const home = second.generation.castles.find(
  (castle) => castle.team_color === team,
);
if (!home) throw new Error("no castle of our own");
const neutral = second.generation.castles
  .filter((castle) => castle.team_color === 0)
  .sort(
    (a, b) =>
      Math.hypot(a.x - home.x, a.y - home.y) -
      Math.hypot(b.x - home.x, b.y - home.y),
  )[0];
if (!neutral) throw new Error("no neutral castle");

const sent = play.send(home.id, neutral.id, { share: 0.5 });
const bought = play.upgrade(home.id, "speed");
check(sent.ok && bought.ok, "both orders pass the local check");
const sendResult = sent.ok ? await sent.settled : undefined;
const upgradeResult = bought.ok ? await bought.settled : undefined;
check(
  sendResult?.state === "accepted",
  `the send is accepted (${sendResult?.sentence ?? ""})`,
);
check(
  upgradeResult?.state === "accepted",
  `the upgrade is accepted (${upgradeResult?.sentence ?? ""})`,
);
const orderedIn = sendResult?.generationId ?? second.generation_id;
const next = await until(() => {
  const state = realtime.game.get();
  return state && state.generation_id > orderedIn ? state : null;
}, "the next generation");
const group = next.generation.soldier_groups.find(
  (candidate) =>
    candidate.team_color === team && candidate.origin_castle_id === home.id,
);
check(
  group?.soldiers === sendResult?.soldiers &&
    group?.destination_castle_id === neutral.id,
  `a group of ${sendResult?.soldiers} soldiers left for castle ${neutral.id} (found ${group?.soldiers})`,
);
const homeNext = next.generation.castles.find(
  (castle) => castle.id === home.id,
);
check(
  homeNext?.upgrades.speed === 2,
  `the castle's Speed is 2 (found ${homeNext?.upgrades.speed})`,
);

// No orders now: the module's deadline calls alone end the turns.
const from = arrivals.length - 1;
const target = (realtime.game.get() as GenerationState).turn_number + turns;
await until(
  () => {
    const state = realtime.game.get();
    return state && (state.turn_number >= target || state.outcome);
  },
  `turn ${target}`,
  (turns + 10) * pace * 2,
);
const gaps = arrivals
  .slice(from)
  .slice(1)
  .map(
    (arrival, index) => arrival.at - (arrivals[from + index]?.at ?? arrival.at),
  );
const sorted = [...gaps].sort((a, b) => a - b);
const median = sorted[Math.floor(sorted.length / 2)] ?? 0;
const largest = sorted.at(-1) ?? 0;
console.log(
  `${gaps.length} turns with no orders: median ${median.toFixed(0)} ms, largest ${largest.toFixed(0)} ms, smallest ${(sorted[0] ?? 0).toFixed(0)} ms apart`,
);
check(
  gaps.length >= turns || Boolean(realtime.game.get()?.outcome),
  "every turn arrived",
);
check(largest < pace * 1.5, `turns came at most 1.5 paces apart`);
const outcome = realtime.game.get()?.outcome;
if (outcome) console.log(`the game ended: ${outcome.summary}`);

play.dispose();
realtime.signOut();
console.log(
  failures.length ? `${failures.length} check(s) failed` : "all checks passed",
);
process.exit(failures.length ? 1 : 0);
