import { describe, expect, test } from "bun:test";
import {
  type GenerationState,
  nextGeneration,
  nextSnapshot,
  type Snapshot,
  sameJson,
} from "./install";

const state = (generationId: number, overrides: object = {}) =>
  ({
    game_id: 1,
    generation_id: generationId,
    submitted: false,
    ...overrides,
  }) as unknown as GenerationState;
const snap = (id: string, version: number, extra: object = {}) =>
  ({ lobby: { id, version }, ...extra }) as unknown as Snapshot;

describe("nextGeneration", () => {
  test("installs the first state", () => {
    const first = state(3);
    expect(nextGeneration(null, first)).toBe(first);
  });
  test("accepts gaps in generation IDs", () => {
    expect(nextGeneration(state(3), state(10))?.generation_id).toBe(10);
  });
  test("drops older generations, duplicates and other games", () => {
    expect(nextGeneration(state(3), state(2))).toBeNull();
    expect(nextGeneration(state(3), state(3, { $schema: "x" }))).toBeNull();
    expect(nextGeneration(state(3), state(4, { game_id: 2 }))).toBeNull();
  });
  test("accepts the outcome for the held generation", () => {
    const ended = state(3, { outcome: { reason: "generation_cap" } });
    expect(nextGeneration(state(3), ended)).toBe(ended);
  });
  test("nothing changes after the outcome", () => {
    const ended = state(3, { outcome: { reason: "generation_cap" } });
    expect(nextGeneration(ended, state(3))).toBeNull();
    expect(nextGeneration(ended, state(4))).toBeNull();
  });
  test("keeps submitted for the same generation", () => {
    const next = nextGeneration(
      state(3, { submitted: true }),
      state(3, { log_lines_stored: 2 }),
    );
    expect(next).toMatchObject({ submitted: true, log_lines_stored: 2 });
  });
});

describe("nextSnapshot", () => {
  test("drops another lobby, older versions and duplicates", () => {
    expect(nextSnapshot(null, snap("b", 1), "a")).toBeNull();
    expect(nextSnapshot(snap("a", 2), snap("a", 1), "a")).toBeNull();
    expect(nextSnapshot(snap("a", 2), snap("a", 2), "a")).toBeNull();
  });
  test("installs the first, a newer version and a changed one at the same version", () => {
    expect(nextSnapshot(null, snap("a", 1), "a")?.lobby.version).toBe(1);
    expect(nextSnapshot(snap("a", 1), snap("a", 2), "a")?.lobby.version).toBe(
      2,
    );
    expect(
      nextSnapshot(snap("a", 1), snap("a", 1, { x: 1 }), "a"),
    ).not.toBeNull();
  });
});

describe("sameJson", () => {
  test("compares structurally and ignores $schema and undefined", () => {
    expect(
      sameJson(
        { a: [1, { b: 2 }], c: undefined },
        { a: [1, { b: 2 }], $schema: "s" },
      ),
    ).toBe(true);
    expect(sameJson({ a: [1] }, { a: [2] })).toBe(false);
    expect(sameJson([1], { 0: 1 })).toBe(false);
    expect(sameJson(null, {})).toBe(false);
  });
});
