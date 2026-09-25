import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { setTimeout as delay } from "node:timers/promises";
import test from "node:test";
import { ExamplePlayer, singleSoldierCommands } from "./example-player.mjs";

const initial = () => ({
  lobby_id: "room", player_id: "player", game_id: 1, generation_id: 1, team_color: 1, submitted: false,
  generation: { castles: [
    { id: 2, team_color: 2, soldiers: 30 },
    { id: 0, team_color: 1, soldiers: 1 },
    { id: 1, team_color: 2, soldiers: 30 },
  ], soldier_groups: [] },
});

class Subscription extends EventEmitter {
  subscribe() { this.emit("subscribed"); }
  unsubscribe() {}
  publish(state) { this.emit("publication", { data: { schema_version: 1, type: "game.next_generation", state } }); }
}

async function until(predicate) {
  for (let attempt = 0; attempt < 100; attempt++) {
    if (predicate()) return;
    await delay(5);
  }
  assert.fail("expected event did not arrive");
}

test("one soldier from the first available owned castle to the first enemy", () => {
  assert.deepEqual(singleSoldierCommands(initial()), [{ command_type: "send_soldier_group", parameters: {
    origin_persistent_castle_id: 0, destination_persistent_castle_id: 1, soldier_amount: 1,
  } }]);
  const empty = initial();
  empty.generation.castles[1].soldiers = 0;
  assert.deepEqual(singleSoldierCommands(empty), []);
  assert.deepEqual(singleSoldierCommands({ ...initial(), generation: { castles: [{ id: 0, team_color: 1, soldiers: 1 }] } }), []);
});

test("push-driven turns deduplicate events; paused idle players do not poll", async (context) => {
  const subscription = new Subscription();
  let reads = 0;
  const commands = [];
  let state = initial();
  const player = new ExamplePlayer({ lobbyID: "room", playerID: "player", credential: "test", subscription,
    fetcher: async (url, options) => {
      if (options.method === "GET") reads++;
      else { commands.push(JSON.parse(options.body)); state = { ...state, submitted: true }; }
      return { ok: true, json: async () => state };
    },
  });
  context.after(() => player.stop());
  player.start();
  await until(() => commands.length === 1 && !player.inFlight);
  subscription.publish(initial()); // Late initial event cannot reopen the turn.
  subscription.publish({ ...initial(), lobby_id: "another-room", generation_id: 9 });
  await delay(30);
  assert.equal(reads, 1);
  assert.equal(commands.length, 1);
  player.setEnabled(false);
  state = { ...initial(), generation_id: 2 };
  subscription.publish(state);
  await delay(30);
  assert.equal(reads, 1);
  assert.equal(commands.length, 1);
  player.setEnabled(true);
  await until(() => commands.length === 2 && !player.inFlight);
  assert.equal(commands[1].generation_id, 2);
  assert.equal(commands[1].complete, true);
  subscription.emit("subscribed"); // Reconnect authorizes a fresh snapshot once.
  await until(() => reads === 2);
  await delay(30);
  assert.equal(reads, 2);
  assert.equal(commands.length, 2);
});

test("lost command responses retry the same UUID and body without polling", async (context) => {
  const subscription = new Subscription();
  const attempts = [];
  let reads = 0;
  const player = new ExamplePlayer({ lobbyID: "room", playerID: "player", credential: "test", subscription, retryDelay: 1,
    fetcher: async (url, options) => {
      if (options.method === "GET") { reads++; return { ok: true, json: async () => initial() }; }
      attempts.push(options.body);
      if (attempts.length === 1) throw new Error("response lost");
      return { ok: true, json: async () => ({ ...initial(), submitted: true }) };
    },
  });
  context.after(() => player.stop());
  player.start();
  await until(() => attempts.length === 2 && !player.inFlight);
  assert.equal(attempts[0], attempts[1]);
  assert.equal(reads, 1);
});

test("new generations submit immediately, with no concurrent turn requests", async (context) => {
  const subscription = new Subscription();
  const commands = [];
  let releaseFirst;
  const firstResponse = new Promise((resolve) => { releaseFirst = resolve; });
  const player = new ExamplePlayer({ lobbyID: "room", playerID: "player", credential: "test", subscription,
    fetcher: async (url, options) => {
      if (options.method === "GET") return { ok: true, json: async () => initial() };
      const command = JSON.parse(options.body);
      commands.push(command);
      if (commands.length === 1) await firstResponse;
      return { ok: true, json: async () => ({ ...initial(), generation_id: command.generation_id, submitted: true }) };
    },
  });
  context.after(() => player.stop());
  player.receive(initial());
  await Promise.resolve();
  assert.equal(commands.length, 1, "the first turn must not wait for a timer");
  subscription.publish({ ...initial(), generation_id: 2 });
  player.receive({ ...initial(), generation_id: 2 });
  assert.equal(commands.length, 1, "new state must not submit concurrently");
  releaseFirst();
  await until(() => commands.length === 2 && !player.inFlight);
  assert.deepEqual(commands.map((command) => command.generation_id), [1, 2]);
});

test("transport failures back off and pause cancels the pending retry", async (context) => {
  const subscription = new Subscription();
  let attempts = 0;
  const player = new ExamplePlayer({ lobbyID: "room", playerID: "player", credential: "test", subscription, retryDelay: 100,
    fetcher: async () => { attempts++; throw new Error("offline"); },
  });
  context.after(() => player.stop());
  player.receive(initial());
  await until(() => attempts === 1 && !player.inFlight);
  await delay(20);
  assert.equal(attempts, 1, "no immediate retry loop");
  player.setEnabled(false);
  await delay(110);
  assert.equal(attempts, 1, "paused retry must not submit");
});
