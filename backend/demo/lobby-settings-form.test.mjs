// The settings form's map choice, run against a minimal stand-in for the DOM.
import assert from "node:assert/strict";
import test from "node:test";

class FakeOption {
  constructor(text, value, defaultSelected = false, selected = false) {
    this.text = text;
    this.value = value;
    this.selected = selected;
  }
}

class FakeElement {
  constructor(tag) {
    this.tag = tag;
    this.children = [];
    this.options = [];
    this.listeners = {};
    this.hidden = false;
    this.text = "";
    this.chosen = null;
  }
  append(...nodes) {
    for (const node of nodes) {
      if (this.tag === "select" && node instanceof FakeOption) this.options.push(node);
      else this.children.push(node);
    }
  }
  insertBefore(node, reference) {
    // The select lists the options of an optgroup beside its own.
    if (this.tag === "select") this.options.splice(this.options.indexOf(reference), 0, ...(node instanceof FakeElement ? node.children : [node]));
    else this.children.push(node);
  }
  removeChild(node) {
    this.options = this.options.filter((option) => option !== node);
    this.children = this.children.filter((child) => child !== node);
  }
  setAttribute() {}
  addEventListener(type, listener) { (this.listeners[type] ||= []).push(listener); }
  dispatch(type) { for (const listener of this.listeners[type] || []) listener(); }
  set textContent(value) { this.text = value; }
  get textContent() { return this.text; }
  get value() {
    if (this.tag !== "select") return this.raw ?? "";
    const chosen = this.options.find((option) => option.value === this.chosen) || this.options.find((option) => option.selected) || this.options[0];
    return chosen ? chosen.value : "";
  }
  set value(value) {
    if (this.tag === "select") this.chosen = String(value);
    else this.raw = value;
  }
}

globalThis.document = { createElement: (tag) => new FakeElement(tag) };
globalThis.Option = FakeOption;
globalThis.window = { addEventListener() {} };
globalThis.location = { origin: "https://lobby.example" };
let defaultMaps = [
  { map_template_id: 4, map_name: "Strait", team_count: 3, plane_size_x: 10, plane_size_y: 6, castle_count: 3 },
  { map_template_id: 7, map_name: "Bay", team_count: 2, plane_size_x: 8, plane_size_y: 6, castle_count: 4 },
];
// Each test reads the maps from a game of its own, since the module loads
// them once per game.
let games = 0;
globalThis.fetch = async () => Response.json({ maps: defaultMaps });

const { SettingsForm } = await import("./lobby-settings.mjs");

function form(options = {}) {
  const container = new FakeElement("div");
  games += 1;
  return new SettingsForm({ container, gameURL: `https://game${games}.example`, ...options });
}

const loaded = () => new Promise((resolve) => setTimeout(resolve));

test("a new lobby starts on the first default map", async () => {
  const created = form();
  assert.equal(created.select.value, "");
  assert.throws(() => created.choice(), /still loading/);
  await loaded();
  assert.equal(created.select.value, "template:4");
  assert.ok(!created.select.options.includes(created.noMap));
  assert.deepEqual(created.choice(), { settings: { map_template_id: 4, turn_timeout_seconds: 30 }, teams: ["Blue", "Red", "Green"] });
  assert.equal(created.status.text, "Teams: Blue, Red, Green.");
});

test("the chosen map sets the teams", async () => {
  const created = form();
  await loaded();
  created.select.value = "template:7";
  created.select.dispatch("change");
  assert.deepEqual(created.choice(), { settings: { map_template_id: 7, turn_timeout_seconds: 30 }, teams: ["Blue", "Red"] });
  created.select.value = "file";
  created.select.dispatch("change");
  assert.throws(() => created.choice(), /Choose a map file/);
});

test("there is no map to choose without default maps or a file", async () => {
  const saved = defaultMaps;
  defaultMaps = [];
  try {
    const created = form();
    await loaded();
    assert.equal(created.select.value, "");
    assert.equal(created.noMap.text, "Pick a map");
    assert.throws(() => created.choice(), /load a map file/);
  } finally {
    defaultMaps = saved;
  }
});

test("the room's form shows the lobby's map, and the first default map for settings without one", async () => {
  const chosen = form({ current: { map_template_id: 7 } });
  await loaded();
  assert.equal(chosen.select.value, "template:7");
  const old = form({ current: { turn_timeout_seconds: 10 } });
  await loaded();
  assert.deepEqual(old.choice(), { settings: { map_template_id: 4, turn_timeout_seconds: 10 }, teams: ["Blue", "Red", "Green"] });
});
