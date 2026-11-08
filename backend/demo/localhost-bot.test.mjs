import assert from "node:assert/strict";
import test from "node:test";
import { BotError, buildBotTurn, checkBotHealth, localhostBotOrigin, parseBotResult, playTurn } from "./localhost-bot.mjs";

const json = (body, init) => new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" }, ...init });

test("a BotTurn carries the state's board, team, memory, deadline and the upgrades", () => {
  const state = { game_id: 4, generation: { castles: [] }, team_color: 2, memory: { a: 1 }, turn_deadline_at: "2030-01-01T00:00:00Z", generation_id: 7, bot: { kind: "localhost" } };
  assert.deepEqual(buildBotTurn(state, [{ level: 1 }]), {
    game_id: 4, generation: { castles: [] }, memory: { a: 1 }, team_color: 2, turn_deadline_at: "2030-01-01T00:00:00Z", upgrades: [{ level: 1 }],
  });
  assert.deepEqual(buildBotTurn({ ...state, memory: undefined, turn_deadline_at: undefined }, []).memory, null);
  assert.equal(buildBotTurn({ ...state, turn_deadline_at: undefined }, []).turn_deadline_at, null);
});

test("an answer is a BotTurnResult with the optional parts filled", () => {
  assert.deepEqual(parseBotResult({ commands: [] }), { commands: [], logs: [], memory: null });
  assert.deepEqual(parseBotResult({ commands: [{}], logs: [{ message: "x" }], memory: { a: 1 } }), { commands: [{}], logs: [{ message: "x" }], memory: { a: 1 } });
  for (const bad of [null, [], { commands: "no" }, { commands: [], memory: [] }, { commands: [], logs: "no" }]) {
    assert.throws(() => parseBotResult(bad), (error) => error instanceof BotError && error.kind === "invalid");
  }
});

test("a turn is posted as JSON to the bot and its answer returned", async () => {
  const calls = [];
  const result = await playTurn({ game_id: 1 }, { fetch: async (url, init) => { calls.push({ url, init }); return json({ commands: [], memory: { n: 2 } }); } });
  assert.deepEqual(result, { commands: [], logs: [], memory: { n: 2 } });
  assert.equal(calls[0].url, `${localhostBotOrigin}/turn`);
  assert.equal(calls[0].init.method, "POST");
  assert.equal(calls[0].init.headers["Content-Type"], "application/json");
  assert.equal(calls[0].init.body, '{"game_id":1}');
});

test("failures are told apart: not running, crashed with a traceback, too slow, not an answer", async () => {
  const kind = async (fetch) => playTurn({}, { fetch }).then(() => null, (error) => error);
  const unreachable = await kind(async () => { throw new TypeError("Failed to fetch"); });
  assert.equal(unreachable.kind, "unreachable");
  const timeout = await kind(async () => { throw new DOMException("timed out", "TimeoutError"); });
  assert.equal(timeout.kind, "too-slow");
  assert.equal((await kind(async () => new Response("", { status: 504 }))).kind, "too-slow");
  const trace = Array.from({ length: 20 }, (_, index) => `line ${index}`).join("\n") + "\nValueError: boom";
  const crashed = await kind(async () => new Response(trace, { status: 500 }));
  assert.equal(crashed.kind, "crashed");
  assert.match(crashed.message, /ValueError: boom/);
  assert.doesNotMatch(crashed.message, /line 3\b/, "only the last lines are shown");
  assert.equal((await kind(async () => new Response("", { status: 500 }))).message, "Your bot answered HTTP 500.");
  assert.equal((await kind(async () => new Response("not json"))).kind, "invalid");
});

test("the health check says whether the bot's server answers", async () => {
  const urls = [];
  assert.equal(await checkBotHealth({ fetch: async (url) => { urls.push(url); return new Response("ok"); } }), true);
  assert.deepEqual(urls, [`${localhostBotOrigin}/health`]);
  assert.equal(await checkBotHealth({ fetch: async () => new Response("", { status: 500 }) }), false);
  assert.equal(await checkBotHealth({ fetch: async () => { throw new TypeError("Failed to fetch"); } }), false);
});
