// The settings form's team count for the generated map (#174), run against a
// minimal stand-in for the DOM.
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
globalThis.fetch = async () => Response.json({ maps: [{ map_template_id: 4, map_name: "Strait", team_count: 3, plane_size_x: 10, plane_size_y: 6, castle_count: 3 }] });

const { SettingsForm, defaultGeneratedTeams, generatedTeamCounts, generatedTeamSelection } = await import("./lobby-settings.mjs");

function form(options = {}) {
  const container = new FakeElement("div");
  const instance = new SettingsForm({ container, ...options });
  return instance;
}

test("the generated map offers 2 to 6 teams and the create form defaults to 2", () => {
  assert.deepEqual(generatedTeamCounts, [2, 3, 4, 5, 6]);
  const created = form();
  assert.deepEqual(created.teams.options.map((option) => option.value), ["2", "3", "4", "5", "6"]);
  assert.equal(created.teams.value, String(defaultGeneratedTeams));
  assert.equal(created.teamsLabel.hidden, false);
  assert.deepEqual(created.choice().teams, ["Blue", "Red"]);
});

test("the chosen number of teams is returned for the generated map", () => {
  const created = form();
  for (const count of generatedTeamCounts) {
    created.teams.value = String(count);
    created.teams.dispatch("change");
    assert.equal(created.choice().teams.length, count);
    assert.match(created.status.text, new RegExp(`^Teams: .*`));
  }
  created.teams.value = "3";
  assert.deepEqual(created.choice().teams, ["Blue", "Red", "Green"]);
  assert.deepEqual(created.choice().settings, { turn_timeout_seconds: 30 });
});

test("the Teams select shows for the generated map only", async () => {
  const created = form();
  await new Promise((resolve) => setTimeout(resolve));
  created.select.value = "template:4";
  created.select.dispatch("change");
  assert.equal(created.teamsLabel.hidden, true);
  assert.equal(created.choice().teams.length, 3);
  created.select.value = "file";
  created.select.dispatch("change");
  assert.equal(created.teamsLabel.hidden, true);
  created.select.value = "";
  created.select.dispatch("change");
  assert.equal(created.teamsLabel.hidden, false);
});

test("the room's form shows the lobby's current number of teams", () => {
  assert.equal(form({ current: {}, teamCount: 4 }).teams.value, "4");
  assert.equal(form({ current: { turn_timeout_seconds: 10 }, teamCount: 6 }).choice().teams.length, 6);
  // A count the form doesn't offer falls back to the default.
  assert.equal(form({ current: {}, teamCount: 9 }).teams.value, "2");
  assert.equal(generatedTeamSelection(5), 5);
  assert.equal(generatedTeamSelection(1), 2);
  assert.equal(generatedTeamSelection(undefined), 2);
});
