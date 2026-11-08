// A player outside a demo relays the turns of a localhost bot to it.
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { setTimeout as delay } from "node:timers/promises";
import test from "node:test";
import { ExamplePlayer } from "./example-player.mjs";
import { BotError } from "./localhost-bot.mjs";

const DEADLINE = "2030-01-01T00:00:00Z";

const stateOf = (overrides = {}) => ({
  lobby_id: "room", player_id: "player", game_id: 1, generation_id: 1, team_color: 1, submitted: false,
  bot: { kind: "localhost" }, memory: null, turn_deadline_at: null,
  generation: { castles: [{ id: 0, team_color: 1, soldiers: 1 }, { id: 1, team_color: 2, soldiers: 30 }], soldier_groups: [] },
  ...overrides,
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

function relay(context, { bot = async () => ({ commands: [], logs: [], memory: null }), state = stateOf(), ...options } = {}) {
  const subscription = new Subscription();
  const requests = [];
  const turns = [];
  const statuses = [];
  const player = new ExamplePlayer({
    lobbyID: "room", playerID: "player", credential: "test", subscription, demo: false, botRetryDelay: 20,
    onStatus: (title, detail) => statuses.push({ title, detail }),
    playBot: async (turn) => { turns.push(turn); return bot(turn); },
    fetcher: async (url, init) => {
      requests.push({ url, method: init.method, body: init.body && JSON.parse(init.body) });
      if (url === "/game/upgrades") return { ok: true, json: async () => ({ upgrades: [{ level: 1, soldier_cost: 10 }] }) };
      if (init.method === "POST" && !url.endsWith("/deadline")) {
        return { ok: true, json: async () => ({ ...state, generation_id: JSON.parse(init.body).generation_id, submitted: true }) };
      }
      return { ok: true, json: async () => state };
    },
    ...options,
  });
  context.after(() => player.stop());
  const submissions = () => requests.filter((request) => request.url === "/generation/commands");
  const deadlines = () => requests.filter((request) => request.url.endsWith("/deadline"));
  return { player, subscription, requests, turns, statuses, submissions, deadlines };
}

test("a localhost bot gets a BotTurn, and its answer completes the turn with its memory", async (context) => {
  const { player, turns, submissions, requests } = relay(context, {
    bot: async () => ({ commands: [{ command_type: "noop" }], logs: [{ message: "hi" }], memory: { seen: 1 } }),
  });
  player.receive(stateOf({ memory: { seen: 0 }, turn_deadline_at: DEADLINE }));
  await until(() => submissions().length === 1);
  assert.deepEqual(turns[0], {
    game_id: 1, generation: stateOf().generation, memory: { seen: 0 }, team_color: 1,
    turn_deadline_at: DEADLINE, upgrades: [{ level: 1, soldier_cost: 10 }],
  });
  const body = submissions()[0].body;
  assert.equal(body.complete, true);
  assert.equal(body.game_id, 1);
  assert.equal(body.generation_id, 1);
  assert.match(body.request_id, /^[0-9a-f-]{36}$/);
  assert.deepEqual(body.commands, [{ command_type: "noop" }]);
  assert.deepEqual(body.logs, [{ message: "hi" }]);
  assert.deepEqual(body.memory, { seen: 1 });
  assert.equal(requests.filter((request) => request.url === "/game/upgrades").length, 1);
});

test("memory carries over: the next BotTurn holds what the server stored; upgrades are read once", async (context) => {
  const { player, turns, requests, submissions } = relay(context, {
    bot: async (turn) => ({ commands: [], logs: [], memory: { count: (turn.memory?.count || 0) + 1 } }),
  });
  player.receive(stateOf());
  await until(() => submissions().length === 1 && !player.inFlight);
  // The server returns the stored memory with the next generation.
  player.receive(stateOf({ generation_id: 2, memory: submissions()[0].body.memory }));
  await until(() => submissions().length === 2);
  assert.deepEqual(turns.map((turn) => turn.memory), [null, { count: 1 }]);
  assert.deepEqual(submissions()[1].body.memory, { count: 2 });
  assert.equal(requests.filter((request) => request.url === "/game/upgrades").length, 1);
});

test("a generation is relayed once, even when its state arrives again", async (context) => {
  const { player, subscription, turns, submissions } = relay(context);
  player.receive(stateOf());
  subscription.publish(stateOf());
  await until(() => submissions().length === 1 && !player.inFlight);
  subscription.publish(stateOf());
  player.receive(stateOf());
  await delay(30);
  assert.equal(turns.length, 1);
  assert.equal(submissions().length, 1);
});

test("a default bot's player relays and reports nothing", async (context) => {
  const { player, turns, requests, statuses } = relay(context);
  player.receive(stateOf({ bot: { kind: "default", level: 2 } }));
  await delay(30);
  assert.equal(turns.length, 0);
  assert.deepEqual(requests, []);
  assert.deepEqual(statuses, []);
});

test("a default bot's player still asks for the deadline", async (context) => {
  const { player, deadlines } = relay(context, { now: () => Date.parse(DEADLINE) + 5000 });
  player.receive(stateOf({ bot: { kind: "default", level: 2 }, turn_deadline_at: DEADLINE }));
  await until(() => deadlines().length === 1);
});

test("a bot that crashed shows its traceback and submits nothing; the player asks for the deadline", async (context) => {
  const { player, turns, statuses, submissions, deadlines } = relay(context, {
    bot: async () => { throw new BotError("crashed", "Your bot failed:\nTraceback...\nValueError: boom"); },
    now: () => Date.parse(DEADLINE) + 5000,
  });
  player.receive(stateOf({ turn_deadline_at: DEADLINE }));
  await until(() => deadlines().length === 1);
  assert.equal(submissions().length, 0);
  assert.equal(turns.length, 1, "a crashed bot is not asked again for the same turn");
  assert.equal(statuses.at(-1).title, "Your bot failed");
  assert.match(statuses.at(-1).detail, /ValueError: boom/);
  assert.equal(player.pending, null);
});

test("a crashed bot is asked again for the next generation", async (context) => {
  let calls = 0;
  const { player, submissions } = relay(context, {
    bot: async () => {
      if (++calls === 1) throw new BotError("crashed", "Your bot failed:\nboom");
      return { commands: [], logs: [], memory: null };
    },
  });
  player.receive(stateOf());
  await until(() => calls === 1 && !player.inFlight);
  player.receive(stateOf({ generation_id: 2 }));
  await until(() => submissions().length === 1);
  assert.equal(submissions()[0].body.generation_id, 2);
});

test("a bot that is not running is asked again, and its turn is then submitted", async (context) => {
  let calls = 0;
  const { player, statuses, submissions } = relay(context, {
    bot: async () => {
      if (++calls < 3) throw new BotError("unreachable", "Your bot is not reachable on 127.0.0.1:41414.");
      return { commands: [], logs: [], memory: null };
    },
  });
  player.receive(stateOf());
  await until(() => submissions().length === 1);
  assert.equal(calls, 3);
  assert.ok(statuses.some((status) => status.title === "Your bot is not running"));
});

test("a player that submitted asks for the deadline like any other waiting player", async (context) => {
  const { player, submissions, deadlines } = relay(context, { state: stateOf({ turn_deadline_at: DEADLINE }), now: () => Date.parse(DEADLINE) + 5000 });
  player.receive(stateOf({ turn_deadline_at: DEADLINE }));
  await until(() => submissions().length === 1);
  await until(() => deadlines().length >= 1);
});
