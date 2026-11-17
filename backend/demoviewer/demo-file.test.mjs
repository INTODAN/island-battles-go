import assert from "node:assert/strict";
import test from "node:test";
import { DemoFileError, describeDemo, fileSizeProblem, maxDemoBytes, readDemo } from "./demo-file.mjs";

const expected = { format: "island-battles-demo", schemaVersion: 1 };

// demo is a two-generation demo saved by Alex on Blue after the game ended,
// in the Demo schema of backend/api/openapi.json.
function demo() {
  const castle = (id, team_color, soldiers, x) => ({ id, team_color, soldiers, x, y: 0, upgrades: { speed: 1, clash_stop_generations: 1 } });
  return {
    format: "island-battles-demo",
    schema_version: 1,
    saved_at: "2026-10-31T12:00:00Z",
    game_id: 12,
    teams: [{ color: 1, name: "Blue", players: ["Alex"] }, { color: 2, name: "Red", players: ["Sam"] }],
    outcome: { reason: "last_team_standing", winner_team_color: 1, summary: "Blue wins as the last team standing" },
    frames: [
      { id: 41, castles: [castle(0, 1, 30, -2), castle(1, 2, 30, 2)], soldier_groups: [], clashes: [] },
      {
        id: 42, castles: [castle(0, 1, 29, -2), castle(1, 2, 30, 2)],
        soldier_groups: [{ id: 0, team_color: 1, soldiers: 1, x: -1.5, y: 0, origin_castle_id: 0, destination_castle_id: 1, clash_radius: 0.25, upgrades: { speed: 1, clash_stop_generations: 1 } }],
        clashes: [{ id: 0, time: 0.5, participants: [{ group_id: 0, team_color: 1, x: 0, y: 0, clash_radius: 0.25, soldiers_before: 2, soldiers_after: 1 }] }],
      },
    ],
    logs: [
      { generation_id: 41, players: [{ display_name: "Alex", team_color: 1, lines: [{ level: "info", logger: "island_battles.player", message: "attacking castle 1" }] }, { display_name: "Sam", team_color: 2, lines: [] }], hidden_team_colors: [] },
      { generation_id: 42, players: [{ display_name: "Alex", team_color: 1, lines: [] }, { display_name: "Sam", team_color: 2, lines: [] }], hidden_team_colors: [] },
    ],
  };
}

function rejects(file, pattern, name) {
  assert.throws(() => readDemo(typeof file === "string" ? file : JSON.stringify(file), expected), (error) => {
    assert.ok(error instanceof DemoFileError, `${name}: ${error}`);
    assert.match(error.message, pattern, name);
    return true;
  }, name);
}

test("a demo opens as it was saved", () => {
  assert.deepEqual(readDemo(JSON.stringify(demo()), expected), demo());
  // A demo of a running game has no outcome, and the saver's team only.
  const running = { ...demo(), outcome: null };
  running.logs = running.logs.map((logs) => ({ ...logs, players: logs.players.slice(0, 1), hidden_team_colors: [2] }));
  assert.deepEqual(readDemo(JSON.stringify(running), expected), running);
  // Formatting doesn't matter.
  assert.deepEqual(readDemo(JSON.stringify(demo(), null, 2), expected), demo());
});

test("files that aren't demos are refused plainly", () => {
  for (const [name, file] of Object.entries({
    "empty file": "",
    "not JSON": "<html>not a demo</html>",
    "a JSON list": "[]",
    "a JSON string": "\"island-battles-demo\"",
    "null": "null",
  })) rejects(file, /not an Island Battles demo/, name);
  rejects({ map_name: "Islands", castles: [] }, /not an Island Battles demo\. Open a file saved with Save demo/, "a map file");
  rejects({ ...demo(), format: "other-game-demo" }, /not an Island Battles demo/, "another format");
});

test("demos of other format versions are refused with their version", () => {
  rejects({ ...demo(), schema_version: 0 }, /not a version number/, "version 0");
  rejects({ ...demo(), schema_version: 2 }, /a later version of Island Battles \(demo format version 2\)\. This viewer opens only version 1\./, "a later version");
  rejects({ ...demo(), schema_version: "1" }, /not a version number/, "a version as text");
  rejects({ ...demo(), schema_version: undefined }, /not a version number/, "no version");
  const older = { format: "island-battles-demo", schema_version: 1 };
  assert.throws(() => readDemo(JSON.stringify(older), { ...expected, schemaVersion: 2 }), /an earlier version of Island Battles \(demo format version 1\)\. This viewer opens only version 2\./);
});

test("damaged demos name the value that is wrong", () => {
  const damaged = (change) => {
    const file = demo();
    change(file);
    return file;
  };
  const cases = [
    [(file) => { file.game_id = 0; }, /game_id must be a whole number of at least 1/],
    [(file) => { file.saved_at = "yesterday"; }, /saved_at must be a date/],
    [(file) => { delete file.teams; }, /teams must be a list/],
    [(file) => { file.teams[1].color = 1; }, /teams\[1\]\.color repeats team colour 1/],
    [(file) => { file.teams[0].players = [7]; }, /teams\[0\]\.players\[0\] must be text/],
    [(file) => { file.outcome = "Blue wins"; }, /outcome must be an object/],
    [(file) => { file.outcome.summary = null; }, /outcome\.summary must be text/],
    [(file) => { file.frames = []; }, /frames must hold at least one generation/],
    [(file) => { file.frames[1].id = 41; }, /frames\[1\]\.id must follow the generation before it/],
    [(file) => { file.frames[0].castles[1].x = "2"; }, /frames\[0\]\.castles\[1\]\.x must be a number/],
    [(file) => { file.frames[0].castles[0].soldiers = -1; }, /frames\[0\]\.castles\[0\]\.soldiers must be a whole number/],
    [(file) => { file.frames[0].castles[0].upgrades = { speed: 0 }; }, /frames\[0\]\.castles\[0\]\.upgrades\.speed must be a whole number of at least 1/],
    [(file) => { file.frames[0].castles[0].upgrades = null; }, /frames\[0\]\.castles\[0\]\.upgrades must be an object/],
    [(file) => { file.frames[0].castles[1].x = 1e308; }, /frames\[0\]\.castles\[1\]\.x must be from -1000000 to 1000000/],
    [(file) => { file.frames[1].soldier_groups[0].y = -2e6; }, /frames\[1\]\.soldier_groups\[0\]\.y must be from -1000000 to 1000000/],
    [(file) => { file.frames[1].clashes[0].participants[0].clash_radius = 1e7; }, /frames\[1\]\.clashes\[0\]\.participants\[0\]\.clash_radius must be more than 0 and at most 1000000/],
    [(file) => { file.logs[0].players[1].team_color = 3; }, /logs\[0\]\.players\[1\]\.team_color must be one of the teams' colours/],
    [(file) => { file.frames[1].soldier_groups[0].clash_radius = 0; }, /frames\[1\]\.soldier_groups\[0\]\.clash_radius must be more than 0 and at most 1000000/],
    [(file) => { file.frames[1].clashes[0].participants = {}; }, /frames\[1\]\.clashes\[0\]\.participants must be a list/],
    [(file) => { file.frames[1].clashes[0].participants[0].soldiers_after = 1.5; }, /frames\[1\]\.clashes\[0\]\.participants\[0\]\.soldiers_after must be a whole number/],
    [(file) => { file.logs.pop(); }, /logs must hold one entry for each of the 2 frames, and it holds 1/],
    [(file) => { file.logs[1].generation_id = 41; }, /logs\[1\]\.generation_id must be frames\[1\]\.id, 42/],
    [(file) => { file.logs[0].hidden_team_colors = null; }, /logs\[0\]\.hidden_team_colors must be a list/],
    [(file) => { file.logs[0].players[0].lines[0].level = "fatal"; }, /logs\[0\]\.players\[0\]\.lines\[0\]\.level must be debug, info, warning or error/],
    [(file) => { file.logs[0].players[0].lines[0].message = { text: "hi" }; }, /logs\[0\]\.players\[0\]\.lines\[0\]\.message must be text/],
  ];
  for (const [change, pattern] of cases) rejects(damaged(change), new RegExp(`damaged and can’t be shown: ${pattern.source}`), pattern.source);
});

test("markup in a demo stays text", () => {
  // The viewer writes every value with textContent, so the file is accepted
  // as it is and shown as written.
  const file = demo();
  file.teams[0].name = "<img src=x onerror=alert(1)>";
  file.logs[0].players[0].lines[0].message = "</script><script>alert(1)</script>";
  assert.equal(readDemo(JSON.stringify(file), expected).teams[0].name, "<img src=x onerror=alert(1)>");
});

test("files larger than any demo are refused before they are read", () => {
  assert.equal(fileSizeProblem(1024), null);
  assert.equal(fileSizeProblem(maxDemoBytes), null);
  assert.match(fileSizeProblem(maxDemoBytes + 1), /more than any demo/);
});

test("the summary says when a demo comes from an unfinished game", () => {
  assert.match(describeDemo(demo()), /^Game 12 · 2 generations · saved .+\. The game had ended\.$/);
  assert.match(describeDemo({ ...demo(), outcome: null }), /Saved during the game, before it ended/);
});
