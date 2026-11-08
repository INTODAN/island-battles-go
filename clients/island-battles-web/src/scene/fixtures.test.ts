import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import type { components } from "../api/generated/game";
import { hasUpgrades, upgradeLevels } from "./format";

type Demo = components["schemas"]["Demo"];

const directory = new URL("../../test/fixtures/", import.meta.url);
const names = readdirSync(directory).filter((name) => name.endsWith(".json"));

describe("fixtures", () => {
  test("the three fixtures are there", () => {
    expect(names.sort()).toEqual([
      "busy-turn.json",
      "generated-map.json",
      "six-teams.json",
    ]);
  });

  for (const name of names) {
    const demo = JSON.parse(
      readFileSync(new URL(name, directory), "utf8"),
    ) as Demo;

    test(`${name} is a demo whose castles never move or change`, () => {
      expect(demo.format).toBe("island-battles-demo");
      expect(demo.frames.length).toBeGreaterThan(0);
      const first = demo.frames[0];
      for (const frame of demo.frames) {
        expect(
          frame.castles.map((castle) => [castle.id, castle.x, castle.y]),
        ).toEqual(
          first?.castles.map((castle) => [castle.id, castle.x, castle.y]) ?? [],
        );
        const ids = frame.soldier_groups.map((group) => group.id);
        expect(ids).toEqual([...ids].sort((left, right) => left - right));
        for (const group of frame.soldier_groups) {
          expect(
            frame.castles.some(
              (castle) => castle.id === group.destination_castle_id,
            ),
          ).toBe(true);
        }
      }
    });
  }

  test("the six-team map has six teams", () => {
    const demo = JSON.parse(
      readFileSync(new URL("six-teams.json", directory), "utf8"),
    ) as Demo;
    expect(demo.teams).toHaveLength(6);
    expect(
      new Set(demo.frames[0]?.castles.map((castle) => castle.team_color)).size,
    ).toBe(6);
  });

  test("the busy turn has clashes and upgrades", () => {
    const demo = JSON.parse(
      readFileSync(new URL("busy-turn.json", directory), "utf8"),
    ) as Demo;
    expect(demo.frames.some((frame) => frame.clashes.length > 0)).toBe(true);
    expect(
      demo.frames.some((frame) =>
        frame.castles.some((castle) => hasUpgrades(castle.upgrades)),
      ),
    ).toBe(true);
  });
});

describe("upgrade text", () => {
  test("is written as the replay writes it", () => {
    expect(upgradeLevels({ clash_stop_generations: 1, speed: 2 })).toBe(
      "Speed 2 · Stop 1",
    );
    expect(upgradeLevels({ other: 3, speed: 1 })).toBe("Speed 1 · other 3");
  });

  test("the upgrade line shows once a level is above the first", () => {
    expect(hasUpgrades({})).toBe(false);
    expect(hasUpgrades({ speed: 1, clash_stop_generations: 1 })).toBe(false);
    expect(hasUpgrades({ speed: 1, clash_stop_generations: 2 })).toBe(true);
  });
});
