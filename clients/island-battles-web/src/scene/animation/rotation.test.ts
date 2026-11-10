import { describe, expect, test } from "bun:test";
import { shortestAngle, TURN_MS, turnToward } from "./rotation";

const FRAME_MS = 1000 / 60;

/** Turns from `from` to `to` frame by frame for `ms`; returns every heading on the way. */
function turn(from: number, to: number, ms: number): number[] {
  const headings = [from];
  for (let time = 0; time < ms; time += FRAME_MS) {
    headings.push(turnToward(headings.at(-1) as number, to, FRAME_MS));
  }
  return headings;
}

describe("turning to a new heading (#176)", () => {
  test("takes about 200 ms and ends exactly on the heading", () => {
    const headings = turn(0, Math.PI / 2, 1000);
    // Not at once: the first frame turns less than a third of the way.
    expect(headings[1] as number).toBeGreaterThan(0);
    expect(headings[1] as number).toBeLessThan(Math.PI / 6);
    // 95% of the way after TURN_MS, and never past it.
    const atTurnMs = headings[Math.round(TURN_MS / FRAME_MS)] as number;
    expect(atTurnMs).toBeGreaterThan(0.94 * (Math.PI / 2));
    for (const heading of headings) {
      expect(heading).toBeLessThanOrEqual(Math.PI / 2);
    }
    expect(headings.at(-1)).toBe(Math.PI / 2);
  });

  test("goes the shorter way round", () => {
    // From just short of +π to just past -π is a small turn through π, not nearly a full one.
    const headings = turn(3, -3, 1000);
    for (let index = 1; index < headings.length; index++) {
      const step = shortestAngle(
        headings[index - 1] as number,
        headings[index] as number,
      );
      expect(step).toBeGreaterThanOrEqual(0);
      expect(step).toBeLessThan(0.1);
    }
    expect(shortestAngle(headings.at(-1) as number, -3)).toBeCloseTo(0, 9);
  });

  test("a boat on its heading stays put, and a long frame finishes the turn", () => {
    expect(turnToward(1, 1, FRAME_MS)).toBe(1);
    expect(turnToward(0, 2, 5000)).toBe(2);
  });
});
