import { describe, expect, test } from "bun:test";
import {
  MAP_MESSAGE_TYPE,
  MAX_PREVIEW_COORDINATE,
  mapFromMessage,
  previewGeneration,
  previewKey,
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
  ],
});

describe("previewGeneration", () => {
  test("draws each castle as a game built from the map starts", () => {
    const { generation, teams } = previewGeneration(crossing());
    expect(generation).toEqual({
      id: 0,
      castles: [
        {
          id: 0,
          team_color: 1,
          soldiers: 30,
          upgrades: { speed: 3 },
          x: -5,
          y: 0,
        },
        { id: 1, team_color: 2, soldiers: 30, upgrades: {}, x: 5, y: 0 },
        // No starting soldiers of its own: the map's default.
        { id: 2, team_color: 0, soldiers: 10, upgrades: {}, x: 0, y: 0 },
      ],
      clashes: [],
      soldier_groups: [],
    });
    expect(teams).toEqual([
      { color: 1, name: "Team 1", players: [] },
      { color: 2, name: "Team 2", players: [] },
    ]);
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
        { team_color: 2, x: MAX_PREVIEW_COORDINATE * 2, y: 0 },
        { team_color: 2, x: 4, y: 4, upgrades: [2] },
      ],
    });
    // A castle keeps its index as its ID, so the preview's IDs match the builder's numbers.
    expect(generation.castles).toEqual([
      { id: 1, team_color: 0, soldiers: 0, upgrades: {}, x: 1, y: 2 },
      {
        id: 2,
        team_color: 0,
        soldiers: 0,
        upgrades: { clash_stop_generations: 2 },
        x: 0,
        y: 3,
      },
      { id: 5, team_color: 2, soldiers: 0, upgrades: {}, x: 4, y: 4 },
    ]);
    expect(Object.is(generation.castles[1]?.x, 0)).toBe(true);
    expect(teams.map((team) => team.color)).toEqual([2]);
  });

  test("an empty or broken map is an empty board", () => {
    for (const map of [{}, null, [], "map", { castles: "none" }]) {
      expect(previewGeneration(map).generation.castles).toEqual([]);
    }
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

  test("takes the map the builder posts", () => {
    const map = crossing();
    expect(
      mapFromMessage(message({ type: MAP_MESSAGE_TYPE, map }), parent, origin),
    ).toBe(map);
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
        map.castles[0] = { ...map.castles[0], x: -4 } as never;
      },
      () => {
        map.castles[1] = { ...map.castles[1], team_color: 1 } as never;
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
