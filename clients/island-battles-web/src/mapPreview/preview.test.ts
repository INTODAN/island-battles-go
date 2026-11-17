import { describe, expect, test } from "bun:test";
import { boardBounds } from "../scene/board";
import {
  createRedrawQueue,
  MAP_MESSAGE_TYPE,
  MAX_FIELD_TEXELS,
  MAX_PREVIEW_DISTANCE,
  MOVE_MAX_WAIT_MS,
  MOVE_SETTLE_MS,
  mapFromMessage,
  PREVIEW_TEXELS_PER_UNIT,
  type Preview,
  previewGeneration,
  previewKey,
  previewTexelsPerUnit,
  type Timers,
} from "./preview";

// The map the builder starts with (backend/mapbuilder/builder.mjs, newMap), with upgrades.
const crossing = () => ({
  map_name: "crossing",
  player_amount: 2,
  plane_size_x: 16,
  plane_size_y: 10,
  neutral_starting_soldier_amount: 10,
  castles: [
    {
      team_color: 1,
      x: -5,
      y: 0,
      starting_soldier_amount: 30,
      upgrades: { speed: 3 },
    },
    { team_color: 2, x: 5, y: 0, starting_soldier_amount: 30 },
    { team_color: 0, x: 0, y: 0 },
  ] as Record<string, unknown>[],
});

// The game's upgrade table, as the builder reads it from upgrades.json.
const types = ["clash_stop_generations", "speed"];

describe("previewGeneration", () => {
  test("draws each castle as a game built from the map starts", () => {
    const { generation, teams, tooLarge } = previewGeneration(
      crossing(),
      types,
    );
    expect(tooLarge).toBe(false);
    expect(generation).toEqual({
      id: 0,
      castles: [
        {
          id: 0,
          team_color: 1,
          soldiers: 30,
          upgrades: { clash_stop_generations: 1, speed: 3 },
          x: -5,
          y: 0,
          burst_debt: 0,
        },
        {
          id: 1,
          team_color: 2,
          soldiers: 30,
          upgrades: { clash_stop_generations: 1, speed: 1 },
          x: 5,
          y: 0,
          burst_debt: 0,
        },
        // No starting soldiers of its own: the map's default.
        {
          id: 2,
          team_color: 0,
          soldiers: 10,
          upgrades: { clash_stop_generations: 1, speed: 1 },
          x: 0,
          y: 0,
          burst_debt: 0,
        },
      ],
      clashes: [],
      soldier_groups: [],
    });
    expect(teams).toEqual([
      { color: 1, name: "Team 1", players: [] },
      { color: 2, name: "Team 2", players: [] },
    ]);
  });

  test("gives every type of the upgrade table, whatever it holds", () => {
    // A new type in the table (#281) reaches the preview with no change here.
    const { generation } = previewGeneration(crossing(), [...types, "range"]);
    expect(generation.castles[0]?.upgrades).toEqual({
      clash_stop_generations: 1,
      range: 1,
      speed: 3,
    });
    // Without the table, a castle has only the levels the map sets.
    expect(
      previewGeneration(crossing()).generation.castles[1]?.upgrades,
    ).toEqual({});
  });

  test("names every team of the map, and any other colour a castle has", () => {
    const map = crossing();
    map.player_amount = 4;
    map.castles[2] = { team_color: 7, x: 0, y: 0 };
    expect(previewGeneration(map).teams.map((team) => team.color)).toEqual([
      1, 2, 3, 4, 7,
    ]);
  });

  test("draws what it can of a half-edited map", () => {
    const { generation, teams } = previewGeneration({
      player_amount: null,
      neutral_starting_soldier_amount: -3,
      castles: [
        { team_color: 1, x: null, y: 0 },
        { team_color: 1.5, x: 1, y: 2, starting_soldier_amount: "many" },
        { x: -0, y: 3, upgrades: { speed: "fast", clash_stop_generations: 2 } },
        "not a castle",
        { team_color: 2, x: Number.POSITIVE_INFINITY, y: 0 },
        { team_color: 2, x: 4, y: 4, upgrades: [2] },
      ],
    });
    // A castle keeps its index as its ID, so the preview's IDs match the builder's numbers.
    expect(generation.castles).toEqual([
      {
        id: 1,
        team_color: 0,
        soldiers: 0,
        upgrades: {},
        x: 1,
        y: 2,
        burst_debt: 0,
      },
      {
        id: 2,
        team_color: 0,
        soldiers: 0,
        upgrades: { clash_stop_generations: 2 },
        x: 0,
        y: 3,
        burst_debt: 0,
      },
      {
        id: 5,
        team_color: 2,
        soldiers: 0,
        upgrades: {},
        x: 4,
        y: 4,
        burst_debt: 0,
      },
    ]);
    expect(Object.is(generation.castles[1]?.x, 0)).toBe(true);
    expect(teams.map((team) => team.color)).toEqual([2]);
  });

  test("an empty or broken map is an empty board", () => {
    for (const map of [{}, null, [], "map", { castles: "none" }]) {
      expect(previewGeneration(map).generation.castles).toEqual([]);
    }
  });

  test("refuses a board that reaches too far from the centre", () => {
    const map = crossing();
    map.castles[1] = { team_color: 2, x: MAX_PREVIEW_DISTANCE, y: 0 };
    map.castles[2] = { team_color: 0, x: 0, y: -MAX_PREVIEW_DISTANCE };
    expect(previewGeneration(map).tooLarge).toBe(false);
    for (const [x, y] of [
      [MAX_PREVIEW_DISTANCE + 0.1, 0],
      [0, -999],
      [9999, 9999],
    ]) {
      map.castles[1] = { team_color: 2, x, y };
      const preview = previewGeneration(map);
      expect(preview.tooLarge).toBe(true);
      // Nothing is drawn: no islands, so no shallows texture the size of the board.
      expect(preview.generation.castles).toEqual([]);
    }
  });
});

describe("previewTexelsPerUnit", () => {
  const side = (castles: Preview["generation"]["castles"]) => {
    const bounds = boardBounds(castles);
    return 2 * Math.max(bounds.halfWidth, bounds.halfDepth) + 6;
  };

  test("is coarser than the game's on a usual map", () => {
    const { castles } = previewGeneration(crossing()).generation;
    expect(previewTexelsPerUnit(castles)).toBe(PREVIEW_TEXELS_PER_UNIT);
    expect(PREVIEW_TEXELS_PER_UNIT).toBeLessThan(16);
  });

  test("keeps the largest board's shallows within the texture limit", () => {
    const map = crossing();
    map.castles[1] = {
      team_color: 2,
      x: MAX_PREVIEW_DISTANCE,
      y: -MAX_PREVIEW_DISTANCE,
    };
    const { castles } = previewGeneration(map).generation;
    const texels = previewTexelsPerUnit(castles);
    expect(texels).toBeLessThan(PREVIEW_TEXELS_PER_UNIT);
    expect(side(castles) * texels).toBeLessThanOrEqual(MAX_FIELD_TEXELS + 1e-6);
  });
});

describe("mapFromMessage", () => {
  const parent = { name: "builder" };
  const origin = "https://island-battles.example.org";
  const message = (data: unknown, overrides = {}) => ({
    origin,
    source: parent,
    data,
    ...overrides,
  });

  test("takes the map and upgrade types the builder posts", () => {
    const map = crossing();
    expect(
      mapFromMessage(
        message({
          type: MAP_MESSAGE_TYPE,
          map,
          upgrade_types: ["speed", 3, "clash_stop_generations"],
          moving: true,
        }),
        parent,
        origin,
      ),
    ).toEqual({
      map,
      upgradeTypes: ["speed", "clash_stop_generations"],
      moving: true,
    });
    expect(
      mapFromMessage(message({ type: MAP_MESSAGE_TYPE, map }), parent, origin),
    ).toEqual({ map, upgradeTypes: [], moving: false });
  });

  test("ignores other windows, other origins and other messages", () => {
    const map = crossing();
    const data = { type: MAP_MESSAGE_TYPE, map };
    expect(
      mapFromMessage(
        message(data, { origin: "https://evil.example" }),
        parent,
        origin,
      ),
    ).toBeUndefined();
    expect(
      mapFromMessage(message(data, { source: {} }), parent, origin),
    ).toBeUndefined();
    // A page that isn't in a frame has no builder to listen to.
    expect(
      mapFromMessage(message(data, { source: null }), null, origin),
    ).toBeUndefined();
    expect(
      mapFromMessage(
        message({ type: "island-battles.map-file", map }),
        parent,
        origin,
      ),
    ).toBeUndefined();
    expect(
      mapFromMessage(
        message({ type: MAP_MESSAGE_TYPE, map: "text" }),
        parent,
        origin,
      ),
    ).toBeUndefined();
  });
});

describe("previewKey", () => {
  test("changes only with what the board draws", () => {
    const map = crossing();
    const before = previewKey(previewGeneration(map));
    map.map_name = "renamed";
    map.plane_size_x = 30;
    expect(previewKey(previewGeneration(map))).toBe(before);
    for (const change of [
      () => {
        map.castles[0] = { ...map.castles[0], x: -4 };
      },
      () => {
        map.castles[1] = { ...map.castles[1], team_color: 1 };
      },
      () => {
        map.neutral_starting_soldier_amount = 12;
      },
      () => {
        map.player_amount = 3;
      },
      () => {
        map.castles.pop();
      },
    ]) {
      const previous = previewKey(previewGeneration(map));
      change();
      expect(previewKey(previewGeneration(map))).not.toBe(previous);
    }
  });
});

describe("createRedrawQueue", () => {
  // Fake timers: `advance` runs what is due.
  const fakeTimers = () => {
    let now = 0;
    let next = 0;
    const due = new Map<number, { at: number; callback: () => void }>();
    const timers: Timers = {
      set(callback, milliseconds) {
        next++;
        due.set(next, { at: now + milliseconds, callback });
        return next;
      },
      clear(handle) {
        due.delete(handle as number);
      },
      now: () => now,
    };
    const advance = (milliseconds: number) => {
      const end = now + milliseconds;
      for (;;) {
        const [id, timer] =
          [...due.entries()].sort((a, b) => a[1].at - b[1].at)[0] ?? [];
        if (id === undefined || !timer || timer.at > end) break;
        due.delete(id);
        now = timer.at;
        timer.callback();
      }
      now = end;
    };
    return { timers, advance };
  };
  const at = (x: number, soldiers = 30) => {
    const map = crossing();
    map.castles[0] = {
      team_color: 1,
      x,
      y: 0,
      starting_soldier_amount: soldiers,
    };
    return previewGeneration(map);
  };
  const xOf = (preview: Preview) => preview.generation.castles[0]?.x;

  test("draws the first map, and edits that move no castle, at once", () => {
    const { timers } = fakeTimers();
    const drawn: Preview[] = [];
    const queue = createRedrawQueue((preview) => drawn.push(preview), timers);
    queue.push(at(-5));
    queue.push(at(-5, 45));
    expect(
      drawn.map((preview) => preview.generation.castles[0]?.soldiers),
    ).toEqual([30, 45]);
    expect(queue.pending).toBe(false);
  });

  test("draws typed moves once they settle, with the latest position", () => {
    const { timers, advance } = fakeTimers();
    const drawn: Preview[] = [];
    const queue = createRedrawQueue((preview) => drawn.push(preview), timers);
    queue.push(at(-5));
    // Arrow keys or typing post a map per change.
    for (let step = 1; step <= 5; step++) {
      queue.push(at(-5 + step * 0.1));
      advance(16);
    }
    expect(drawn.length).toBe(1);
    expect(queue.pending).toBe(true);
    // A soldier edit meanwhile waits too, and is drawn with the move.
    queue.push(at(-4.5, 50));
    advance(MOVE_SETTLE_MS - 1);
    expect(drawn.length).toBe(1);
    advance(1);
    expect(drawn.map(xOf)).toEqual([-5, -4.5]);
    expect(drawn[1]?.generation.castles[0]?.soldiers).toBe(50);
    expect(queue.pending).toBe(false);
  });

  test("still redraws now and then during a long run of moves", () => {
    const { timers, advance } = fakeTimers();
    let draws = 0;
    const queue = createRedrawQueue(() => draws++, timers);
    queue.push(at(-5));
    for (let frame = 1; frame * 16 <= MOVE_MAX_WAIT_MS * 1.5; frame++) {
      queue.push(at(-5 + frame * 0.01));
      advance(16);
    }
    // The first map, then a redraw once the moves have gone on for MOVE_MAX_WAIT_MS.
    expect(draws).toBe(2);
    // And the last position once it settles.
    advance(MOVE_SETTLE_MS);
    expect(draws).toBe(3);
    expect(queue.pending).toBe(false);
  });

  test("a drag redraws every MOVE_MAX_WAIT_MS, however slow its steps, and its end at once", () => {
    const { timers, advance } = fakeTimers();
    const drawn: Preview[] = [];
    const queue = createRedrawQueue((preview) => drawn.push(preview), timers);
    queue.push(at(-5));
    // A slow page: the builder posts every 400 ms, longer than MOVE_SETTLE_MS.
    for (let step = 1; step <= 6; step++) {
      queue.push(at(-5 + step * 0.1), true);
      advance(400);
    }
    // 2.4 s of dragging: four redraws at most, not one per step.
    expect(drawn.length).toBeLessThanOrEqual(
      1 + Math.ceil(2400 / MOVE_MAX_WAIT_MS),
    );
    expect(drawn.length).toBeGreaterThan(1);
    // The pointer comes up: the builder posts the map without `moving`, drawn at once.
    queue.push(at(-4.3));
    advance(0);
    expect(xOf(drawn[drawn.length - 1] as Preview)).toBe(-4.3);
    expect(queue.pending).toBe(false);
  });
});
