import assert from "node:assert/strict";
import test from "node:test";
import {
  checkMap, describeProblems, downloadGate, dragPosition, fileName, freeSpot, lobbyGate, lobbyMessage, lobbyPageFromMessage, mapFile, missingTeams, newMap,
  openerState, openMapFile, placeProblems, problemsFor, serialize, withoutCastle,
} from "./builder.mjs";

const crossing = () => ({
  map_name: "crossing", player_amount: 2, plane_size_x: 8, plane_size_y: 6, neutral_starting_soldier_amount: 10,
  castles: [
    { team_color: 1, x: -3, y: 0, starting_soldier_amount: 30, upgrades: { speed: 2, clash_stop_generations: 3 } },
    { team_color: 2, x: 3, y: 0, starting_soldier_amount: 30 },
    { team_color: 0, x: 0, y: 2.5 },
  ],
});

test("the file has a fixed key order and only the optional keys that are set", () => {
  const map = crossing();
  map.castles[2].starting_soldier_amount = null;
  map.castles[1].upgrades = {};
  assert.deepEqual(Object.keys(mapFile(map)), ["map_name", "player_amount", "plane_size_x", "plane_size_y", "neutral_starting_soldier_amount", "castles"]);
  const [first, second, third] = mapFile(map).castles;
  assert.deepEqual(Object.keys(first), ["team_color", "x", "y", "starting_soldier_amount", "upgrades"]);
  assert.deepEqual(Object.keys(first.upgrades), ["clash_stop_generations", "speed"]);
  assert.deepEqual(second, { team_color: 2, x: 3, y: 0, starting_soldier_amount: 30 });
  assert.deepEqual(third, { team_color: 0, x: 0, y: 2.5 });
  // Cleared required fields stay in the file as null, so the server names them.
  map.plane_size_x = null;
  assert.match(serialize(map), /"plane_size_x": null/);
});

test("opening a downloaded file restores the same map", () => {
  for (const map of [crossing(), newMap()]) {
    const text = serialize(map);
    const opened = openMapFile(text);
    assert.deepEqual(mapFile(opened), mapFile(map));
    assert.equal(serialize(opened), text);
  }
});

test("opening keeps what the server must judge and drops what the editor can't show", () => {
  const opened = openMapFile(JSON.stringify({ $schema: "x", map_name: 4, castles: [{ team_color: "1", x: 1, position: "(1,0)", upgrades: { armour: 2 } }] }));
  assert.deepEqual(opened, { map_name: 4, castles: [{ team_color: "1", x: 1, upgrades: { armour: 2 } }] });
  assert.throws(() => openMapFile("{"), /isn't valid JSON/);
  for (const text of ["[]", "{}", '{"castles":{}}', '{"castles":[1]}']) assert.throws(() => openMapFile(text), /isn't a map file/);
});

// Locations as models.DecodeMapTemplate reports them; the Go tests pin the same format.
const serverErrors = [
  { location: "map_name", message: "must not be blank" },
  { location: "castles[0].team_color", message: "must be 0 for a neutral castle or a team colour from 1 to player_amount (2)" },
  { location: "castles[2].x", message: "must be inside the plane, from -4 to 4" },
  { location: "castles[2].upgrades.speed", message: "invalid upgrade: speed level must be between 1 and 4" },
  { location: "castles[1]", message: "expected required property y to be present" },
  { message: "must be valid JSON: unexpected end of JSON input" },
];

test("problems are placed next to the castle and field they name", () => {
  const placed = placeProblems(serverErrors);
  assert.deepEqual(placed.map, [{ field: "map_name", message: "must not be blank" }, { field: "", message: "must be valid JSON: unexpected end of JSON input" }]);
  assert.deepEqual([...placed.castles.keys()], [0, 2, 1]);
  assert.deepEqual(placed.castles.get(2), [
    { field: "x", message: "must be inside the plane, from -4 to 4" },
    { field: "upgrades.speed", message: "invalid upgrade: speed level must be between 1 and 4" },
  ]);
  assert.deepEqual(placed.castles.get(1), [{ field: "", message: "expected required property y to be present" }]);
  assert.deepEqual(describeProblems(serverErrors), [
    "Map name: must not be blank",
    "Castle 0 · Team: must be 0 for a neutral castle or a team colour from 1 to player_amount (2)",
    "Castle 2 · X: must be inside the plane, from -4 to 4",
    "Castle 2 · Speed upgrade: invalid upgrade: speed level must be between 1 and 4",
    "Castle 1: expected required property y to be present",
    "must be valid JSON: unexpected end of JSON input",
  ]);
});

test("deleting a castle renumbers the problems of the castles after it", () => {
  assert.deepEqual(withoutCastle(serverErrors, 1).map((error) => error.location), [
    "map_name", "castles[0].team_color", "castles[1].x", "castles[1].upgrades.speed", undefined,
  ]);
});

test("only the exact text the server accepted can be downloaded", () => {
  const text = serialize(crossing());
  assert.deepEqual(downloadGate(text, null), { ready: false, reason: "Checking the map…" });
  assert.equal(downloadGate(text, { text, state: "pending" }).ready, false);
  assert.equal(downloadGate(text, { text: `${text} `, state: "valid", errors: [] }).ready, false);
  assert.deepEqual(downloadGate(text, { text, state: "invalid", errors: serverErrors.slice(0, 1) }), { ready: false, reason: "Fix 1 problem before downloading." });
  assert.match(downloadGate(text, { text, state: "invalid", errors: serverErrors }).reason, /Fix 6 problems/);
  assert.match(downloadGate(text, { text, state: "failed", message: "Couldn't reach the server: offline." }).reason, /offline/);
  assert.deepEqual(downloadGate(text, { text, state: "valid", errors: [] }), { ready: true, reason: "" });
});

test("reopening a file that was already checked shows that check's problems again", () => {
  const map = crossing();
  map.castles[1].x = 20;
  const text = serialize(map);
  const errors = serverErrors.slice(2, 3);
  const check = { text, state: "invalid", errors };
  // Opening a file clears the problems on screen; the same text isn't sent again.
  assert.equal(problemsFor(serialize(openMapFile(text)), check, []), errors);
  const shown = [];
  assert.equal(problemsFor(text, { text, state: "pending" }, shown), shown);
  assert.equal(problemsFor(text, { text, state: "failed", errors: [] }, shown), shown);
  assert.equal(problemsFor(`${text} `, check, shown), shown);
});

test("checkMap sends the text and reads the server's verdict", async () => {
  const requests = [];
  const answer = (status, body) => async (url, options) => {
    requests.push({ url, options });
    return { ok: status === 200, status, json: async () => body };
  };
  assert.deepEqual(await checkMap("{}", answer(200, { valid: false, errors: serverErrors })), { valid: false, errors: serverErrors });
  assert.equal(requests[0].url, "/map/validate");
  assert.equal(requests[0].options.method, "POST");
  assert.equal(requests[0].options.body, "{}");
  await assert.rejects(checkMap("", answer(400, { detail: "request body is required" })), /request body is required/);
  await assert.rejects(checkMap("{}", answer(502, null)), /answered 502/);
});

test("team hint lists the teams without a castle", () => {
  assert.deepEqual(missingTeams(crossing()), []);
  assert.deepEqual(missingTeams({ ...crossing(), player_amount: 4 }), [3, 4]);
  assert.deepEqual(missingTeams({ ...crossing(), player_amount: null }), []);
});

test("a drag snaps to a 0.1 grid and stays on the plane", () => {
  const map = crossing();
  assert.deepEqual(dragPosition(map, 1.234, -0.04), { x: 1.2, y: 0 });
  assert.ok(Object.is(dragPosition(map, -0.01, 0).x, 0));
  assert.deepEqual(dragPosition(map, 9, -9), { x: 4, y: -3 });
  assert.deepEqual(dragPosition({ ...map, plane_size_x: 5 }, 9, 0), { x: 2.5, y: 0 });
  // Without a usable plane size there is nothing to keep the castle on.
  assert.deepEqual(dragPosition({ ...map, plane_size_x: null }, 40, 0), { x: 40, y: 0 });
});

test("a new castle goes to the free spot nearest the centre", () => {
  const map = newMap();
  const spot = freeSpot(map);
  assert.deepEqual(spot, { x: -2, y: 0 });
  assert.ok(map.castles.every((castle) => Math.hypot(castle.x - spot.x, castle.y - spot.y) >= 1.5));
  assert.deepEqual(freeSpot({ ...map, castles: [] }), { x: 0, y: 0 });
  // A full plane still offers a spot.
  assert.deepEqual(freeSpot({ ...map, plane_size_x: 1, plane_size_y: 1, castles: [{ x: 0, y: 0 }] }), { x: 0, y: 0 });
});

test("the download is named after the map", () => {
  assert.equal(fileName({ map_name: "Big Crossing #2" }), "big-crossing-2.json");
  assert.equal(fileName({ map_name: "" }), "map.json");
  assert.equal(fileName({}), "map.json");
});

test("a map reaches the lobby only when it can be downloaded and every team owns a castle", () => {
  const map = crossing();
  const text = serialize(map);
  const valid = { text, state: "valid", errors: [] };
  assert.deepEqual(lobbyGate(map, text, valid), { ready: true, reason: "" });
  assert.deepEqual(lobbyGate(map, text, null), { ready: false, reason: "Checking the map…" });
  assert.equal(lobbyGate(map, text, { text, state: "pending" }).ready, false);
  assert.equal(lobbyGate(map, text, { text: `${text} `, state: "valid", errors: [] }).ready, false);
  assert.match(lobbyGate(map, text, { text, state: "invalid", errors: serverErrors.slice(0, 1) }).reason, /Fix 1 problem before sending/);
  assert.match(lobbyGate(map, text, { text, state: "failed", message: "Couldn't reach the server: offline." }).reason, /offline/);

  // Download is allowed without a castle for every team; the lobby is not.
  map.castles = map.castles.filter((castle) => castle.team_color !== 2);
  const unevenText = serialize(map);
  const uneven = { text: unevenText, state: "valid", errors: [] };
  assert.equal(downloadGate(unevenText, uneven).ready, true);
  assert.deepEqual(lobbyGate(map, unevenText, uneven), { ready: false, reason: "A lobby needs a castle for every team. Team 2 owns none." });
  map.castles = [];
  assert.match(lobbyGate(map, serialize(map), { text: serialize(map), state: "valid", errors: [] }).reason, /Teams 1, 2 own none/);
});

test("the lobby message holds exactly the accepted text", () => {
  assert.deepEqual(lobbyMessage("{}"), { type: "island-battles.map-file", text: "{}" });
});

test("the builder hands its map only to an open window on its own origin", () => {
  const origin = "https://lobby.example";
  assert.equal(openerState(null, origin), "none");
  assert.equal(openerState(undefined, origin), "none");
  assert.equal(openerState({ closed: false, location: { origin } }, origin), "ready");
  assert.equal(openerState({ closed: true, location: { origin } }, origin), "closed");
  assert.equal(openerState({ closed: false, location: { origin: "https://evil.example" } }, origin), "none");
  const crossOrigin = { get location() { throw new DOMException("blocked", "SecurityError"); } };
  assert.equal(openerState(crossOrigin, origin), "none");
});

test("a reloaded lobby form names itself to the builder, from the builder's own origin only", () => {
  const origin = "https://lobby.example";
  const form = {};
  const self = {};
  const message = (overrides = {}) => ({ origin, source: form, data: { type: "island-battles.lobby-page" }, ...overrides });
  assert.equal(lobbyPageFromMessage(message(), origin, self), form);
  assert.equal(lobbyPageFromMessage(message({ origin: "https://evil.example" }), origin, self), null);
  assert.equal(lobbyPageFromMessage(message({ source: self }), origin, self), null);
  assert.equal(lobbyPageFromMessage(message({ source: null }), origin, self), null);
  assert.equal(lobbyPageFromMessage(message({ data: { type: "island-battles.map-file" } }), origin, self), null);
  assert.equal(lobbyPageFromMessage(message({ data: null }), origin, self), null);
});
