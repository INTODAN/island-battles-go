import { describe, expect, test } from "bun:test";
import { clashSummary } from "./clashes";
import { generation, sampleDemo } from "./testing";

type Clash = ReturnType<typeof generation>["clashes"][number];

const side = (team_color: number, before: number, after: number) => ({
  group_id: team_color,
  team_color,
  x: 0,
  y: 0,
  clash_radius: 0.25,
  soldiers_before: before,
  soldiers_after: after,
});

const clash = (...participants: Clash["participants"]): Clash => ({
  id: 0,
  time: 0.5,
  participants,
});

describe("clashSummary", () => {
  test("a turn without a clash has no line", () => {
    expect(clashSummary(generation(1))).toBe("");
  });

  test("one clash names each side's soldiers before and after", () => {
    const frame = generation(2);
    frame.clashes = [clash(side(1, 5, 3), side(2, 4, 2))];
    expect(clashSummary(frame)).toBe("Clash: Blue 5 → 3 · Red 4 → 2");
    const second = sampleDemo().frames[1];
    if (!second) throw new Error("the sample has two turns");
    expect(clashSummary(second)).toBe("Clash: Blue 2 → 1");
  });

  test("a clash that stopped both sides shows 0 for both", () => {
    const frame = generation(2);
    frame.clashes = [clash(side(1, 3, 0), side(2, 3, 0))];
    expect(clashSummary(frame)).toBe("Clash: Blue 3 → 0 · Red 3 → 0");
  });

  test("several clashes are joined, and one without participants is skipped", () => {
    const frame = generation(2);
    frame.clashes = [
      clash(side(1, 5, 3), side(2, 4, 0)),
      clash(),
      clash(side(3, 2, 1), side(4, 2, 0), side(5, 1, 0)),
    ];
    expect(clashSummary(frame)).toBe(
      "Clash: Blue 5 → 3 · Red 4 → 0. Clash: Green 2 → 1 · Purple 2 → 0 · Amber 1 → 0",
    );
  });
});
