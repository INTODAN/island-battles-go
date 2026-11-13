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
  replaceChildren(...nodes) {
    this.children = [];
    this.append(...nodes);
  }
  focus() {}
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

function panel({ health = async () => true, refuse = null, serverBots = undefined, refreshSeatOptions = undefined, compute = undefined } = {}) {
  const container = new FakeElement("div");
  const saved = [];
  const copied = [];
  const checks = { count: 0 };
  const instance = new RoomPanel({
    container, lobbyID: "lobby", playerID: "me",
    ...(compute ? { compute } : {}),
    ...(serverBots === undefined ? {} : { options: async () => ({ levels: [1], serverBots }) }),
    ...(refreshSeatOptions ? { refreshSeatOptions } : {}),
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

// The lobby's packages, as the packages panel lists them.
const packages = [
  { package_id: "p-mine", owner_player_id: "me", owner_name: "Alex", language: "python", entry_or_command: "bot:play", size_bytes: 1048576 },
  { package_id: "p-sam", owner_player_id: "sam", owner_name: "Sam", language: "native", entry_or_command: "./bot worker", size_bytes: 2097152 },
];

test("without server bots the select offers no hosted or uploaded bot", async () => {
  const { instance } = panel({ serverBots: false });
  await delay(10);
  instance.listed(packages);
  assert.deepEqual(instance.select.children.map((option) => option.value), ["", "localhost", "level-1"]);
  instance.stopPolling();
});

test("with server bots the select offers your hosted bot and your own packages", async () => {
  let refreshed = 0;
  const { instance, saved } = panel({ serverBots: true, refreshSeatOptions: () => { refreshed++; } });
  await delay(10);
  instance.listed(packages);
  assert.deepEqual(instance.select.children.map((option) => [option.text, option.value]), [
    ["Choose your bot", ""], ["My localhost bot", "localhost"], ["Default bot, level 1", "level-1"],
    ["My hosted bot (URL)", "hosted"], ["Uploaded: python · bot:play · 1.0 MiB", "package-p-mine"],
  ]);
  // The host's Add bot is told that the lobby's packages changed, once.
  instance.listed(packages);
  assert.equal(refreshed, 1);

  await instance.save("package-p-mine");
  assert.deepEqual(saved, [{ bot: { kind: "uploaded", package_id: "p-mine" } }]);
  assert.match(instance.summary.textContent, /Alex \(you\): uploaded bot/);

  // A hosted bot needs its URL first, then saves it.
  instance.url.value = "";
  await instance.save("hosted");
  assert.equal(saved.length, 1);
  assert.match(instance.summary.textContent, /Type your bot's URL first/);
  instance.url.value = " https://bot.example.com/turn ";
  await instance.save("hosted");
  assert.deepEqual(saved[1], { bot: { kind: "hosted", url: "https://bot.example.com/turn" } });
  assert.equal(instance.hosted.hidden, false);
  assert.equal(instance.local.hidden, true);
  assert.equal(instance.select.value, "hosted");
  assert.match(instance.summary.textContent, /Alex \(you\): hosted bot \(bot\.example\.com\)/);
  // The same URL again sends nothing; another one is saved.
  await instance.save("hosted");
  assert.equal(saved.length, 2);
  instance.url.value = "https://other.example.com/turn";
  await instance.save("hosted");
  assert.deepEqual(saved[2], { bot: { kind: "hosted", url: "https://other.example.com/turn" } });
});

test("a chosen package that was replaced stays shown until you choose again", async () => {
  const { instance } = panel({ serverBots: true });
  await delay(10);
  instance.listed(packages);
  instance.update(snapshotWith({ bot: { kind: "uploaded", package_id: "p-old" } }));
  assert.equal(instance.select.value, "package-p-old");
  assert.match(instance.select.children.at(-1).text, /replaced or deleted/);
});

test("once the month's compute is spent, your uploaded bots are greyed out and the panel says why", async () => {
  const { instance } = panel({ serverBots: true, compute: async () => ({ month: "2026-11", used_gb_seconds: 300000, budget_gb_seconds: 300000, available: false }) });
  await delay(10);
  instance.listed(packages);
  instance.update(snapshotWith({ bot: { kind: "uploaded", package_id: "p-old" } }));
  assert.deepEqual(instance.select.children.map((option) => [option.value, option.disabled === true]), [
    ["", false], ["localhost", false], ["level-1", false], ["hosted", false], ["package-p-mine", true], ["package-p-old", true],
  ]);
  assert.equal(instance.spentNote.hidden, false);
  assert.equal(instance.spentNote.textContent, "Uploaded bots have used this month's free compute. They're back on 1 December.");
  instance.stopPolling();
});

test("while the month's compute lasts, nothing is greyed out or said", async () => {
  const { instance } = panel({ serverBots: true, compute: async () => ({ month: "2026-11", used_gb_seconds: 1200, budget_gb_seconds: 300000, available: true }) });
  await delay(10);
  instance.listed(packages);
  assert.ok(instance.select.children.every((option) => option.disabled !== true));
  assert.equal(instance.spentNote.hidden, true);
  instance.stopPolling();
});

// The lobby page's stylesheet is the generic lobby's, which knows nothing of
// bots, so the panel brings the styles of its boxes and adds them only once.
test("the panel adds its styles to the page once", async () => {
  const { addPanelStyles, panelStyles, panelStylesID } = await import("./bot-choice.mjs");
  const head = new FakeElement("head");
  const page = {
    head,
    createElement: (tag) => new FakeElement(tag),
    getElementById: (id) => head.children.find((child) => child.id === id) ?? null,
  };
  addPanelStyles(page);
  addPanelStyles(page);
  assert.equal(head.children.length, 1);
  assert.equal(head.children[0].tag, "style");
  assert.equal(head.children[0].id, panelStylesID);
  assert.equal(head.children[0].textContent, panelStyles);
  assert.match(panelStyles, /\.bot-health\[data-state="connected"\]/);
  // Without a page head, such as in these tests' stand-in, it adds nothing.
  addPanelStyles({ createElement: page.createElement });
});
