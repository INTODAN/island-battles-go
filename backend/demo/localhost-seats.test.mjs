// A player relays the localhost bot seats that they added as the host, each
// to its own port, next to their own bot.
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { setTimeout as delay } from "node:timers/promises";
import test from "node:test";
import { ExamplePlayer } from "./example-player.mjs";
import { BotError } from "./localhost-bot.mjs";

const seats = [
  { player_id: "seat-a", team_color: 2, display_name: "Localhost bot (port 41415)", port: 41415 },
  { player_id: "seat-b", team_color: 3, display_name: "Localhost bot (port 41416)", port: 41416 },
];

const stateOf = (overrides = {}) => ({
  lobby_id: "room", player_id: "host", game_id: 1, generation_id: 1, team_color: 1, submitted: false,
  bot: { kind: "localhost", port: 41414 }, relayed_bots: seats, memory: null, turn_deadline_at: null,
  generation: { castles: [], soldier_groups: [] },
  ...overrides,
});

class Subscription extends EventEmitter {
  subscribe() { this.emit("subscribed"); }
  unsubscribe() {}
}

async function until(predicate) {
  for (let attempt = 0; attempt < 200; attempt++) {
    if (predicate()) return;
    await delay(5);
  }
  assert.fail("expected event did not arrive");
}

// A player whose deliveries are answered by bot(delivery, port), and whose
// turn deliveries name the player that the request asked for.
function host(context, { bot = async () => {}, read = null, endBot = async () => {} } = {}) {
  const reads = [];
  const deliveries = [];
  const endReads = [];
  const ends = [];
  const statuses = [];
  const player = new ExamplePlayer({
    lobbyID: "room", playerID: "host", subscription: new Subscription(), demo: false, botRetryDelay: 20, retryDelay: 20,
    onStatus: (title, detail) => statuses.push({ title, detail }),
    deliver: async (delivery, port) => { deliveries.push({ player: delivery.player, port }); return bot(delivery, port); },
    deliverEnd: async (delivery, port) => { ends.push({ player: delivery.player, port }); return endBot(delivery, port); },
    fetcher: async (url) => {
      if (url.includes("/game-ended-delivery")) {
        const seat = new URL(url, "http://game.test").searchParams.get("player_id");
        endReads.push(seat);
        return { ok: true, status: 200, json: async () => ({ player: seat ?? "host", game_id: player.state.game_id, outcome: player.state.outcome }) };
      }
      if (url.includes("/turn-delivery")) {
        const seat = new URL(url, "http://game.test").searchParams.get("player_id");
        reads.push(seat);
        const answer = (await read?.(seat)) ?? { status: 200 };
        const body = answer.body ?? { player: seat ?? "host", turn: { generation: { id: player.state.generation_id } }, submit: { token: `ibst1.${seat ?? "host"}` } };
        return { ok: answer.status === 200, status: answer.status, json: async () => body };
      }
      return { ok: true, status: 200, json: async () => player.state };
    },
  });
  context.after(() => player.stop());
  return { player, reads, deliveries, endReads, ends, statuses };
}

const byPlayer = (deliveries) => Object.fromEntries(deliveries.map((delivery) => [delivery.player, delivery.port]));

test("each relayed seat's turn is read with its player ID and delivered to its own port", async (context) => {
  const { player, reads, deliveries, statuses } = host(context);
  player.receive(stateOf());
  await until(() => deliveries.length === 3 && !player.inFlight);
  assert.deepEqual(byPlayer(deliveries), { host: 41414, "seat-a": 41415, "seat-b": 41416 });
  assert.deepEqual(reads.sort(), ["seat-a", "seat-b", null].sort());
  await until(() => /Localhost bot \(port 41416\): has this turn/.test(statuses.at(-1).detail));
  const last = statuses.at(-1);
  assert.equal(last.title, "Your bot is playing");
  assert.match(last.detail, /Your bot has this turn/);
  assert.match(last.detail, /Localhost bot \(port 41415\): has this turn and submits it itself\./);
  // The same generation is not delivered again; the next one is, to every bot.
  player.receive(stateOf());
  await delay(30);
  assert.equal(deliveries.length, 3);
  player.receive(stateOf({ generation_id: 2 }));
  await until(() => deliveries.length === 6);
});

test("a host whose own bot the backend plays still relays the seats", async (context) => {
  const { player, deliveries, statuses } = host(context);
  player.receive(stateOf({ bot: { kind: "default", level: 2 } }));
  await until(() => deliveries.length === 2);
  assert.deepEqual(byPlayer(deliveries), { "seat-a": 41415, "seat-b": 41416 });
  await until(() => statuses.at(-1).detail.split("has this turn").length === 3);
  assert.equal(statuses.at(-1).title, "Your localhost bot seats");
});

test("a seat's bot that isn't running is asked again, while the others play", async (context) => {
  let calls = 0;
  const { player, deliveries, statuses } = host(context, {
    bot: async (delivery, port) => {
      if (port === 41416 && ++calls < 3) throw new BotError("unreachable", "Your bot is not reachable on 127.0.0.1:41416.");
    },
  });
  player.receive(stateOf());
  await until(() => calls === 3);
  assert.ok(statuses.some((status) => /Localhost bot \(port 41416\): not running\. Your bot is not reachable on 127\.0\.0\.1:41416\. Trying again…/.test(status.detail)));
  assert.equal(deliveries.filter((delivery) => delivery.port === 41415).length, 1);
  await until(() => /Localhost bot \(port 41416\): has this turn/.test(statuses.at(-1).detail));
});

test("a seat's bot that refuses waits for the next turn, and a seat with nothing to play is left alone", async (context) => {
  const { player, deliveries, statuses } = host(context, {
    bot: async (delivery, port) => {
      if (port === 41415) throw new BotError("refused", "Your bot refused the turn (HTTP 400).");
    },
    read: (seat) => (seat === "seat-b" ? { status: 409, body: { detail: "your team has been eliminated" } } : null),
  });
  player.receive(stateOf());
  await until(() => /port 41415\): refused the turn/.test(statuses.at(-1).detail) && /port 41416\): nothing to play/.test(statuses.at(-1).detail));
  await delay(40);
  assert.equal(deliveries.filter((delivery) => delivery.port === 41415).length, 1);
  assert.equal(deliveries.some((delivery) => delivery.port === 41416), false);
  player.receive(stateOf({ generation_id: 2 }));
  await until(() => deliveries.filter((delivery) => delivery.port === 41415).length === 2);
});

test("a turn that arrives while a seat's read is pending is still sent to the seat's bot", async (context) => {
  let release;
  const held = new Promise((resolve) => { release = resolve; });
  let holding = true;
  const { player, deliveries } = host(context, {
    // seat-a's first read, of turn 1, is answered only once it is released.
    read: async (seat) => {
      if (seat === "seat-a" && holding) {
        holding = false;
        await held;
      }
      return null;
    },
  });
  player.receive(stateOf());
  await until(() => deliveries.some((delivery) => delivery.player === "seat-b"));
  // Turn 2 arrives during turn 1's read of seat-a, which then finishes late.
  player.receive(stateOf({ generation_id: 2 }));
  await until(() => deliveries.filter((delivery) => delivery.player === "seat-b").length === 2);
  assert.equal(deliveries.some((delivery) => delivery.player === "seat-a"), false);
  release();
  await until(() => player.seats.get("seat-a").done === "1:2");
  assert.equal(deliveries.filter((delivery) => delivery.player === "seat-a").length, 1);
});

test("a player without seats relays none", async (context) => {
  const { player, reads, statuses } = host(context);
  player.receive(stateOf({ relayed_bots: [], bot: { kind: "default", level: 1 } }));
  await delay(30);
  assert.deepEqual(reads, []);
  assert.deepEqual(statuses, []);
});

const OUTCOME = { reason: "generation_cap", winner_team_color: null, summary: "A draw" };

test("at the end, the host tells their own bot and each relayed seat's bot, each on its own port, once", async (context) => {
  const { player, endReads, ends, statuses } = host(context);
  player.receive(stateOf());
  await until(() => player.seats.size === 2 && [...player.seats.values()].every((seat) => seat.done === "1:1"));
  player.receive(stateOf({ generation_id: 2, outcome: OUTCOME }));
  await until(() => ends.length === 3);
  assert.deepEqual(endReads.sort(), ["seat-a", "seat-b", null].sort());
  assert.deepEqual(byPlayer(ends), { host: 41414, "seat-a": 41415, "seat-b": 41416 });
  assert.match(statuses.at(-1).detail, /Localhost bot \(port 41415\): game over\./);
  player.receive(stateOf({ generation_id: 2, outcome: OUTCOME }));
  await delay(30);
  assert.equal(ends.length, 3);
});

test("a host whose own bot the backend plays tells only the seats' bots; a seat whose bot is down changes nothing for the others", async (context) => {
  const debug = context.mock.method(console, "debug", () => {});
  const { player, ends, statuses } = host(context, {
    endBot: async (delivery, port) => {
      if (port === 41415) throw new BotError("unreachable", "Your bot is not reachable on 127.0.0.1:41415.");
    },
  });
  player.receive(stateOf({ bot: { kind: "default", level: 2 }, outcome: OUTCOME }));
  await until(() => ends.length === 2 && debug.mock.callCount() === 1);
  assert.deepEqual(byPlayer(ends), { "seat-a": 41415, "seat-b": 41416 });
  await delay(40);
  assert.equal(ends.length, 2, "a failed one is not asked again");
  assert.match(statuses.at(-1).detail, /Localhost bot \(port 41415\): game over\./);
});
