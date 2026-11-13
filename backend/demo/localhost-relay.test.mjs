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

function relay(context, { bot = async () => {}, state = stateOf(), delivery = null, ...options } = {}) {
  const subscription = new Subscription();
  const requests = [];
  const deliveries = [];
  const statuses = [];
  let current = state;
  const player = new ExamplePlayer({
    lobbyID: "room", playerID: "player", subscription, demo: false, botRetryDelay: 20, retryDelay: 20,
    onStatus: (title, detail) => statuses.push({ title, detail }),
    deliver: async (sent) => { deliveries.push(sent); return bot(sent); },
    fetcher: async (url, init) => {
      requests.push({ url, method: init.method, headers: init.headers, body: init.body && JSON.parse(init.body) });
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
  return { player, subscription, requests, deliveries, statuses, reads, submissions, deadlines, setState: (next) => { current = next; } };
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
