// Map builder logic without the DOM, so Node can test it.
//
// POST /map/validate is the only judge of whether a map is valid. It runs the
// same strict decoder that reads map files elsewhere, so this module repeats
// none of the map rules: it builds the file, places the server's problems next
// to the castles they name, and allows a download only for the exact text the
// server accepted.

// castleRadius is the replay's visual castle radius, in world units.
export const castleRadius = 0.5;

// Team colours match the replay, so a castle looks the same in a game.
const teamColors = ["#3464e0", "#d94d5b", "#288562", "#9560cb", "#b37914", "#24828f"];
const neutralColor = "#8290a4";

export const teamColor = (team) => Number.isInteger(team) && team >= 1 ? teamColors[(team - 1) % teamColors.length] : neutralColor;

const mapKeys = ["map_name", "player_amount", "plane_size_x", "plane_size_y", "neutral_starting_soldier_amount"];
const castleKeys = ["team_color", "x", "y", "starting_soldier_amount", "upgrades"];

const fieldLabels = {
  map_name: "Map name",
  player_amount: "Players",
  plane_size_x: "Plane width",
  plane_size_y: "Plane height",
  neutral_starting_soldier_amount: "Default soldiers",
  castles: "Castles",
  team_color: "Team",
  x: "X",
  y: "Y",
  starting_soldier_amount: "Starting soldiers",
  upgrades: "Upgrades",
};

// newMap is the map the page starts with: two teams facing each other across
// a neutral castle. It has no name yet, so it can't be downloaded as it is.
export function newMap() {
  return {
    map_name: "",
    player_amount: 2,
    plane_size_x: 16,
    plane_size_y: 10,
    neutral_starting_soldier_amount: 10,
    castles: [
      { team_color: 1, x: -5, y: 0, starting_soldier_amount: 30 },
      { team_color: 2, x: 5, y: 0, starting_soldier_amount: 30 },
      { team_color: 0, x: 0, y: 0 },
    ],
  };
}

const isObject = (value) => typeof value === "object" && value !== null && !Array.isArray(value);
const isSet = (value) => value !== undefined && value !== null;

function castleFile(castle) {
  const file = { team_color: castle.team_color, x: castle.x, y: castle.y };
  if (isSet(castle.starting_soldier_amount)) file.starting_soldier_amount = castle.starting_soldier_amount;
  if (isObject(castle.upgrades)) {
    const types = Object.keys(castle.upgrades).sort();
    if (types.length > 0) file.upgrades = Object.fromEntries(types.map((type) => [type, castle.upgrades[type]]));
  } else if (isSet(castle.upgrades)) {
    file.upgrades = castle.upgrades;
  }
  return file;
}

// mapFile returns the map file object: its keys in a fixed order, and a
// castle's optional keys only when they are set.
export function mapFile(map) {
  const file = {};
  for (const key of mapKeys) file[key] = map[key];
  file.castles = (map.castles ?? []).map(castleFile);
  return file;
}

// serialize returns the text that is both checked and downloaded.
export function serialize(map) {
  return `${JSON.stringify(mapFile(map), null, 2)}\n`;
}

// openMapFile reads a map file for editing. It only needs a castles list to
// draw; every other problem is left for the server to report. Unknown keys
// are dropped, because the editor has nowhere to show them.
export function openMapFile(text) {
  let file;
  try {
    file = JSON.parse(text);
  } catch (error) {
    throw new Error(`The file isn't valid JSON: ${error.message}`, { cause: error });
  }
  if (!isObject(file) || !Array.isArray(file.castles) || !file.castles.every(isObject)) {
    throw new Error("The file isn't a map file: it needs a castles list of objects.");
  }
  const map = {};
  for (const key of mapKeys) if (key in file) map[key] = file[key];
  map.castles = file.castles.map((fileCastle) => {
    const castle = {};
    for (const key of castleKeys) if (key in fileCastle) castle[key] = structuredClone(fileCastle[key]);
    return castle;
  });
  return map;
}

const castleLocation = /^castles\[(\d+)\](?:\.(.+))?$/;

// placeProblems sorts the server's problems by where they belong. A castle's
// problems are keyed by its index, with field "" for the castle as a whole,
// such as a missing key. Everything else belongs to the map, with the field
// "" for the whole file.
export function placeProblems(errors) {
  const placed = { map: [], castles: new Map() };
  for (const error of errors) {
    const location = error.location ?? "";
    const message = error.message || "is invalid";
    const match = castleLocation.exec(location);
    if (!match) {
      placed.map.push({ field: location, message });
      continue;
    }
    const index = Number(match[1]);
    if (!placed.castles.has(index)) placed.castles.set(index, []);
    placed.castles.get(index).push({ field: match[2] ?? "", message });
  }
  return placed;
}

// problemCastle returns the index of the castle a problem names, or null.
export function problemCastle(error) {
  const match = castleLocation.exec(error.location ?? "");
  return match ? Number(match[1]) : null;
}

// withoutCastle keeps the problems of the castles that remain after one is
// deleted, renumbered to their new indexes, until the server checks again.
export function withoutCastle(errors, removed) {
  return errors.flatMap((error) => {
    const index = problemCastle(error);
    if (index === null || index < removed) return [error];
    if (index === removed) return [];
    return [{ ...error, location: error.location.replace(`castles[${index}]`, `castles[${index - 1}]`) }];
  });
}

const words = (key) => key.replaceAll("_", " ").replace(/^./, (first) => first.toUpperCase());

// upgradeLabel names an upgrade type, such as "Clash stop generations".
export const upgradeLabel = (type) => words(type);

// fieldLabel names a field as the page shows it.
export function fieldLabel(field) {
  if (field in fieldLabels) return fieldLabels[field];
  if (field.startsWith("upgrades.")) return `${upgradeLabel(field.slice("upgrades.".length))} upgrade`;
  return field;
}

// describeProblems lists every problem in one line each, in the server's order.
export function describeProblems(errors) {
  return errors.map((error) => {
    const location = error.location ?? "";
    const message = error.message || "is invalid";
    const match = castleLocation.exec(location);
    if (match) return `Castle ${match[1]}${match[2] ? ` · ${fieldLabel(match[2])}` : ""}: ${message}`;
    return location ? `${fieldLabel(location)}: ${message}` : message;
  });
}

// missingTeams lists the team colours from 1 to player_amount that own no
// castle. Lobbies are expected to need a castle for every team, so the page
// hints at them without blocking the download.
export function missingTeams(map) {
  const players = map.player_amount;
  if (!Number.isInteger(players) || players < 1) return [];
  const owned = new Set((map.castles ?? []).map((castle) => castle.team_color));
  const missing = [];
  for (let team = 1; team <= Math.min(players, 1000); team++) if (!owned.has(team)) missing.push(team);
  return missing;
}

const planeHalf = (size) => (typeof size === "number" && size > 0 ? size / 2 : Infinity);
const snap = (value) => Math.round(value * 10) / 10 + 0;

// dragPosition is where a dragged castle lands: on a 0.1 grid, kept on the
// plane so a drag can't lose a castle off its edge. Typing a position or
// resizing the plane can still put a castle outside, and the server reports
// that.
export function dragPosition(map, x, y) {
  const halfWidth = planeHalf(map.plane_size_x);
  const halfHeight = planeHalf(map.plane_size_y);
  const clamp = (value, half) => Math.min(half, Math.max(-half, value));
  return { x: clamp(snap(x), Math.floor(halfWidth * 10) / 10), y: clamp(snap(y), Math.floor(halfHeight * 10) / 10) };
}

// freeSpot finds a whole-number position on the plane, nearest the centre,
// that no castle is drawn over.
export function freeSpot(map) {
  const reach = (half) => Math.floor(Math.min(half, 25));
  const halfWidth = reach(planeHalf(map.plane_size_x));
  const halfHeight = reach(planeHalf(map.plane_size_y));
  const spots = [];
  for (let x = -halfWidth; x <= halfWidth; x++) for (let y = -halfHeight; y <= halfHeight; y++) spots.push({ x: x + 0, y: y + 0 });
  spots.sort((left, right) => Math.hypot(left.x, left.y) - Math.hypot(right.x, right.y));
  const taken = (spot) => (map.castles ?? []).some((castle) => Math.hypot(castle.x - spot.x, castle.y - spot.y) < castleRadius * 3);
  return spots.find((spot) => !taken(spot)) ?? { x: 0, y: 0 };
}

// fileName names the download after the map.
export function fileName(map) {
  const slug = String(map.map_name ?? "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  return `${slug || "map"}.json`;
}

// checkMap asks the server whether text is a valid map file.
export async function checkMap(text, fetcher = fetch) {
  const response = await fetcher("/map/validate", {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: text,
  });
  const body = await response.json().catch(() => null);
  if (!response.ok || typeof body?.valid !== "boolean" || !Array.isArray(body.errors)) {
    throw new Error(body?.detail || `the server answered ${response.status}`);
  }
  return { valid: body.valid, errors: body.errors };
}

// problemsFor returns the problems to show for text: those of a finished
// check of that same text, or else the ones already shown, which stay until
// the next check answers.
export function problemsFor(text, check, shown) {
  const finished = check?.state === "valid" || check?.state === "invalid";
  return finished && check.text === text ? check.errors : shown;
}

const plural = (count, noun) => `${count} ${noun}${count === 1 ? "" : "s"}`;

// downloadGate decides whether text can be downloaded, given the latest
// check. Only the exact text the server accepted can be.
export function downloadGate(text, check) {
  if (!check || check.text !== text || check.state === "pending") return { ready: false, reason: "Checking the map…" };
  if (check.state === "failed") return { ready: false, reason: `The map can't be downloaded until the server has checked it. ${check.message}` };
  if (check.state === "invalid") return { ready: false, reason: `Fix ${plural(check.errors.length, "problem")} before downloading.` };
  return { ready: true, reason: "" };
}
