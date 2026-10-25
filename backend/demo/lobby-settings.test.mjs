import assert from "node:assert/strict";
import test from "node:test";
import { describeSettings, loadDefaultMaps, readMapFile, teamNames } from "./lobby-settings.mjs";

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
  assert.equal(await describeSettings({}), "Map: generated for the players");
  assert.equal(await describeSettings({ map: { map_name: "Loaded" } }), "Map: Loaded");
  assert.equal(requests, 0);
  // A failed list falls back to the ID and is fetched again next time.
  assert.equal(await describeSettings({ map_template_id: 4 }), "Map: default map #4");
  available = true;
  assert.equal(await describeSettings({ map_template_id: 4 }), "Map: Strait");
  assert.equal(await describeSettings({ map_template_id: 9 }), "Map: default map #9");
  assert.equal((await loadDefaultMaps()).length, 1);
  assert.equal(requests, 2);
});
