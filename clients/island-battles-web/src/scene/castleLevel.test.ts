import { describe, expect, test } from "bun:test";
import { castleDisplayLevel, MAX_CASTLE_LEVEL } from "./castleLevel";

describe("castleDisplayLevel", () => {
  test("a castle with no upgrades shows level 1", () => {
    expect(castleDisplayLevel({})).toBe(1);
  });

  test("one type gives its level", () => {
    expect(castleDisplayLevel({ speed: 2 })).toBe(2);
    expect(castleDisplayLevel({ clash_stop_generations: 1 })).toBe(1);
  });

  test("with mixed types the highest level wins", () => {
    expect(castleDisplayLevel({ speed: 2, clash_stop_generations: 3 })).toBe(3);
    expect(castleDisplayLevel({ speed: 4, clash_stop_generations: 2 })).toBe(4);
  });

  test("a level above 4 is clamped to the last model", () => {
    expect(MAX_CASTLE_LEVEL).toBe(4);
    expect(castleDisplayLevel({ speed: 4 })).toBe(4);
    expect(castleDisplayLevel({ speed: 7, other: 9 })).toBe(4);
  });

  test("levels below 1 count as level 1", () => {
    expect(castleDisplayLevel({ speed: 0 })).toBe(1);
  });
});
