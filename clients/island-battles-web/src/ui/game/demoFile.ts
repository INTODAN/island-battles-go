// Reads a demo file for the game view. The file comes from the player's disk, so it is
// untrusted: `readDemo` checks every value the view draws or writes before anything is shown,
// and throws a `DemoFileError` whose message tells the player what is wrong. This is a port of
// backend/demoviewer/demo-file.mjs against the generated `Demo` type; the messages are the same.

import type { components } from "../../api/generated/game";

export type Demo = components["schemas"]["Demo"];

/** The format and the one schema version this client opens. */
export const DEMO_FORMAT = "island-battles-demo";
export const DEMO_SCHEMA_VERSION = 1;

/**
 * The largest file the view opens. A demo is saved through the lobby page, which reads at most
 * 64 MiB of it, so this leaves room for a demo that was reformatted.
 */
export const maxDemoBytes = 128 * 1024 * 1024;

const logLevels = new Set(["debug", "info", "warning", "error"]);

/**
 * The farthest a position may be from the origin, and the largest radius, in world units. A
 * board this large is already far too large to show; a larger number, such as 1e308, would make
 * the board's size infinite and draw nothing.
 */
export const maxCoordinate = 1e6;

/** Why a file can't be opened. */
export class DemoFileError extends Error {}

/** Returns why a file of `size` bytes is not opened, or null. */
export function fileSizeProblem(size: number): string | null {
  if (size > maxDemoBytes) {
    return `This file has ${Math.ceil(size / 1024 / 1024)} MiB, more than any demo, which has at most ${maxDemoBytes / 1024 / 1024} MiB.`;
  }
  return null;
}

export interface ExpectedDemo {
  format: string;
  schemaVersion: number;
}

const current: ExpectedDemo = {
  format: DEMO_FORMAT,
  schemaVersion: DEMO_SCHEMA_VERSION,
};

/** Parses and checks a demo file's text. Returns the demo, or throws a `DemoFileError`. */
export function readDemo(text: string, expected: ExpectedDemo = current): Demo {
  let demo: unknown;
  try {
    demo = JSON.parse(text);
  } catch {
    throw new DemoFileError(
      "This file is not an Island Battles demo: it is not JSON.",
    );
  }
  if (!isObject(demo) || demo.format !== expected.format) {
    throw new DemoFileError(
      "This file is not an Island Battles demo. Open a file saved with Save demo in a game’s replay.",
    );
  }
  const version = demo.schema_version;
  if (version !== expected.schemaVersion) {
    if (
      typeof version === "number" &&
      Number.isSafeInteger(version) &&
      version > 0
    ) {
      const when = version < expected.schemaVersion ? "an earlier" : "a later";
      throw new DemoFileError(
        `This demo comes from ${when} version of Island Battles (demo format version ${version}). This viewer opens only version ${expected.schemaVersion}.`,
      );
    }
    throw new DemoFileError(
      "This file is not an Island Battles demo: its schema_version is not a version number.",
    );
  }
  try {
    checkDemo(demo);
  } catch (error) {
    if (!(error instanceof Problem)) throw error;
    throw new DemoFileError(
      `This demo is damaged and can’t be shown: ${error.message}.`,
    );
  }
  return demo as unknown as Demo;
}

/** A check that failed, with the location of the value. */
class Problem extends Error {}

type Obj = Record<string, unknown>;

function isObject(value: unknown): value is Obj {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function object(value: unknown, path: string): Obj {
  if (!isObject(value)) throw new Problem(`${path} must be an object`);
  return value;
}

function array(value: unknown, path: string): unknown[] {
  if (!Array.isArray(value)) throw new Problem(`${path} must be a list`);
  return value;
}

function string(value: unknown, path: string): string {
  if (typeof value !== "string") throw new Problem(`${path} must be text`);
  return value;
}

function number(value: unknown, path: string): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new Problem(`${path} must be a number`);
  }
  return value;
}

/** Checks a position that the board can draw. */
function coordinate(value: unknown, path: string): number {
  const checked = number(value, path);
  if (Math.abs(checked) > maxCoordinate) {
    throw new Problem(
      `${path} must be from -${maxCoordinate} to ${maxCoordinate}`,
    );
  }
  return checked;
}

/** Checks a soldier group's clash radius. */
function radius(value: unknown, path: string): number {
  const checked = number(value, path);
  if (!(checked > 0 && checked <= maxCoordinate)) {
    throw new Problem(
      `${path} must be more than 0 and at most ${maxCoordinate}`,
    );
  }
  return checked;
}

/** Checks a whole number from `minimum` up. */
function count(value: unknown, path: string, minimum = 0): number {
  if (
    typeof value !== "number" ||
    !Number.isSafeInteger(value) ||
    value < minimum
  ) {
    throw new Problem(`${path} must be a whole number of at least ${minimum}`);
  }
  return value;
}

function upgrades(value: unknown, path: string): void {
  for (const [type, level] of Object.entries(object(value, path))) {
    count(level, `${path}.${type}`, 1);
  }
}

function checkFrame(rawFrame: unknown, path: string): number {
  const frame = object(rawFrame, path);
  const id = count(frame.id, `${path}.id`, 1);
  array(frame.castles, `${path}.castles`).forEach((rawCastle, index) => {
    const castlePath = `${path}.castles[${index}]`;
    const castle = object(rawCastle, castlePath);
    count(castle.id, `${castlePath}.id`);
    count(castle.team_color, `${castlePath}.team_color`);
    count(castle.soldiers, `${castlePath}.soldiers`);
    coordinate(castle.x, `${castlePath}.x`);
    coordinate(castle.y, `${castlePath}.y`);
    upgrades(castle.upgrades, `${castlePath}.upgrades`);
  });
  array(frame.soldier_groups, `${path}.soldier_groups`).forEach(
    (rawGroup, index) => {
      const groupPath = `${path}.soldier_groups[${index}]`;
      const group = object(rawGroup, groupPath);
      count(group.id, `${groupPath}.id`);
      count(group.team_color, `${groupPath}.team_color`);
      count(group.soldiers, `${groupPath}.soldiers`);
      coordinate(group.x, `${groupPath}.x`);
      coordinate(group.y, `${groupPath}.y`);
      radius(group.clash_radius, `${groupPath}.clash_radius`);
    },
  );
  array(frame.clashes, `${path}.clashes`).forEach((rawClash, index) => {
    const clashPath = `${path}.clashes[${index}]`;
    const clash = object(rawClash, clashPath);
    array(clash.participants, `${clashPath}.participants`).forEach(
      (rawParticipant, participantIndex) => {
        const participantPath = `${clashPath}.participants[${participantIndex}]`;
        const participant = object(rawParticipant, participantPath);
        count(participant.group_id, `${participantPath}.group_id`);
        count(participant.team_color, `${participantPath}.team_color`);
        coordinate(participant.x, `${participantPath}.x`);
        coordinate(participant.y, `${participantPath}.y`);
        radius(participant.clash_radius, `${participantPath}.clash_radius`);
        count(
          participant.soldiers_before,
          `${participantPath}.soldiers_before`,
        );
        count(participant.soldiers_after, `${participantPath}.soldiers_after`);
      },
    );
  });
  return id;
}

function checkLogs(
  rawLogs: unknown,
  path: string,
  frameId: number | undefined,
  index: number,
  teamColors: ReadonlySet<number>,
): void {
  const generationLogs = object(rawLogs, path);
  if (generationLogs.generation_id !== frameId) {
    throw new Problem(
      `${path}.generation_id must be frames[${index}].id, ${frameId}`,
    );
  }
  array(
    generationLogs.hidden_team_colors,
    `${path}.hidden_team_colors`,
  ).forEach((color, colorIndex) => {
    count(color, `${path}.hidden_team_colors[${colorIndex}]`, 1);
  });
  array(generationLogs.players, `${path}.players`).forEach(
    (rawPlayer, playerIndex) => {
      const playerPath = `${path}.players[${playerIndex}]`;
      const player = object(rawPlayer, playerPath);
      string(player.display_name, `${playerPath}.display_name`);
      // The panel lists lines under their team.
      const color = count(player.team_color, `${playerPath}.team_color`, 1);
      if (!teamColors.has(color)) {
        throw new Problem(
          `${playerPath}.team_color must be one of the teams' colours`,
        );
      }
      array(player.lines, `${playerPath}.lines`).forEach(
        (rawLine, lineIndex) => {
          const linePath = `${playerPath}.lines[${lineIndex}]`;
          const line = object(rawLine, linePath);
          if (typeof line.level !== "string" || !logLevels.has(line.level)) {
            throw new Problem(
              `${linePath}.level must be debug, info, warning or error`,
            );
          }
          string(line.logger, `${linePath}.logger`);
          string(line.message, `${linePath}.message`);
        },
      );
    },
  );
}

function checkDemo(demo: Obj): void {
  count(demo.game_id, "game_id", 1);
  if (Number.isNaN(Date.parse(string(demo.saved_at, "saved_at")))) {
    throw new Problem("saved_at must be a date");
  }
  const teamColors = new Set<number>();
  array(demo.teams, "teams").forEach((raw, index) => {
    const path = `teams[${index}]`;
    const team = object(raw, path);
    const color = count(team.color, `${path}.color`, 1);
    if (teamColors.has(color)) {
      throw new Problem(`${path}.color repeats team colour ${color}`);
    }
    teamColors.add(color);
    string(team.name, `${path}.name`);
    array(team.players, `${path}.players`).forEach((player, playerIndex) => {
      string(player, `${path}.players[${playerIndex}]`);
    });
  });
  if (demo.outcome !== null) {
    const outcome = object(demo.outcome, "outcome");
    string(outcome.reason, "outcome.reason");
    if (outcome.winner_team_color !== null) {
      count(outcome.winner_team_color, "outcome.winner_team_color", 1);
    }
    string(outcome.summary, "outcome.summary");
  }
  const frames = array(demo.frames, "frames");
  if (!frames.length) {
    throw new Problem("frames must hold at least one generation");
  }
  let previousID = 0;
  const frameIDs: number[] = [];
  frames.forEach((rawFrame, index) => {
    const path = `frames[${index}]`;
    const id = checkFrame(rawFrame, path);
    if (id <= previousID) {
      throw new Problem(`${path}.id must follow the generation before it`);
    }
    previousID = id;
    frameIDs.push(id);
  });
  const logs = array(demo.logs, "logs");
  if (logs.length !== frames.length) {
    throw new Problem(
      `logs must hold one entry for each of the ${frames.length} frames, and it holds ${logs.length}`,
    );
  }
  logs.forEach((rawLogs, index) => {
    checkLogs(rawLogs, `logs[${index}]`, frameIDs[index], index, teamColors);
  });
}

/** Sums up an opened demo in one line, and says when it comes from a game that hadn't ended. */
export function describeDemo(demo: Demo): string {
  const savedAt = new Date(demo.saved_at).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });
  const generations =
    demo.frames.length === 1
      ? "1 generation"
      : `${demo.frames.length} generations`;
  const state = demo.outcome
    ? "The game had ended."
    : "Saved during the game, before it ended, so it has no outcome and holds only the saver’s team’s logs.";
  return `Game ${demo.game_id} · ${generations} · saved ${savedAt}. ${state}`;
}

/** Reads a local file: checks its size, then its text. */
export async function readDemoFile(file: File): Promise<Demo> {
  const sizeProblem = fileSizeProblem(file.size);
  if (sizeProblem) throw new DemoFileError(sizeProblem);
  let text: string;
  try {
    text = await file.text();
  } catch {
    throw new DemoFileError("The file could not be read.");
  }
  return readDemo(text);
}
