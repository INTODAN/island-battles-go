import assert from "node:assert/strict";
import test from "node:test";
import { BotError, checkBotHealth, deliverTurn, localhostBotOrigin, nextFreePort, parseBotPort, portProblem, serveCommandFor } from "./localhost-bot.mjs";

const delivery = { turn: { game_id: 1 }, submit: { url: "http://localhost:8888/generation/commands", token: "ibst1.x", deadline_at: null } };

test("a delivery is posted as JSON to the bot, which accepts it with 202", async () => {
  const calls = [];
  const result = await deliverTurn(delivery, { fetch: async (url, init) => { calls.push({ url, init }); return new Response('{"accepted": true}', { status: 202 }); } });
  assert.equal(result, undefined);
  assert.equal(calls[0].url, "http://127.0.0.1:41414/turn");
  assert.equal(calls[0].init.method, "POST");
  assert.equal(calls[0].init.headers["Content-Type"], "application/json");
  assert.deepEqual(JSON.parse(calls[0].init.body), delivery);
});

test("a bot on another port gets its turns there", async () => {
  const urls = [];
  await deliverTurn(delivery, { port: 41415, fetch: async (url) => { urls.push(url); return new Response("", { status: 202 }); } });
  assert.deepEqual(urls, ["http://127.0.0.1:41415/turn"]);
  assert.equal(localhostBotOrigin(50000), "http://127.0.0.1:50000");
  const down = await deliverTurn(delivery, { port: 41415, fetch: async () => { throw new TypeError("Failed to fetch"); } }).catch((error) => error);
  assert.match(down.message, /not reachable on 127\.0\.0\.1:41415\. Start it with island-battles-bot serve --port 41415\./);
});

test("failures are told apart: not running, too slow, refused with the bot's reason, an old SDK", async () => {
  const failure = async (fetch) => deliverTurn(delivery, { fetch }).then(() => null, (error) => error);
  const unreachable = await failure(async () => { throw new TypeError("Failed to fetch"); });
  assert.ok(unreachable instanceof BotError);
  assert.equal(unreachable.kind, "unreachable");
  assert.match(unreachable.message, /not reachable on 127\.0\.0\.1:41414\. Start it with island-battles-bot serve\./);
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
  assert.match(old.message, /its SDK is older than the game.*uv sync --upgrade-package island-battles-python/);
  assert.match(old.message, /go get github\.com\/INTODAN\/island-battles-go\/clients\/island-battles-go@latest /);
});

test("the health check says whether the bot's server answers, on its port", async () => {
  const urls = [];
  assert.equal(await checkBotHealth({ fetch: async (url) => { urls.push(url); return new Response("ok"); } }), true);
  assert.equal(await checkBotHealth({ port: 41416, fetch: async (url) => { urls.push(url); return new Response("ok"); } }), true);
  assert.deepEqual(urls, ["http://127.0.0.1:41414/health", "http://127.0.0.1:41416/health"]);
  assert.equal(await checkBotHealth({ fetch: async () => new Response("", { status: 500 }) }), false);
  assert.equal(await checkBotHealth({ fetch: async () => { throw new TypeError("Failed to fetch"); } }), false);
});

test("a port is a whole number from 1024 to 65535, and each of a player's bots needs its own", () => {
  assert.equal(parseBotPort("41415"), 41415);
  assert.equal(parseBotPort(" 1024 "), 1024);
  assert.equal(parseBotPort(65535), 65535);
  for (const invalid of ["", "80", "1023", "65536", "4141.5", "41 415", "-41415", "0x1000", "port", null, undefined]) {
    assert.equal(parseBotPort(invalid), null, String(invalid));
  }
  assert.equal(portProblem("41415", [41414]), null);
  assert.match(portProblem("41414", [41414]), /Port 41414 is taken by another of your localhost bots/);
  assert.match(portProblem("80"), /from 1024 to 65535/);
  assert.equal(nextFreePort([]), 41414);
  assert.equal(nextFreePort([41414, 41415, 41417]), 41416);
});

test("the serve command names the port unless it is the default one", () => {
  assert.equal(serveCommandFor(), "uv run island-battles-bot serve mybot:play");
  assert.equal(serveCommandFor(41414), "uv run island-battles-bot serve mybot:play");
  assert.equal(serveCommandFor(41415), "uv run island-battles-bot serve --port 41415 mybot:play");
});
