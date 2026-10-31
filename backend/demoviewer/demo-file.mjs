// Reads a demo file for the viewer. The file comes from the player's disk, so
// it is untrusted: readDemo checks every value the viewer draws or writes
// before anything is shown, and throws an Error whose message tells the
// player what is wrong.

// The largest file the viewer opens. A demo is saved through the lobby page,
// which reads at most 64 MiB of it, so this leaves room for a demo that was
// reformatted.
export const maxDemoBytes = 128 * 1024 * 1024;

const logLevels = new Set(["debug", "info", "warning", "error"]);

// DemoFileError says why a file can't be opened.
export class DemoFileError extends Error {}

// fileSizeProblem returns why a file of size bytes is not opened, or null.
export function fileSizeProblem(size) {
  if (size > maxDemoBytes) return `This file has ${Math.ceil(size / 1024 / 1024)} MiB, more than any demo, which has at most ${maxDemoBytes / 1024 / 1024} MiB.`;
  return null;
}

// readDemo parses and checks a demo file's text. expected names the format
// and the one schema version this viewer opens. It returns the demo, or
// throws a DemoFileError.
export function readDemo(text, expected) {
  let demo;
  try {
    demo = JSON.parse(text);
  } catch {
    throw new DemoFileError("This file is not an Island Battles demo: it is not JSON.");
  }
  if (!isObject(demo) || demo.format !== expected.format) {
    throw new DemoFileError("This file is not an Island Battles demo. Open a file saved with Save demo in a game’s replay.");
  }
  const version = demo.schema_version;
  if (version !== expected.schemaVersion) {
    if (Number.isSafeInteger(version) && version > 0) {
      const when = version < expected.schemaVersion ? "an earlier" : "a later";
      throw new DemoFileError(`This demo comes from ${when} version of Island Battles (demo format version ${version}). This viewer opens only version ${expected.schemaVersion}.`);
    }
    throw new DemoFileError("This file is not an Island Battles demo: its schema_version is not a version number.");
  }
  try {
    checkDemo(demo);
  } catch (error) {
    if (!(error instanceof Problem)) throw error;
    throw new DemoFileError(`This demo is damaged and can’t be shown: ${error.message}.`);
  }
  return demo;
}

// Problem is a check that failed, with the location of the value.
class Problem extends Error {}

function isObject(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function object(value, path) {
  if (!isObject(value)) throw new Problem(`${path} must be an object`);
  return value;
}

function array(value, path) {
  if (!Array.isArray(value)) throw new Problem(`${path} must be a list`);
  return value;
}

function string(value, path) {
  if (typeof value !== "string") throw new Problem(`${path} must be text`);
  return value;
}

function number(value, path) {
  if (typeof value !== "number" || !Number.isFinite(value)) throw new Problem(`${path} must be a number`);
  return value;
}

// count checks a whole number from minimum up.
function count(value, path, minimum = 0) {
  if (!Number.isSafeInteger(value) || value < minimum) throw new Problem(`${path} must be a whole number of at least ${minimum}`);
  return value;
}

function upgrades(value, path) {
  for (const [type, level] of Object.entries(object(value, path))) count(level, `${path}.${type}`, 1);
}

function checkDemo(demo) {
  count(demo.game_id, "game_id", 1);
  if (Number.isNaN(Date.parse(string(demo.saved_at, "saved_at")))) throw new Problem("saved_at must be a date");
  const teamColors = new Set();
  array(demo.teams, "teams").forEach((team, index) => {
    const path = `teams[${index}]`;
    object(team, path);
    const color = count(team.color, `${path}.color`, 1);
    if (teamColors.has(color)) throw new Problem(`${path}.color repeats team colour ${color}`);
    teamColors.add(color);
    string(team.name, `${path}.name`);
    array(team.players, `${path}.players`).forEach((player, playerIndex) => string(player, `${path}.players[${playerIndex}]`));
  });
  if (demo.outcome !== null) {
    const outcome = object(demo.outcome, "outcome");
    string(outcome.reason, "outcome.reason");
    if (outcome.winner_team_color !== null) count(outcome.winner_team_color, "outcome.winner_team_color", 1);
    string(outcome.summary, "outcome.summary");
  }
  const frames = array(demo.frames, "frames");
  if (!frames.length) throw new Problem("frames must hold at least one generation");
  let previousID = 0;
  frames.forEach((frame, index) => {
    const path = `frames[${index}]`;
    object(frame, path);
    const id = count(frame.id, `${path}.id`, 1);
    if (id <= previousID) throw new Problem(`${path}.id must follow the generation before it`);
    previousID = id;
    array(frame.castles, `${path}.castles`).forEach((castle, castleIndex) => {
      const castlePath = `${path}.castles[${castleIndex}]`;
      object(castle, castlePath);
      count(castle.id, `${castlePath}.id`);
      count(castle.team_color, `${castlePath}.team_color`);
      count(castle.soldiers, `${castlePath}.soldiers`);
      number(castle.x, `${castlePath}.x`);
      number(castle.y, `${castlePath}.y`);
      upgrades(castle.upgrades, `${castlePath}.upgrades`);
    });
    array(frame.soldier_groups, `${path}.soldier_groups`).forEach((group, groupIndex) => {
      const groupPath = `${path}.soldier_groups[${groupIndex}]`;
      object(group, groupPath);
      count(group.id, `${groupPath}.id`);
      count(group.team_color, `${groupPath}.team_color`);
      count(group.soldiers, `${groupPath}.soldiers`);
      number(group.x, `${groupPath}.x`);
      number(group.y, `${groupPath}.y`);
      if (!(number(group.clash_radius, `${groupPath}.clash_radius`) > 0)) throw new Problem(`${groupPath}.clash_radius must be more than 0`);
    });
    array(frame.clashes, `${path}.clashes`).forEach((clash, clashIndex) => {
      const clashPath = `${path}.clashes[${clashIndex}]`;
      object(clash, clashPath);
      array(clash.participants, `${clashPath}.participants`).forEach((participant, participantIndex) => {
        const participantPath = `${clashPath}.participants[${participantIndex}]`;
        object(participant, participantPath);
        count(participant.group_id, `${participantPath}.group_id`);
        count(participant.team_color, `${participantPath}.team_color`);
        number(participant.x, `${participantPath}.x`);
        number(participant.y, `${participantPath}.y`);
        if (!(number(participant.clash_radius, `${participantPath}.clash_radius`) >= 0)) throw new Problem(`${participantPath}.clash_radius must not be negative`);
        count(participant.soldiers_before, `${participantPath}.soldiers_before`);
        count(participant.soldiers_after, `${participantPath}.soldiers_after`);
      });
    });
  });
  const logs = array(demo.logs, "logs");
  if (logs.length !== frames.length) throw new Problem(`logs must hold one entry for each of the ${frames.length} frames, and it holds ${logs.length}`);
  logs.forEach((generationLogs, index) => {
    const path = `logs[${index}]`;
    object(generationLogs, path);
    if (generationLogs.generation_id !== frames[index].id) throw new Problem(`${path}.generation_id must be frames[${index}].id, ${frames[index].id}`);
    array(generationLogs.hidden_team_colors, `${path}.hidden_team_colors`).forEach((color, colorIndex) => count(color, `${path}.hidden_team_colors[${colorIndex}]`, 1));
    array(generationLogs.players, `${path}.players`).forEach((player, playerIndex) => {
      const playerPath = `${path}.players[${playerIndex}]`;
      object(player, playerPath);
      string(player.display_name, `${playerPath}.display_name`);
      count(player.team_color, `${playerPath}.team_color`, 1);
      array(player.lines, `${playerPath}.lines`).forEach((line, lineIndex) => {
        const linePath = `${playerPath}.lines[${lineIndex}]`;
        object(line, linePath);
        if (!logLevels.has(line.level)) throw new Problem(`${linePath}.level must be debug, info, warning or error`);
        string(line.logger, `${linePath}.logger`);
        string(line.message, `${linePath}.message`);
      });
    });
  });
}

// describeDemo sums up an opened demo in one line, and says when it comes
// from a game that hadn't ended.
export function describeDemo(demo) {
  const savedAt = new Date(demo.saved_at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
  const generations = demo.frames.length === 1 ? "1 generation" : `${demo.frames.length} generations`;
  const state = demo.outcome ? "The game had ended." : "Saved during the game, before it ended, so it has no outcome and holds only the saver’s team’s logs.";
  return `Game ${demo.game_id} · ${generations} · saved ${savedAt}. ${state}`;
}
