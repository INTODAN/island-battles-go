// A player outside a demo delivers the turns of a localhost bot to it, and
// the bot submits them itself.
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { setTimeout as delay } from "node:timers/promises";
import test from "node:test";
import { ExamplePlayer, GAME_ENDED_PROBLEM } from "./example-player.mjs";
import { BotError } from "./localhost-bot.mjs";

const DEADLINE = "2030-01-01T00:00:00Z";

const stateOf = (overrides = {}) => ({
  lobby_id: "room", player_id: "player", game_id: 1, generation_id: 1, team_color: 1, submitted: false,
  bot: { kind: "localhost" }, memory: null, turn_deadline_at: null,
  generation: { castles: [{ id: 0, team_color: 1, soldiers: 1 }, { id: 1, team_color: 2, soldiers: 30 }], soldier_groups: [] },
  ...overrides,
});

// The server's turn delivery for a state.
const deliveryOf = (state) => ({
  turn: { game_id: state.game_id, generation: { id: state.generation_id }, team_color: state.team_color, memory: state.memory, upgrades: [], turn_deadline_at: state.turn_deadline_at },
  submit: { url: "http://localhost:8888/generation/commands", token: `ibst1.${state.generation_id}`, deadline_at: state.turn_deadline_at },
});

class Subscription extends EventEmitter {
  subscribe() { this.emit("subscribed"); }
  unsubscribe() {}
  publish(state) { this.emit("publication", { data: { schema_version: 1, type: "game.next_generation", state } }); }
}

async function until(predicate) {
  for (let attempt = 0; attempt < 200; attempt++) {
    if (predicate()) return;
    await delay(5);
  }
  assert.fail("expected event did not arrive");
}

// The server's game-ended delivery for a state.
const endedOf = (state) => ({
  game_id: state.game_id, team_color: state.team_color, turn_number: state.generation_id, outcome: state.outcome,
  generation: state.generation, memory: state.memory,
  demo: { url: `https://island-battles.example.org/game/${state.game_id}/demo?token=ibdt1.x`, expires_at: "2030-01-08T00:00:00Z" },
});

function relay(context, { bot = async () => {}, state = stateOf(), delivery = null, ended = null, endBot = async () => {}, ...options } = {}) {
  const subscription = new Subscription();
  const requests = [];
  const deliveries = [];
  const ends = [];
  const statuses = [];
  let current = state;
  const player = new ExamplePlayer({
    lobbyID: "room", playerID: "player", subscription, demo: false, botRetryDelay: 20, retryDelay: 20,
    onStatus: (title, detail) => statuses.push({ title, detail }),
    deliver: async (sent) => { deliveries.push(sent); return bot(sent); },
    deliverEnd: async (sent, port) => { ends.push({ sent, port }); return endBot(sent, port); },
    fetcher: async (url, init) => {
      requests.push({ url, method: init.method, headers: init.headers, body: init.body && JSON.parse(init.body) });
      if (url.endsWith("/game-ended-delivery")) {
        const answer = ended ? ended(player.state) : { status: 200, body: endedOf(player.state) };
        return { ok: answer.status === 200, status: answer.status, json: async () => answer.body };
      }
      if (url.endsWith("/turn-delivery")) {
        const answer = delivery ? delivery(player.state) : { status: 200, body: deliveryOf(player.state) };
        return { ok: answer.status === 200, status: answer.status, json: async () => answer.body };
      }
      return { ok: true, status: 200, json: async () => current };
    },
    ...options,
  });
  context.after(() => player.stop());
  const reads = () => requests.filter((request) => request.url.endsWith("/turn-delivery"));
  const submissions = () => requests.filter((request) => request.url === "/generation/commands");
  const deadlines = () => requests.filter((request) => request.url.endsWith("/deadline"));
  const endReads = () => requests.filter((request) => request.url.includes("/game-ended-delivery"));
  return { player, subscription, requests, deliveries, ends, statuses, reads, submissions, deadlines, endReads, setState: (next) => { current = next; } };
}

test("a localhost bot gets the server's turn delivery, and the page submits nothing", async (context) => {
  const { player, deliveries, reads, submissions, statuses } = relay(context);
  player.receive(stateOf({ memory: { seen: 0 }, turn_deadline_at: DEADLINE }));
  await until(() => deliveries.length === 1 && !player.inFlight);
  assert.equal(reads()[0].url, "/game/from-lobby/room/turn-delivery");
  assert.equal(reads()[0].method, "GET");
  // The page's fetcher adds the player's token; the player adds no credential.
  assert.equal(reads()[0].headers.Authorization, undefined);
  assert.deepEqual(deliveries[0], deliveryOf(stateOf({ memory: { seen: 0 }, turn_deadline_at: DEADLINE })));
  await delay(30);
  assert.equal(submissions().length, 0, "the bot submits, not the page");
  assert.equal(statuses.at(-1).title, "Your bot is playing");
  assert.match(statuses.at(-1).detail, /submits it itself/);
});

test("each generation is delivered once, even when its state arrives again", async (context) => {
  const { player, subscription, deliveries } = relay(context);
  player.receive(stateOf());
  subscription.publish(stateOf());
  await until(() => deliveries.length === 1 && !player.inFlight);
  subscription.publish(stateOf());
  player.receive(stateOf());
  await delay(30);
  assert.equal(deliveries.length, 1);
  player.receive(stateOf({ generation_id: 2 }));
  await until(() => deliveries.length === 2);
  assert.equal(deliveries[1].turn.generation.id, 2);
});

test("a delivered turn waits for the others and asks for the deadline", async (context) => {
  const { player, deliveries, deadlines } = relay(context, { now: () => Date.parse(DEADLINE) + 5000 });
  player.receive(stateOf({ turn_deadline_at: DEADLINE }));
  await until(() => deliveries.length === 1);
  await until(() => deadlines().length >= 1);
});

test("a default bot's player delivers and reports nothing", async (context) => {
  const { player, deliveries, requests, statuses } = relay(context);
  player.receive(stateOf({ bot: { kind: "default", level: 2 } }));
  await delay(30);
  assert.equal(deliveries.length, 0);
  assert.deepEqual(requests, []);
  assert.deepEqual(statuses, []);
});

test("a hosted or uploaded bot's player delivers nothing and says that the server plays", async (context) => {
  for (const [bot, detail] of [
    [{ kind: "hosted", host: "bot.example.com" }, /your hosted bot at bot\.example\.com; this page relays nothing/],
    [{ kind: "uploaded" }, /your uploaded bot each turn; this page relays nothing/],
  ]) {
    const { player, deliveries, requests, statuses } = relay(context);
    player.receive(stateOf({ bot }));
    await delay(30);
    assert.equal(deliveries.length, 0);
    assert.deepEqual(requests, []);
    assert.equal(statuses.at(-1).title, "Played by the server");
    assert.match(statuses.at(-1).detail, detail);
  }
});

test("a server-played bot's player still asks for the deadline", async (context) => {
  const { player, deadlines } = relay(context, { now: () => Date.parse(DEADLINE) + 5000 });
  player.receive(stateOf({ bot: { kind: "uploaded" }, turn_deadline_at: DEADLINE }));
  await until(() => deadlines().length === 1);
});

test("a default bot's player still asks for the deadline", async (context) => {
  const { player, deadlines } = relay(context, { now: () => Date.parse(DEADLINE) + 5000 });
  player.receive(stateOf({ bot: { kind: "default", level: 2 }, turn_deadline_at: DEADLINE }));
  await until(() => deadlines().length === 1);
});

test("a bot that is not running is asked again, and then gets the turn", async (context) => {
  let calls = 0;
  const { player, statuses, deliveries } = relay(context, {
    bot: async () => {
      if (++calls < 3) throw new BotError("unreachable", "Your bot is not reachable on 127.0.0.1:41414.");
    },
  });
  player.receive(stateOf());
  await until(() => calls === 3 && !player.inFlight);
  assert.equal(deliveries.length, 3);
  assert.ok(statuses.some((status) => status.title === "Your bot is not running"));
  assert.equal(statuses.at(-1).title, "Your bot is playing");
});

test("a bot that refuses the turn shows why and is not asked again; the player asks for the deadline", async (context) => {
  const { player, deliveries, statuses, submissions, deadlines } = relay(context, {
    bot: async () => { throw new BotError("refused", "Your bot refused the turn (HTTP 400).\nsubmit.url is not an allowed host"); },
    now: () => Date.parse(DEADLINE) + 5000,
  });
  player.receive(stateOf({ turn_deadline_at: DEADLINE }));
  await until(() => deadlines().length === 1);
  assert.equal(deliveries.length, 1);
  assert.equal(submissions().length, 0);
  assert.equal(statuses.at(-1).title, "Your bot refused the turn");
  assert.match(statuses.at(-1).detail, /not an allowed host/);
  // The next generation is delivered again.
  player.receive(stateOf({ generation_id: 2, turn_deadline_at: DEADLINE }));
  await until(() => deliveries.length === 2);
});

test("a turn with nothing to play is not delivered: the state is read again", async (context) => {
  const { player, deliveries, requests, setState } = relay(context, {
    delivery: () => ({ status: 409, body: { detail: "player turn is already complete" } }),
  });
  setState(stateOf({ submitted: true }));
  player.receive(stateOf());
  await until(() => player.state.submitted);
  await delay(30);
  assert.equal(deliveries.length, 0);
  assert.equal(requests.filter((request) => request.url === "/game/from-lobby/room/turn-delivery").length, 1);
});

test("a game that ended while reading the delivery stops the player", async (context) => {
  const { player, deliveries, statuses, setState } = relay(context, {
    delivery: () => ({ status: 409, body: { type: GAME_ENDED_PROBLEM, detail: "the game has ended" } }),
  });
  setState(stateOf({ outcome: { summary: "Blue wins" } }));
  player.receive(stateOf());
  await until(() => statuses.some((status) => status.title === "Game over"));
  assert.equal(deliveries.length, 0);
});

test("a server that doesn't answer the delivery read is asked again", async (context) => {
  let reads = 0;
  const { player, deliveries } = relay(context, {
    delivery: (state) => (++reads < 2 ? { status: 503, body: { detail: "unavailable" } } : { status: 200, body: deliveryOf(state) }),
  });
  player.receive(stateOf());
  await until(() => deliveries.length === 1);
  assert.equal(reads, 2);
});

const OUTCOME = { reason: "last_team_standing", winner_team_color: 2, summary: "Blue wins" };

test("once the game has an outcome, the localhost bot gets its game-ended delivery once, on its port", async (context) => {
  const { player, ends, endReads, statuses } = relay(context);
  player.receive(stateOf());
  await until(() => !player.inFlight && player.delivered === "1:1");
  const final = stateOf({ generation_id: 2, outcome: OUTCOME, bot: { kind: "localhost", port: 41417 } });
  player.receive(final);
  await until(() => ends.length === 1);
  assert.equal(endReads()[0].url, "/game/from-lobby/room/game-ended-delivery");
  assert.equal(endReads()[0].method, "GET");
  assert.equal(endReads()[0].headers.Authorization, undefined);
  assert.deepEqual(ends[0], { sent: endedOf(final), port: 41417 });
  // The final state again, or a deadline answer for it, tells the bot nothing more.
  player.receive(final);
  player.receive({ ...final, outcome: undefined });
  await delay(30);
  assert.equal(endReads().length, 1);
  assert.equal(ends.length, 1);
  assert.equal(statuses.at(-1).title, "Game over");
});

test("a bot whose team was out, or a page that opens after the end, still tells the bot", async (context) => {
  const { player, ends, reads } = relay(context);
  player.receive(stateOf({ generation_id: 9, outcome: OUTCOME, submitted: true, team_color: 4 }));
  await until(() => ends.length === 1);
  assert.equal(ends[0].port, 41414, "the default port, when the state names none");
  assert.equal(reads().length, 0, "the final generation has no turn");
});

test("a player whose bot isn't a localhost bot, or a demo's player, tells nothing at the end", async (context) => {
  for (const bot of [{ kind: "default", level: 2 }, { kind: "hosted", host: "bot.example.com" }, { kind: "uploaded" }, { kind: "manual" }]) {
    const { player, ends, endReads } = relay(context);
    player.receive(stateOf({ bot, outcome: OUTCOME }));
    await delay(20);
    assert.equal(endReads().length, 0, bot.kind);
    assert.equal(ends.length, 0, bot.kind);
  }
  const { player, ends, endReads } = relay(context, { demo: true });
  player.receive(stateOf({ outcome: OUTCOME }));
  await delay(20);
  assert.equal(endReads().length, 0);
  assert.equal(ends.length, 0);
});

test("a failure to tell the bot that the game ended is ignored quietly and not retried", async (context) => {
  const debug = context.mock.method(console, "debug", () => {});
  const cases = [
    { endBot: async () => { throw new BotError("unreachable", "Your bot is not reachable on 127.0.0.1:41414."); } },
    { endBot: async () => { throw new BotError("refused", "Your bot refused the end of the game (HTTP 404)."); } },
    { ended: () => ({ status: 409, body: { type: "urn:island-battles:problem:game-not-ended", detail: "the game has not ended" } }) },
    { ended: () => ({ status: 404, body: { detail: "Not Found" } }) },
  ];
  for (const options of cases) {
    const { player, endReads, statuses } = relay(context, options);
    player.receive(stateOf({ outcome: OUTCOME }));
    await until(() => debug.mock.callCount() === cases.indexOf(options) + 1);
    await delay(30);
    assert.equal(endReads().length, 1);
    assert.equal(statuses.at(-1).title, "Game over");
  }
});

test("another game's game-ended delivery is not posted", async (context) => {
  const { player, ends, endReads } = relay(context, { ended: (state) => ({ status: 200, body: endedOf({ ...state, game_id: 99 }) }) });
  player.receive(stateOf({ outcome: OUTCOME }));
  await until(() => endReads().length === 1);
  await delay(20);
  assert.equal(ends.length, 0);
});
