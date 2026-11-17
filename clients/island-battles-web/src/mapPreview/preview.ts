import type { components } from "../api/generated/game";
import { boardBounds } from "../scene/board";

// The map builder's live 3D preview (#278), without Babylon, so `bun test` checks it. The map
// builder (backend/mapbuilder) shows map-preview.html in a frame on the same origin and posts it
// the map being edited; this module turns that map into the board's first generation, as a game
// built from the map starts, and decides when to redraw it.

type Generation = components["schemas"]["Generation"];
type GameTeam = components["schemas"]["GameTeam"];
type Castle = components["schemas"]["Castle"];

/**
 * What the builder posts: the map file as `map`, the game's upgrade types as `upgrade_types`, and
 * `moving` while a castle is being dragged. backend/mapbuilder/builder.mjs sends it.
 */
export const MAP_MESSAGE_TYPE = "island-battles.map-preview";

/** What the preview posts to the builder once it listens, so the builder sends its map. */
export const READY_MESSAGE_TYPE = "island-battles.map-preview-ready";

/** What the preview posts to the builder when it can't draw at all, with a `message` to show. */
export const FAILED_MESSAGE_TYPE = "island-battles.map-preview-failed";

/**
 * The preview draws castles at most this far from the centre, in map units, along x or y. The
 * shallows around the islands are a texture as large as the board, and redrawing one much larger
 * blocks the page, the builder's too, since the frame shares its process.
 */
export const MAX_PREVIEW_DISTANCE = 100;

/** At most this many team colours are named, as the builder offers at most 99 teams. */
const MAX_TEAMS = 99;

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const isCount = (value: unknown): value is number =>
  typeof value === "number" && Number.isInteger(value) && value >= 0;

const isCoordinate = (value: unknown): value is number =>
  typeof value === "number" && Number.isFinite(value);

/**
 * A castle's upgrades as a game's first generation has them: every type of the game's upgrade
 * table, at level 1 unless the map sets another (backend/models/upgrades.go, Upgrades.Clone), and
 * any other type the map names, which the server will refuse.
 */
function upgradesOf(
  value: unknown,
  types: readonly string[],
): Record<string, number> {
  const levels: Record<string, number> = {};
  for (const type of types) levels[type] = 1;
  if (!isObject(value)) return levels;
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
  /** A castle lies farther than MAX_PREVIEW_DISTANCE from the centre, so nothing is drawn. */
  tooLarge: boolean;
}

/**
 * The board a map file starts as: each castle with its index as its ID, as in every game built from
 * the map, its team, its upgrades with every type of `upgradeTypes`, and its starting soldiers, or
 * the map's default soldiers when it sets none. The map may be half edited, so the preview draws
 * what it can rather than refusing it: a castle with no usable position is left out, a team that
 * isn't a whole number from 0 up is drawn neutral, and a soldier count that isn't one shows 0.
 * POST /map/validate, not the preview, judges the map. A map with a castle beyond
 * MAX_PREVIEW_DISTANCE is `tooLarge` and has no castles.
 */
export function previewGeneration(
  map: unknown,
  upgradeTypes: readonly string[] = [],
): Preview {
  const file = isObject(map) ? map : {};
  const defaultSoldiers = isCount(file.neutral_starting_soldier_amount)
    ? file.neutral_starting_soldier_amount
    : 0;
  let castles: Castle[] = [];
  let tooLarge = false;
  const entries = Array.isArray(file.castles) ? file.castles : [];
  entries.forEach((entry: unknown, index) => {
    if (!isObject(entry) || !isCoordinate(entry.x) || !isCoordinate(entry.y)) {
      return;
    }
    if (
      Math.abs(entry.x) > MAX_PREVIEW_DISTANCE ||
      Math.abs(entry.y) > MAX_PREVIEW_DISTANCE
    ) {
      tooLarge = true;
    }
    castles.push({
      id: index,
      team_color: isCount(entry.team_color) ? entry.team_color : 0,
      soldiers: isCount(entry.starting_soldier_amount)
        ? entry.starting_soldier_amount
        : defaultSoldiers,
      upgrades: upgradesOf(entry.upgrades, upgradeTypes),
      // `+ 0` turns -0 into 0, as the builder writes positions.
      x: entry.x + 0,
      y: entry.y + 0,
    });
  });
  if (tooLarge) castles = [];

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
    tooLarge,
  };
}

/** The shallows' texture is at most this many texels along a side. */
export const MAX_FIELD_TEXELS = 2048;

/** The preview's shallows are coarser than the game's 16 texels a unit, to redraw faster. */
export const PREVIEW_TEXELS_PER_UNIT = 8;

/**
 * How finely the preview samples the shallows around `castles`, in texels per world unit: at most
 * PREVIEW_TEXELS_PER_UNIT, and fewer on a large board, so the texture stays within MAX_FIELD_TEXELS.
 */
export function previewTexelsPerUnit(castles: readonly Castle[]): number {
  const bounds = boardBounds(castles);
  // The board, plus room for the islands and shallows beyond the outermost castles.
  const side = 2 * Math.max(bounds.halfWidth, bounds.halfDepth) + 6;
  return Math.min(PREVIEW_TEXELS_PER_UNIT, MAX_FIELD_TEXELS / side);
}

/** The parts of a MessageEvent that `mapFromMessage` reads. */
export interface PreviewMessageEvent {
  origin: string;
  source: unknown;
  data: unknown;
}

export interface MapMessage {
  map: Record<string, unknown>;
  upgradeTypes: string[];
  /** A castle is being dragged on the builder's plane. */
  moving: boolean;
}

/**
 * The map and upgrade types a message carries, or undefined for any other message. Only the
 * builder that shows the preview, its parent window on the preview's own origin, may send one.
 */
export function mapFromMessage(
  event: PreviewMessageEvent,
  parent: unknown,
  origin: string,
): MapMessage | undefined {
  if (event.origin !== origin || !parent || event.source !== parent) {
    return undefined;
  }
  const data = event.data;
  if (!isObject(data) || data.type !== MAP_MESSAGE_TYPE) return undefined;
  if (!isObject(data.map)) return undefined;
  const types = Array.isArray(data.upgrade_types) ? data.upgrade_types : [];
  return {
    map: data.map,
    upgradeTypes: types.filter(
      (type): type is string => typeof type === "string",
    ),
    moving: data.moving === true,
  };
}

/**
 * A key for what the board draws of a map: a new key means the islands, castles or labels change.
 * The builder posts on every edit, such as a new map name, which changes nothing on the board.
 */
export function previewKey(preview: Preview): string {
  return JSON.stringify([
    preview.generation.castles,
    preview.teams,
    preview.tooLarge,
  ]);
}

/**
 * A key for where the castles are. Islands, bridges and shallows follow it, and redrawing them is
 * the slow part of a redraw.
 */
export function positionKey(preview: Preview): string {
  return JSON.stringify(
    preview.generation.castles.map((castle) => [castle.id, castle.x, castle.y]),
  );
}

export interface Timers {
  set(callback: () => void, milliseconds: number): unknown;
  clear(handle: unknown): void;
  now(): number;
}

const browserTimers: Timers = {
  set: (callback, milliseconds) => setTimeout(callback, milliseconds),
  clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
  now: () => performance.now(),
};

/** A position change waits this long for the next one, as while a position is typed. */
export const MOVE_SETTLE_MS = 150;
/** A drag, or a run of position changes, that goes on still redraws this often. */
export const MOVE_MAX_WAIT_MS = 600;

/**
 * Decides when to draw the previews the builder posts. The first one, and one whose castles stand
 * where the drawn ones do, such as a new team or soldier count, is drawn at once. One that moves
 * castles waits, and then the latest one is drawn: each redraw of the islands and shallows can take
 * tens of milliseconds, on the builder's thread too, and a drag sends a map every frame. While the
 * builder says a castle is being dragged (`moving`), a move waits up to MOVE_MAX_WAIT_MS, and the end
 * of the drag is drawn at once. Any other move, such as a typed position, waits until the castles
 * have stopped for MOVE_SETTLE_MS, or at most MOVE_MAX_WAIT_MS.
 */
export function createRedrawQueue(
  draw: (preview: Preview) => void,
  timers: Timers = browserTimers,
): {
  push(preview: Preview, moving?: boolean): void;
  readonly pending: boolean;
} {
  let drawnPositions: string | null = null;
  let latest: Preview | null = null;
  let timer: unknown = null;
  let waitingSince = 0;
  let dragging = false;

  const flush = () => {
    timer = null;
    const preview = latest;
    latest = null;
    if (!preview) return;
    drawnPositions = positionKey(preview);
    draw(preview);
  };

  return {
    push(preview, moving = false) {
      const dragEnded = dragging && !moving;
      dragging = moving;
      // The first map is drawn at once, so the preview shows something as soon as it can.
      const positions = positionKey(preview);
      if (
        timer === null &&
        (drawnPositions === null || positions === drawnPositions)
      ) {
        drawnPositions = positions;
        draw(preview);
        return;
      }
      latest = preview;
      const now = timers.now();
      if (timer === null) {
        waitingSince = now;
      } else {
        timers.clear(timer);
      }
      const left = waitingSince + MOVE_MAX_WAIT_MS - now;
      const wait = dragEnded
        ? 0
        : moving
          ? left
          : Math.min(MOVE_SETTLE_MS, left);
      timer = timers.set(flush, Math.max(0, wait));
    },
    get pending() {
      return timer !== null;
    },
  };
}
