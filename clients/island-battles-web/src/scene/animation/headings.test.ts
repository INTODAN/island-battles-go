import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { components } from "../../api/generated/game";
import { headingStyle } from "../headingLines";
import { shorePoint } from "../shore";
import {
  clashHeld,
  heldGroups,
  planTurnHeadings,
  restingHeadings,
  sampleTurnHeadings,
} from "./headings";
import { planTurn, sampleTurn } from "./plan";

type Generation = components["schemas"]["Generation"];
type Castle = components["schemas"]["Castle"];
type SoldierGroup = components["schemas"]["SoldierGroup"];
type Demo = components["schemas"]["Demo"];

const scale = (soldiers: number) =>
  Math.min(1.7, 1 + 0.18 * Math.log2(1 + soldiers / 10));

const castle = (id: number, x: number, y: number, team = 1): Castle => ({
  id,
  x,
  y,
  team_color: team,
  soldiers: 10,
  upgrades: { speed: 1 },
});
const group = (
  id: number,
  x: number,
  y: number,
  origin: number,
  destination: number,
  team = 1,
  soldiers = 5,
): SoldierGroup => ({
  id,
  x,
  y,
  team_color: team,
  soldiers,
  origin_castle_id: origin,
  destination_castle_id: destination,
  clash_radius: 0.25,
  upgrades: { speed: 1 },
});
const castles = [castle(0, -4, 0), castle(1, 4, 0, 2), castle(3, 0, 3)];
const generation = (
  id: number,
  soldierGroups: SoldierGroup[],
  clashes: Generation["clashes"] = [],
): Generation => ({
  id,
  castles,
  soldier_groups: soldierGroups,
  clashes,
});

/** The arrows of a turn at many moments, by group ID. */
function play(previous: Generation, next: Generation, steps = 200) {
  const plan = planTurn(previous, next);
  const turn = planTurnHeadings(plan, clashHeld(previous), scale);
  return Array.from({ length: steps + 1 }, (_, step) => {
    const sample = sampleTurn(plan, step / steps);
    return new Map(
      sampleTurnHeadings(turn, sample, scale).map((segment) => [
        segment.groupId,
        segment,
      ]),
    );
  });
}

describe("heading arrows during a turn", () => {
  test("case 13: playing busy-turn.json, no arrow pops in or out or jumps", () => {
    const demo = JSON.parse(
      readFileSync(
        new URL("../../../test/fixtures/busy-turn.json", import.meta.url),
        "utf8",
      ),
    ) as Demo;
    let held = clashHeld(demo.frames[0] as Generation);
    for (let index = 1; index < demo.frames.length; index++) {
      const previous = demo.frames[index - 1] as Generation;
      const next = demo.frames[index] as Generation;
      const plan = planTurn(previous, next);
      const turn = planTurnHeadings(plan, held, scale);
      const steps = 400;
      let before = new Map<
        number,
        {
          opacity: number;
          tip: { x: number; y: number };
          start: { x: number; y: number };
        }
      >();
      for (let step = 0; step <= steps; step++) {
        const sample = sampleTurn(plan, step / steps);
        const now = new Map(
          sampleTurnHeadings(turn, sample, scale).map((segment) => [
            segment.groupId,
            segment,
          ]),
        );
        if (step > 0) {
          for (const id of new Set([...before.keys(), ...now.keys()])) {
            const was = before.get(id);
            const is = now.get(id);
            // Opacity changes gradually: an arrow fades rather than pops.
            const change = Math.abs((was?.opacity ?? 0) - (is?.opacity ?? 0));
            const visible = sample.groups.get(id)?.visible ?? false;
            if (visible) expect(change).toBeLessThan(0.2);
            // Its tip never jumps (no sudden lane or head change).
            if (was && is) {
              expect(
                Math.hypot(was.tip.x - is.tip.x, was.tip.y - is.tip.y),
              ).toBeLessThan(0.05);
            }
          }
        }
        before = now;
      }
      // The turn starts as the previous generation was drawn and ends as the next one is.
      const atStart = sampleTurnHeadings(turn, sampleTurn(plan, 0), scale);
      const rest = restingHeadings(previous, held, scale);
      expect(atStart.map((s) => s.groupId)).toEqual(rest.map((s) => s.groupId));
      // A head one end of the turn draws is drawn all turn (see `sampleTurnHeadings`), so a
      // line that shares another group's head at rest may draw its own at t = 0. That is
      // only allowed where nothing is seen to appear: the new head sits exactly on a head
      // the resting view draws for the same team. A head the resting view draws is never lost.
      atStart.forEach((segment, position) => {
        const resting = rest[position];
        if (resting?.arrowhead) expect(segment.arrowhead).toBe(true);
        if (!resting || resting.arrowhead || !segment.arrowhead) return;
        const covered = rest.some(
          (other) =>
            other.arrowhead &&
            other.teamColor === segment.teamColor &&
            Math.hypot(
              other.tip.x - segment.tip.x,
              other.tip.y - segment.tip.y,
            ) < 1e-6,
        );
        expect(covered).toBe(true);
      });
      atStart.forEach((segment, position) => {
        const resting = rest[position];
        if (!resting) return;
        expect(segment.tip.x).toBeCloseTo(resting.tip.x, 6);
        expect(segment.start.y).toBeCloseTo(resting.start.y, 6);
        expect(segment.opacity).toBeCloseTo(resting.opacity, 6);
      });
      held = heldGroups(previous, next);
      const atEnd = sampleTurnHeadings(turn, sampleTurn(plan, 1), scale);
      const restEnd = restingHeadings(next, held, scale);
      expect(atEnd.map((s) => s.groupId)).toEqual(
        restEnd.map((s) => s.groupId),
      );
      atEnd.forEach((segment, position) => {
        const resting = restEnd[position];
        if (!resting) return;
        expect(segment.tip.x).toBeCloseTo(resting.tip.x, 6);
        expect(segment.start.x).toBeCloseTo(resting.start.x, 6);
        expect(segment.opacity).toBeCloseTo(resting.opacity, 6);
      });
    }
  });

  test("case 13: a group landing this turn keeps its arrow, getting shorter, until it lands", () => {
    const frames = play(
      generation(1, [group(1, 1.5, 0, 0, 1)]),
      generation(2, []),
    );
    const lengths = frames.map((frame) => {
      const line = frame.get(1);
      return line
        ? Math.hypot(line.tip.x - line.start.x, line.tip.y - line.start.y)
        : 0;
    });
    expect(frames[0]?.get(1)?.opacity).toBe(1);
    // It is drawn at full strength while long, shortens steadily, and is gone at the end.
    expect(frames[20]?.get(1)?.opacity).toBe(1);
    for (let index = 1; index < lengths.length; index++) {
      expect(lengths[index] ?? 0).toBeLessThanOrEqual(
        (lengths[index - 1] ?? 0) + 1e-9,
      );
    }
    expect(frames[frames.length - 1]?.get(1)).toBeUndefined();
  });

  test("case 13: a group launched this turn gets its arrow once it has left the shore", () => {
    const frames = play(
      generation(1, []),
      generation(2, [group(1, -2.5, 0, 0, 1)]),
    );
    const launch = shorePoint(castles[0] as Castle, castles[1] as Castle);
    expect(frames[0]?.get(1)).toBeUndefined();
    expect(frames[frames.length - 1]?.get(1)?.opacity).toBe(1);
    // While the boat is still at the shore, there is no arrow.
    const plan = planTurn(
      generation(1, []),
      generation(2, [group(1, -2.5, 0, 0, 1)]),
    );
    frames.forEach((frame, step) => {
      const pose = sampleTurn(plan, step / 200).groups.get(1);
      if (pose && Math.hypot(pose.x - launch.x, pose.y - launch.y) < 1e-9) {
        expect(frame.get(1)).toBeUndefined();
      }
    });
  });

  test("case 13: an arrow that only the start has fades out over the turn", () => {
    // At the end group 5, launched this turn, is behind group 2 on its route, so group 2's
    // line gives way to group 5's (one team on one route is one line, the rearmost's).
    const frames = play(
      generation(1, [group(2, -2, 0, 0, 1)]),
      generation(2, [group(2, -1.5, 0, 0, 1), group(5, -2.6, 0, 0, 1)]),
    );
    const opacities = frames.map((frame) => frame.get(2)?.opacity ?? 0);
    expect(opacities[0]).toBe(1);
    expect(opacities[100]).toBeCloseTo(0.5, 9);
    expect(opacities[opacities.length - 1]).toBe(0);
    for (let index = 1; index < opacities.length; index++) {
      expect(opacities[index] ?? 0).toBeLessThanOrEqual(
        (opacities[index - 1] ?? 0) + 1e-9,
      );
    }
    // Group 5's arrow fades in as it sails off its island, and is whole at the end.
    expect(frames[0]?.get(5)).toBeUndefined();
    expect(frames[frames.length - 1]?.get(5)?.opacity).toBe(1);
  });

  test("case 13: shared heads that regroup over a turn slide into place and never pop", () => {
    // Blue lines arrive at castle 1 from 156, 168 and 180 degrees: {1, 2} share a head and 3 has its
    // own. Group 1 lands this turn, so at the end {2, 3} share one.
    const at = (id: number, degrees: number, distance: number) =>
      group(
        id,
        4 + Math.cos((degrees * Math.PI) / 180) * distance,
        Math.sin((degrees * Math.PI) / 180) * distance,
        9,
        1,
      );
    const frames = play(
      generation(1, [at(1, 156, 1.6), at(2, 168, 3), at(3, 180, 3)]),
      generation(2, [at(2, 168, 2.6), at(3, 180, 2.6)]),
    );
    const first = frames[0];
    const last = frames[frames.length - 1];
    // At the start group 2's head (which it draws all turn, as the end has it) lies exactly on
    // group 1's shared head, so it shows as one.
    const start1 = first?.get(1);
    const start2 = first?.get(2);
    if (!start1 || !start2) throw new Error("missing line");
    expect(start1.arrowhead).toBe(true);
    expect(
      Math.hypot(start1.tip.x - start2.tip.x, start1.tip.y - start2.tip.y),
    ).toBeLessThan(1e-9);
    expect(
      Math.hypot(start1.base.x - start2.base.x, start1.base.y - start2.base.y),
    ).toBeLessThan(1e-9);
    expect(last?.get(2)?.arrowhead).toBe(true);
    expect(last?.get(3)?.arrowhead).toBe(false);
    // Group 3's own head meets group 2's at the shared tip by the end, then gives way to it.
    const beforeEnd = frames[frames.length - 2];
    const tip2 = beforeEnd?.get(2)?.tip;
    const tip3 = beforeEnd?.get(3)?.tip;
    if (!tip2 || !tip3) throw new Error("missing line");
    expect(Math.hypot(tip2.x - tip3.x, tip2.y - tip3.y)).toBeLessThan(0.01);
    expect(beforeEnd?.get(3)?.arrowhead).toBe(true);
  });

  test("case 14: a group that stays put is dimmed, one stopped by a clash dims from the clash", () => {
    const still = group(1, -2, 0, 0, 1);
    const moving = group(2, 2, 2, 3, 1, 2);
    expect(
      heldGroups(
        generation(1, [still, moving]),
        generation(2, [still, { ...moving, x: 1.6, y: 2.2 }]),
      ),
    ).toEqual(new Set([1]));
    // Two groups met at t = 0.5, 0.2 apart; the survivor stopped half-way between them.
    const clash = {
      id: 1,
      time: 0.5,
      participants: [
        {
          group_id: 1,
          team_color: 1,
          x: -1.1,
          y: 0,
          clash_radius: 0.25,
          soldiers_before: 9,
          soldiers_after: 3,
        },
        {
          group_id: 3,
          team_color: 2,
          x: -0.9,
          y: 0,
          clash_radius: 0.25,
          soldiers_before: 6,
          soldiers_after: 0,
        },
      ],
    };
    const next = generation(2, [{ ...still, x: -1, soldiers: 3 }], [clash]);
    expect(clashHeld(next)).toEqual(new Set([1]));
    const frames = play(
      generation(1, [still, group(3, 0, 0, 1, 0, 2, 6)]),
      next,
    );
    expect(frames[50]?.get(1)?.opacity).toBe(1);
    expect(frames[200]?.get(1)?.opacity).toBeCloseTo(
      headingStyle.heldOpacity,
      9,
    );
    const resting = restingHeadings(next, clashHeld(next), scale);
    expect(resting[0]?.opacity).toBeCloseTo(headingStyle.heldOpacity, 9);
  });
});
