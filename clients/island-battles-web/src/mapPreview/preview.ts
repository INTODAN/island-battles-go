import type { components } from "../api/generated/game";

// The map builder's live 3D preview (#278), without Babylon, so `bun test` checks it. The map
// builder (backend/mapbuilder) shows map-preview.html in a frame on the same origin and posts it
// the map being edited; this module turns that map into the board's first generation, as a game
// built from the map starts.

type Generation = components["schemas"]["Generation"];
type GameTeam = components["schemas"]["GameTeam"];
type Castle = components["schemas"]["Castle"];

/** What the builder posts, with the map file as its `map`. backend/mapbuilder/builder.mjs sends it. */
export const MAP_MESSAGE_TYPE = "island-battles.map-preview";

/** What the preview posts to the builder once it listens, so the builder sends its map. */
export const READY_MESSAGE_TYPE = "island-battles.map-preview-ready";

/** Castles farther than this from the origin aren't drawn: the board would have no useful size. */
export const MAX_PREVIEW_COORDINATE = 1_000_000;

/** At most this many team colours are named, as the builder offers at most 99 teams. */
const MAX_TEAMS = 99;

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const isCount = (value: unknown): value is number =>
  typeof value === "number" && Number.isInteger(value) && value >= 0;

const isCoordinate = (value: unknown): value is number =>
  typeof value === "number" &&
  Number.isFinite(value) &&
  Math.abs(value) <= MAX_PREVIEW_COORDINATE;

function upgradesOf(value: unknown): Record<string, number> {
  if (!isObject(value)) return {};
  const levels: Record<string, number> = {};
  for (const [type, level] of Object.entries(value)) {
    if (typeof level === "number" && Number.isFinite(level)) {
      levels[type] = level;
    }
  }
  return levels;
}

export interface Preview {
  generation: Generation;
  teams: GameTeam[];
}

/**
 * The board a map file starts as: each castle with its index as its ID, as in every game built from
 * the map, its team and its starting soldiers, or the map's default soldiers when it sets none. The
 * map may be half edited, so the preview draws what it can rather than refusing it: a castle with no
 * usable position is left out, a team that isn't a whole number from 0 up is drawn neutral, and a
 * soldier count that isn't one shows 0. POST /map/validate, not the preview, judges the map.
 */
export function previewGeneration(map: unknown): Preview {
  const file = isObject(map) ? map : {};
  const defaultSoldiers = isCount(file.neutral_starting_soldier_amount)
    ? file.neutral_starting_soldier_amount
    : 0;
  const castles: Castle[] = [];
  const entries = Array.isArray(file.castles) ? file.castles : [];
  entries.forEach((entry: unknown, index) => {
    if (!isObject(entry) || !isCoordinate(entry.x) || !isCoordinate(entry.y)) {
      return;
    }
    castles.push({
      id: index,
      team_color: isCount(entry.team_color) ? entry.team_color : 0,
      soldiers: isCount(entry.starting_soldier_amount)
        ? entry.starting_soldier_amount
        : defaultSoldiers,
      upgrades: upgradesOf(entry.upgrades),
      // `+ 0` turns -0 into 0, as the builder writes positions.
      x: entry.x + 0,
      y: entry.y + 0,
    });
  });

  const colors = new Set<number>();
  const players = isCount(file.player_amount)
    ? Math.min(file.player_amount, MAX_TEAMS)
    : 0;
  for (let color = 1; color <= players; color++) colors.add(color);
  for (const castle of castles) {
    if (castle.team_color > 0) colors.add(castle.team_color);
  }
  const teams = [...colors]
    .sort((left, right) => left - right)
    .map((color) => ({ color, name: `Team ${color}`, players: [] }));

  return {
    generation: { id: 0, castles, clashes: [], soldier_groups: [] },
    teams,
  };
}

/** The parts of a MessageEvent that `mapFromMessage` reads. */
export interface PreviewMessageEvent {
  origin: string;
  source: unknown;
  data: unknown;
}

/**
 * The map a message carries, or undefined for any other message. Only the builder that shows the
 * preview, its parent window on the preview's own origin, may send one.
 */
export function mapFromMessage(
  event: PreviewMessageEvent,
  parent: unknown,
  origin: string,
): unknown {
  if (event.origin !== origin || !parent || event.source !== parent) {
    return undefined;
  }
  const data = event.data;
  if (!isObject(data) || data.type !== MAP_MESSAGE_TYPE) return undefined;
  return isObject(data.map) ? data.map : undefined;
}

/**
 * A key for what the board draws of a map: a new key means the islands, castles or labels change.
 * The builder posts on every edit, such as a new map name, which changes nothing on the board.
 */
export function previewKey(preview: Preview): string {
  return JSON.stringify([preview.generation.castles, preview.teams]);
}
