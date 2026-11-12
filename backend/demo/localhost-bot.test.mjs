import assert from "node:assert/strict";
import test from "node:test";
import { BotError, checkBotHealth, deliverTurn, localhostBotOrigin } from "./localhost-bot.mjs";

const delivery = { turn: { game_id: 1 }, submit: { url: "http://localhost:8888/generation/commands", token: "ibst1.x", deadline_at: null } };

test("a delivery is posted as JSON to the bot, which accepts it with 202", async () => {
  const calls = [];
  const result = await deliverTurn(delivery, { fetch: async (url, init) => { calls.push({ url, init }); return new Response('{"accepted": true}', { status: 202 }); } });
  assert.equal(result, undefined);
  assert.equal(calls[0].url, `${localhostBotOrigin}/turn`);
  assert.equal(calls[0].init.method, "POST");
  assert.equal(calls[0].init.headers["Content-Type"], "application/json");
  assert.deepEqual(JSON.parse(calls[0].init.body), delivery);
});

test("failures are told apart: not running, too slow, refused with the bot's reason, an old SDK", async () => {
  const failure = async (fetch) => deliverTurn(delivery, { fetch }).then(() => null, (error) => error);
  const unreachable = await failure(async () => { throw new TypeError("Failed to fetch"); });
  assert.ok(unreachable instanceof BotError);
  assert.equal(unreachable.kind, "unreachable");
  assert.match(unreachable.message, /not reachable on 127\.0\.0\.1:41414/);
  assert.equal((await failure(async () => { throw new DOMException("timed out", "TimeoutError"); })).kind, "too-slow");
  const lines = Array.from({ length: 20 }, (_, index) => `line ${index}`).join("\n") + "\nsubmit.url https://evil.example.com is not an allowed host";
  const refused = await failure(async () => new Response(lines, { status: 400 }));
  assert.equal(refused.kind, "refused");
  assert.match(refused.message, /^Your bot refused the turn \(HTTP 400\)\./);
  assert.match(refused.message, /not an allowed host/);
  assert.doesNotMatch(refused.message, /line 3\b/, "only the last lines are shown");
  assert.equal((await failure(async () => new Response("", { status: 500 }))).message, "Your bot refused the turn (HTTP 500).");
  // An old SDK answers 200 with a result, or 422 for a body that isn't a BotTurn.
  assert.equal((await failure(async () => new Response("{}", { status: 200 }))).kind, "refused");
  const old = await failure(async () => new Response("the body is not a BotTurn", { status: 422 }));
  assert.match(old.message, /Upgrade its SDK/);
});

test("the health check says whether the bot's server answers", async () => {
  const urls = [];
  assert.equal(await checkBotHealth({ fetch: async (url) => { urls.push(url); return new Response("ok"); } }), true);
  assert.deepEqual(urls, [`${localhostBotOrigin}/health`]);
  assert.equal(await checkBotHealth({ fetch: async () => new Response("", { status: 500 }) }), false);
  assert.equal(await checkBotHealth({ fetch: async () => { throw new TypeError("Failed to fetch"); } }), false);
});
