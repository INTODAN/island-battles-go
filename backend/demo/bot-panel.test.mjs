// The bot choice panel, run against a minimal stand-in for the DOM.
import assert from "node:assert/strict";
import { setTimeout as delay } from "node:timers/promises";
import test from "node:test";

class FakeElement {
  constructor(tag) {
    this.tag = tag;
    this.children = [];
    this.listeners = {};
    this.attributes = {};
    this.dataset = {};
    this.hidden = false;
    this.disabled = false;
    this.parent = null;
    this.text = "";
    this.value = "";
  }
  append(...nodes) {
    for (const node of nodes) {
      if (node instanceof FakeElement) node.parent = this;
      this.children.push(node);
    }
  }
  setAttribute(name, value) { this.attributes[name] = value; }
  addEventListener(type, listener) { this.listeners[type] = listener; }
  set textContent(value) { this.text = value; }
  get textContent() { return this.text; }
  closest() {
    for (let element = this; element; element = element.parent) if (element.hidden) return element;
    return null;
  }
}

globalThis.document = { createElement: (tag) => new FakeElement(tag) };
globalThis.Option = class { constructor(text, value) { this.text = text; this.value = value; } };
// The default levels come from GET /bots/defaults.
globalThis.fetch = async () => new Response(JSON.stringify({ bots: [{ level: 1 }, { level: 2 }] }), { headers: { "Content-Type": "application/json" } });

const { RoomPanel, describeHealth, healthPollMilliseconds, safariNote } = await import("./bot-choice.mjs");
const { serveCommand } = await import("./localhost-bot.mjs");

// A lobby snapshot whose one member, the player, has settings.
const snapshotWith = (settings) => ({ members: [{ player_id: "me", display_name: "Alex", kind: "human", seat_settings: null, member_settings: settings }] });

function panel({ health = async () => true, refuse = null } = {}) {
  const container = new FakeElement("div");
  const saved = [];
  const copied = [];
  const checks = { count: 0 };
  const instance = new RoomPanel({
    container, lobbyID: "lobby", playerID: "me",
    saveMemberSettings: async (settings) => {
      saved.push(settings);
      if (refuse) throw new Error(refuse);
      return snapshotWith(settings);
    },
    checkHealth: async () => { checks.count++; return health(); },
    copy: async (text) => { copied.push(text); },
  });
  return { instance, container, saved, copied, checks };
}

test("the select offers the localhost bot and every default level", async () => {
  const { instance } = panel();
  await delay(10);
  assert.deepEqual(instance.select.children.map((option) => [option.text, option.value]), [
    ["Choose your bot", ""], ["My localhost bot", "localhost"], ["Default bot, level 1", "level-1"], ["Default bot, level 2", "level-2"],
  ]);
  instance.stopPolling();
});

test("choosing the localhost bot saves it and shows the indicator, the command and the Safari note", async () => {
  const { instance, saved, copied } = panel({ health: async () => false });
  await instance.save("localhost");
  assert.deepEqual(saved, [{ bot: { kind: "localhost" } }]);
  // Choosing the same bot again sends nothing.
  await instance.save("localhost");
  assert.equal(saved.length, 1);
  assert.equal(instance.local.hidden, false);
  await delay(10);
  assert.equal(instance.health.dataset.state, "stopped");
  assert.equal(instance.health.textContent, describeHealth(false));
  assert.match(instance.health.textContent, /^Not running/);
  assert.ok(instance.local.children.some((child) => child.children.some((node) => node.textContent === serveCommand)));
  assert.ok(instance.local.children.some((child) => child.textContent === safariNote));
  await instance.copyCommand();
  assert.deepEqual(copied, [serveCommand]);
  instance.stopPolling();
});

test("the indicator follows the bot while the panel is shown and stops when it is hidden", async () => {
  let running = false;
  const { instance, container, checks } = panel({ health: async () => running });
  instance.update(snapshotWith({ bot: { kind: "localhost" } }));
  await delay(10);
  assert.equal(instance.health.dataset.state, "stopped");
  running = true;
  await delay(healthPollMilliseconds + 100);
  assert.equal(instance.health.dataset.state, "connected");
  assert.match(instance.health.textContent, /^Connected/);
  container.hidden = true;
  await delay(healthPollMilliseconds + 100);
  const stopped = checks.count;
  await delay(healthPollMilliseconds + 100);
  assert.equal(checks.count, stopped);
  assert.equal(instance.polling, null);
});

test("a default level shows no localhost help and asks nothing of the bot", async () => {
  const { instance, checks } = panel();
  instance.update(snapshotWith({ bot: { kind: "default", level: 2 } }));
  await delay(20);
  assert.equal(instance.local.hidden, true);
  assert.equal(checks.count, 0);
  assert.equal(instance.select.value, "level-2");
  assert.match(instance.summary.textContent, /Alex \(you\): level 2 bot/);
});

test("a refused choice shows the lobby's detail and keeps the saved choice", async () => {
  const { instance, saved } = panel({ refuse: "kind default needs a level that GET /bots/defaults lists: 1, 2, 3" });
  instance.update(snapshotWith(null));
  await instance.save("level-2");
  assert.deepEqual(saved, [{ bot: { kind: "default", level: 2 } }]);
  assert.equal(instance.summary.textContent, "kind default needs a level that GET /bots/defaults lists: 1, 2, 3");
  assert.equal(instance.select.value, "");
  assert.equal(instance.select.disabled, false);
});
