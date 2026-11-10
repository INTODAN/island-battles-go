import assert from "node:assert/strict";
import test from "node:test";
import { builderWindowAction, changeSettingsLabel, describeSettings, describeTurnTimeout, loadDefaultMaps, mapFromBuilderMessage, readMapFile, settingsSelection, teamNames, turnTimeouts } from "./lobby-settings.mjs";

test("a map's teams are named after the board's team colours", () => {
  assert.deepEqual(teamNames(2), ["Blue", "Red"]);
  assert.deepEqual(teamNames(7), ["Blue", "Red", "Green", "Purple", "Amber", "Teal", "Team 7"]);
});

test("a map file needs a name and a team count", () => {
  const map = readMapFile('{"map_name":"Strait","player_amount":3,"castles":[]}');
  assert.equal(map.map_name, "Strait");
  assert.throws(() => readMapFile("{"), /not valid JSON/);
  assert.throws(() => readMapFile("[1]"), /one map object/);
  assert.throws(() => readMapFile('{"map_name":"Strait","player_amount":1.5}'), /player_amount/);
  assert.throws(() => readMapFile('{"map_name":" ","player_amount":2}'), /map_name/);
});

test("the room names the chosen map", async (context) => {
  let requests = 0;
  let available = false;
  context.mock.method(globalThis, "fetch", async (url) => {
    requests++;
    assert.equal(url, "/map/templates");
    if (!available) return new Response("", { status: 503 });
    return Response.json({ maps: [{ map_template_id: 4, map_name: "Strait", team_count: 2, plane_size_x: 10, plane_size_y: 6, castle_count: 3 }] });
  });
  assert.equal(await describeSettings({}), "Map: generated for the players · Turn limit: 30 s");
  assert.equal(await describeSettings({ map: { map_name: "Loaded" }, turn_timeout_seconds: 10 }), "Map: Loaded · Turn limit: 10 s");
  assert.equal(requests, 0);
  // A failed list falls back to the ID and is fetched again next time.
  assert.equal(await describeSettings({ map_template_id: 4 }), "Map: default map #4 · Turn limit: 30 s");
  available = true;
  assert.equal(await describeSettings({ map_template_id: 4, turn_timeout_seconds: null }), "Map: Strait · No turn limit");
  assert.equal(await describeSettings({ map_template_id: 9 }), "Map: default map #9 · Turn limit: 30 s");
  assert.equal((await loadDefaultMaps()).length, 1);
  assert.equal(requests, 2);
});

test("the form offers the turn timeouts, and the room names the lobby's", () => {
  assert.deepEqual(turnTimeouts.map(({ seconds }) => seconds), [10, 30, 120, 600, null]);
  assert.deepEqual(turnTimeouts.map(({ label }) => label), ["10 s", "30 s", "2 min", "10 min", "No limit"]);
  // Settings without a timeout get the server's default of 30 seconds.
  assert.equal(describeTurnTimeout({}), "Turn limit: 30 s");
  assert.equal(describeTurnTimeout(null), "Turn limit: 30 s");
  assert.equal(describeTurnTimeout({ turn_timeout_seconds: 120 }), "Turn limit: 2 min");
  assert.equal(describeTurnTimeout({ turn_timeout_seconds: 600 }), "Turn limit: 10 min");
  assert.equal(describeTurnTimeout({ turn_timeout_seconds: 90 }), "Turn limit: 90 s");
  assert.equal(describeTurnTimeout({ turn_timeout_seconds: null }), "No turn limit");
});

test("the form takes a map only from the builder window it opened, on its own origin", () => {
  const builder = {};
  const origin = "https://lobby.example";
  const text = '{"map_name":"Strait","player_amount":2,"castles":[]}';
  const message = (overrides = {}) => ({ origin, source: builder, data: { type: "island-battles.map-file", text }, ...overrides });
  assert.equal(mapFromBuilderMessage(message(), { origin, builder }).map_name, "Strait");
  assert.equal(mapFromBuilderMessage(message({ origin: "https://evil.example" }), { origin, builder }), null);
  assert.equal(mapFromBuilderMessage(message({ source: {} }), { origin, builder }), null);
  assert.equal(mapFromBuilderMessage(message(), { origin, builder: null }), null);
  assert.equal(mapFromBuilderMessage(message({ data: { type: "other", text } }), { origin, builder }), null);
  assert.equal(mapFromBuilderMessage(message({ data: null }), { origin, builder }), null);
  assert.equal(mapFromBuilderMessage(message({ data: "island-battles.map-file" }), { origin, builder }), null);
  // A message that is the builder's but unusable is an error, as a bad file is.
  assert.throws(() => mapFromBuilderMessage(message({ data: { type: "island-battles.map-file" } }), { origin, builder }), /no map file/);
  assert.throws(() => mapFromBuilderMessage(message({ data: { type: "island-battles.map-file", text: "{" } }), { origin, builder }), /not valid JSON/);
});

test("a builder window that is already open is kept, and anything else is sent to the builder", () => {
  const origin = "https://lobby.example";
  const at = (windowOrigin, pathname) => ({ location: { origin: windowOrigin, pathname } });
  assert.equal(builderWindowAction(at(origin, "/map/builder/"), origin), "keep");
  assert.equal(builderWindowAction(at(origin, "/map/builder/index.html"), origin), "keep");
  assert.equal(builderWindowAction(at("null", "blank"), origin), "navigate");
  assert.equal(builderWindowAction(at(origin, "/lobby/ui/"), origin), "navigate");
  assert.equal(builderWindowAction(at("https://other.example", "/map/builder/"), origin), "navigate");
  const crossOrigin = { get location() { throw new DOMException("blocked", "SecurityError"); } };
  assert.equal(builderWindowAction(crossOrigin, origin), "navigate");
});

test("the room's form shows the lobby's current map and turn timeout", () => {
  assert.deepEqual(settingsSelection({}), { map: "", file: null, timeout: "30" });
  assert.deepEqual(settingsSelection(null), { map: "", file: null, timeout: "30" });
  assert.deepEqual(settingsSelection({ map_template_id: 4, turn_timeout_seconds: null }), { map: "template:4", file: null, timeout: "none" });
  const map = { map_name: "Loaded", player_amount: 3 };
  assert.deepEqual(settingsSelection({ map, turn_timeout_seconds: 120 }), { map: "file", file: map, timeout: "120" });
  // A timeout the form doesn't offer, set through the API, is still shown.
  assert.equal(settingsSelection({ turn_timeout_seconds: 45 }).timeout, "45");
  assert.equal(changeSettingsLabel, "Change map");
});
