import { describe, expect, test } from "bun:test";
import type { components } from "../api/generated/game";
import { type MapPoint, toMapDistance, toWorldDistance } from "./coordinates";
import {
  BOAT_BOW,
  castleGaps,
  clusterAngles,
  computeHeadings,
  type HeadingSegment,
  headingDrawOrder,
  headingStyle,
  meanAngle,
  ribbonGeometry,
  spreadAngles,
} from "./headingLines";
import { PLATEAU_RADIUS } from "./landforms";
import { insideWaterline, shorePoint } from "./shore";

type Castle = components["schemas"]["Castle"];
type SoldierGroup = components["schemas"]["SoldierGroup"];

const castle = (id: number, x: number, y: number, team = 1): Castle => ({
  id,
  x,
  y,
  team_color: team,
  soldiers: 10,
  upgrades: {},
});
const group = (
  id: number,
  x: number,
  y: number,
  origin: number,
  destination: number,
  team = 1,
): SoldierGroup => ({
  id,
  x,
  y,
  team_color: team,
  soldiers: 5,
  origin_castle_id: origin,
  destination_castle_id: destination,
  clash_radius: 0.25,
  upgrades: {},
});
/** A group `distance` from castle `target` at `degrees` (map angle, y down), heading there. */
const towards = (
  id: number,
  target: Castle,
  degrees: number,
  distance: number,
  team = 1,
  origin = 99,
): SoldierGroup =>
  group(
    id,
    target.x + Math.cos((degrees * Math.PI) / 180) * distance,
    target.y + Math.sin((degrees * Math.PI) / 180) * distance,
    origin,
    target.id,
    team,
  );

const map = toMapDistance;
const style = headingStyle;
/** The sizes the rules use, casings included, in map units. */
const lineWidth = map(style.lineWidth + style.casingWidth);
const headWidth = map(style.arrowWidth + style.casingWidth);
const headLength = map(style.arrowLength);
const laneSpacing = headWidth / 2 + lineWidth / 2 + map(style.laneGap);

const castles = [
  castle(0, -4, 0),
  castle(1, 4, 0, 2),
  castle(2, 0, 0, 0),
  castle(3, 0, 3),
];
const target = castle(5, 0, 0, 0);

const byId = (lines: HeadingSegment[]) =>
  new Map(lines.map((line) => [line.groupId, line]));
const distance = (a: MapPoint, b: MapPoint) => Math.hypot(a.x - b.x, a.y - b.y);
const direction = (from: MapPoint, to: MapPoint) => {
  const length = distance(from, to);
  return { x: (to.x - from.x) / length, y: (to.y - from.y) / length };
};
/** How far `point` is from the infinite line through a segment's start and base. */
const offLine = (point: MapPoint, line: HeadingSegment) => {
  const d = direction(line.start, line.base);
  return Math.abs(
    (point.x - line.start.x) * d.y - (point.y - line.start.y) * d.x,
  );
};

/** The arrowhead of a line as a triangle, casing included. */
function headTriangle(line: HeadingSegment): MapPoint[] {
  const d = direction(line.base, line.tip);
  const half = headWidth / 2;
  return [
    line.tip,
    { x: line.base.x - d.y * half, y: line.base.y + d.x * half },
    { x: line.base.x + d.y * half, y: line.base.y - d.x * half },
  ];
}

/** The least distance between two convex polygons, 0 when they overlap. */
function polygonGap(first: MapPoint[], second: MapPoint[]): number {
  const axes = [first, second].flatMap((polygon) =>
    polygon.map((point, index) => {
      const next = polygon[(index + 1) % polygon.length] as MapPoint;
      const d = direction(point, next);
      return { x: -d.y, y: d.x };
    }),
  );
  let gap = 0;
  for (const axis of axes) {
    const project = (polygon: MapPoint[]) =>
      polygon.map((point) => point.x * axis.x + point.y * axis.y);
    const a = project(first);
    const b = project(second);
    gap = Math.max(
      gap,
      Math.min(...b) - Math.max(...a),
      Math.min(...a) - Math.max(...b),
    );
  }
  return gap;
}

describe("heading lines", () => {
  test("a line runs from ahead of the boat to its destination's shore", () => {
    const [line] = computeHeadings(castles, [group(1, -2, 0, 0, 1)]);
    if (!line) throw new Error("no line");
    const shore = shorePoint(castles[1] as Castle, { x: -2, y: 0 });
    expect(line.tip.x).toBeCloseTo(shore.x, 9);
    expect(line.tip.y).toBeCloseTo(shore.y, 9);
    expect(line.start.x).toBeGreaterThan(-2);
    expect(line.arrowhead).toBe(true);
    expect(line.opacity).toBe(1);
  });

  test("a group about to arrive has no line, stub or backward head", () => {
    const shore = shorePoint(castles[1] as Castle, castles[0] as Castle);
    const near = group(1, shore.x - 0.1, 0, 0, 1);
    expect(computeHeadings(castles, [near])).toEqual([]);
    const far = group(2, shore.x - 1, 0, 0, 1);
    expect(computeHeadings(castles, [far])).toHaveLength(1);
  });

  test("groups of one team on one route share the rearmost group's line", () => {
    const lines = computeHeadings(castles, [
      group(1, -1, 0, 0, 1),
      group(2, -2.5, 0, 0, 1),
      group(3, 0.5, 0, 0, 1),
    ]);
    expect(lines.map((line) => line.groupId)).toEqual([2]);
  });

  test("the stacking order follows group IDs, whatever order the groups come in", () => {
    const groups = [
      group(9, -2, 2, 3, 0),
      group(3, 2, 2, 3, 1),
      group(6, 3, -2, 3, 0),
    ];
    const forward = computeHeadings(castles, groups);
    const backward = computeHeadings(castles, [...groups].reverse());
    expect(forward.map((line) => [line.groupId, line.rank])).toEqual([
      [3, 0],
      [6, 1],
      [9, 2],
    ]);
    expect(backward).toEqual(forward);
  });

  test("case 1: two teams' heads at one castle are spread so they never touch", () => {
    for (const apart of [0, 3, 8, 26]) {
      const lines = computeHeadings(
        [target],
        [towards(1, target, 150, 3, 1), towards(2, target, 150 + apart, 3, 2)],
      );
      expect(lines).toHaveLength(2);
      const [blue, red] = lines as [HeadingSegment, HeadingSegment];
      expect(blue.arrowhead && red.arrowhead).toBe(true);
      // The heads keep at least the head gap between them, casings included.
      expect(
        polygonGap(headTriangle(blue), headTriangle(red)),
      ).toBeGreaterThanOrEqual(map(style.headGap) * 0.99);
      // Both tips are still on the castle's shore.
      for (const line of lines) {
        const shore = shorePoint(target, {
          x: target.x + (line.tip.x - target.x) * 10,
          y: target.y + (line.tip.y - target.y) * 10,
        });
        expect(distance(line.tip, shore)).toBeLessThan(1e-9);
      }
    }
  });

  test("case 1: heads far enough apart are not moved", () => {
    const lines = computeHeadings(
      [target],
      [towards(1, target, 0, 3, 1), towards(2, target, 90, 3, 2)],
    );
    for (const line of lines) {
      const at = line.groupId === 1 ? { x: 3, y: 0 } : { x: 0, y: 3 };
      const shore = shorePoint(target, at);
      expect(distance(line.tip, shore)).toBeLessThan(1e-9);
    }
  });

  test("case 2: a laned line and a plain one of one team share one head", () => {
    // Blue 0 -> 2 is in a lane beside red 2 -> 0; blue from castle 3's side arrives 8 degrees off.
    const lines = byId(
      computeHeadings(
        [castle(0, -4, 0), target],
        [
          group(1, -2.5, 0, 0, 5),
          towards(2, target, 172, 2.5, 1),
          group(3, -1.2, 0, 5, 0, 2),
        ],
      ),
    );
    const laned = lines.get(1);
    const plain = lines.get(2);
    if (!laned || !plain) throw new Error("missing line");
    expect(Math.abs(laned.start.y)).toBeGreaterThan(laneSpacing / 2 - 1e-9);
    expect([laned.arrowhead, plain.arrowhead].sort()).toEqual([false, true]);
    expect(distance(laned.base, plain.base)).toBeLessThan(1e-9);
  });

  test("case 3: opposite lanes are far enough apart that lines and heads never touch", () => {
    const lines = computeHeadings(castles, [
      group(1, -2, 0, 0, 1),
      group(2, 2, 0, 1, 0, 2),
    ]);
    expect(lines).toHaveLength(2);
    const [east, west] = lines as [HeadingSegment, HeadingSegment];
    // Each keeps to the right of its travel: east-bound south (y > 0), west-bound north.
    expect(east.start.y).toBeCloseTo(laneSpacing / 2, 9);
    expect(west.start.y).toBeCloseTo(-laneSpacing / 2, 9);
    // Every corner of one head is clear of the other line's casing, by the lane gap.
    for (const [line, other] of [
      [east, west],
      [west, east],
    ] as const) {
      for (const corner of headTriangle(line)) {
        expect(offLine(corner, other) - lineWidth / 2).toBeGreaterThanOrEqual(
          map(style.laneGap) - 1e-9,
        );
      }
    }
  });

  test("case 4: a third castle's group on the same corridor gets a lane", () => {
    // Castle 9 sits just off the line from castle 0 to castle 1: its group sails the same way back.
    const withThird = [...castles, castle(9, 6.5, 0.15, 2)];
    const lines = byId(
      computeHeadings(withThird, [
        group(1, -2, 0, 0, 1),
        group(2, 2, 0.05, 9, 0, 2),
      ]),
    );
    const east = lines.get(1);
    const west = lines.get(2);
    if (!east || !west) throw new Error("missing line");
    const gap = Math.abs(east.start.y - west.start.y);
    expect(gap).toBeGreaterThan(laneSpacing - 0.05 - 1e-6);
    expect(east.start.y).toBeGreaterThan(0);
    expect(west.start.y).toBeLessThan(0.05);
  });

  test("case 4: a group whose own line is hidden does not shift others", () => {
    const shore = shorePoint(castles[0] as Castle, castles[1] as Castle);
    const lines = computeHeadings(castles, [
      group(1, -2, 0, 0, 1),
      // About to land at castle 0: no line of its own.
      group(2, shore.x + 0.05, 0, 1, 0, 2),
    ]);
    expect(lines.map((line) => line.groupId)).toEqual([1]);
    expect(lines[0]?.start.y).toBeCloseTo(0, 9);
  });

  test("case 4: lines that only cross are not laned", () => {
    const lines = computeHeadings(castles, [
      group(1, -2, 0, 0, 1),
      group(2, 0.5, 2.5, 3, 2, 2),
    ]);
    expect(lines.find((line) => line.groupId === 1)?.start.y).toBeCloseTo(0, 9);
  });

  test("case 5: two teams sailing the same way on one path get parallel lanes", () => {
    // This replaces #89's rule that another team's group on the same route keeps its line on it.
    const lines = computeHeadings(castles, [
      group(1, -1, 0, 0, 1),
      group(2, -2.5, 0, 0, 1, 2),
    ]);
    expect(lines.map((line) => line.groupId)).toEqual([1, 2]);
    const [first, second] = lines as [HeadingSegment, HeadingSegment];
    // The lanes are centred on the path. (The heads are spread at the castle, as in case 1, so
    // the lines turn very slightly towards them: the start is in its lane to within a hair.)
    expect(first.start.y).toBeCloseTo(-laneSpacing / 2, 2);
    expect(second.start.y).toBeCloseTo(laneSpacing / 2, 2);
    expect(first.arrowhead && second.arrowhead).toBe(true);
    expect(
      polygonGap(headTriangle(first), headTriangle(second)),
    ).toBeGreaterThan(0);
  });

  test("case 6: lines sharing a head end at its base, not at its tip", () => {
    const lines = byId(
      computeHeadings(
        [target],
        [towards(1, target, 0, 3, 1), towards(2, target, 12, 2.5, 1)],
      ),
    );
    const leader = lines.get(1);
    const member = lines.get(2);
    if (!leader || !member) throw new Error("missing line");
    expect(leader.arrowhead).toBe(true);
    expect(member.arrowhead).toBe(false);
    expect(distance(member.base, leader.base)).toBeLessThan(1e-9);
    expect(distance(leader.base, leader.tip)).toBeCloseTo(headLength, 9);
    // The member's shaft stays inside the head's base: its end is the base's centre.
    const ribbon = ribbonGeometry(member, style);
    const ends = [ribbon.points.slice(4, 6), ribbon.points.slice(6, 8)].map(
      ([x, y]) => ({ x: x ?? 0, y: y ?? 0 }),
    );
    for (const end of ends) {
      expect(distance(end, leader.base)).toBeLessThanOrEqual(
        map(style.arrowWidth) / 2,
      );
    }
  });

  test("case 7: a shared head covers at most 15 degrees in total", () => {
    const angles = [0, 7, 14, 21, 28];
    const lines = computeHeadings(
      [target],
      angles.map((angle, index) => towards(index + 1, target, angle, 3, 1)),
    );
    expect(lines).toHaveLength(angles.length);
    const groups = new Map<string, number[]>();
    for (const line of lines) {
      const key = `${line.base.x.toFixed(9)},${line.base.y.toFixed(9)}`;
      groups.set(key, [
        ...(groups.get(key) ?? []),
        angles[line.groupId - 1] ?? 0,
      ]);
    }
    expect(groups.size).toBeGreaterThan(1);
    for (const members of groups.values()) {
      expect(Math.max(...members) - Math.min(...members)).toBeLessThanOrEqual(
        15,
      );
    }
    expect(lines.filter((line) => line.arrowhead)).toHaveLength(groups.size);
    expect(
      clusterAngles(
        [0, 7, 14, 21, 28].map((a) => (a * Math.PI) / 180),
        (15 * Math.PI) / 180,
      ),
    ).toEqual([
      [0, 1, 2],
      [3, 4],
    ]);
  });

  test("case 8: angles either side of 180 degrees are compared round the circle", () => {
    const lines = byId(
      computeHeadings(
        [target],
        [
          towards(1, target, 174, 3, 1),
          towards(2, target, 10, 3, 1),
          towards(3, target, -174, 3, 1),
        ],
      ),
    );
    expect(lines.get(1)?.arrowhead).toBe(true);
    expect(lines.get(3)?.arrowhead).toBe(false);
    expect(lines.get(2)?.arrowhead).toBe(true);
    const shared = lines.get(1);
    if (!shared) throw new Error("no line");
    // The shared head points from 180 degrees, between the two.
    expect(Math.abs(Math.atan2(shared.tip.y, shared.tip.x))).toBeCloseTo(
      Math.PI,
      6,
    );
    expect(meanAngle([Math.PI - 0.1, -Math.PI + 0.1])).toBeCloseTo(Math.PI, 9);
  });

  test("case 9: every line drawn is longer than its head and a piece of shaft, never NaN", () => {
    let seed = 7;
    const random = () => {
      seed = (seed * 16807) % 2147483647;
      return seed / 2147483647;
    };
    for (let round = 0; round < 60; round++) {
      const groups = Array.from({ length: 6 }, (_, index) =>
        towards(
          index + 1,
          target,
          random() * 360,
          map(1.2) + random() * 1.2,
          1 + Math.floor(random() * 2),
        ),
      );
      for (const line of computeHeadings([target, castle(6, 6, 6)], groups)) {
        for (const value of [
          line.start.x,
          line.start.y,
          line.tip.x,
          line.tip.y,
          line.base.x,
          line.base.y,
          line.opacity,
        ]) {
          expect(Number.isFinite(value)).toBe(true);
        }
        const shaft = distance(line.start, line.base);
        expect(shaft).toBeGreaterThanOrEqual(map(style.minShaft) - 1e-9);
        // The shaft runs towards the castle: the base is ahead of the start.
        const toCastle = direction(line.start, target);
        expect(
          (line.base.x - line.start.x) * toCastle.x +
            (line.base.y - line.start.y) * toCastle.y,
        ).toBeGreaterThan(0);
      }
    }
    // A group on its castle's centre has no direction: no line, and no NaN.
    expect(computeHeadings([target], [towards(1, target, 0, 0)])).toEqual([]);
  });

  test("case 9: a ribbon of a degenerate segment has no NaN", () => {
    const point = { x: 1, y: 1 };
    const ribbon = ribbonGeometry(
      { start: point, base: point, tip: point, arrowhead: true, gaps: [] },
      style,
    );
    expect(ribbon.points.every(Number.isFinite)).toBe(true);
  });

  test("case 10: the head ends at the destination's shore point, fully outside the island", () => {
    for (const degrees of [0, 60, 135, 200, 290]) {
      const [line] = computeHeadings(
        [target],
        [towards(1, target, degrees, 3)],
      );
      if (!line) throw new Error("no line");
      const at = {
        x: Math.cos((degrees * Math.PI) / 180) * 3,
        y: Math.sin((degrees * Math.PI) / 180) * 3,
      };
      const shore = shorePoint(target, at);
      expect(distance(line.tip, shore)).toBeLessThan(1e-9);
      for (const corner of headTriangle(line)) {
        expect(insideWaterline(target, corner)).toBe(false);
        // Clear of the biggest castle model's base.
        expect(toWorldDistance(distance(corner, target))).toBeGreaterThan(
          PLATEAU_RADIUS,
        );
      }
    }
  });

  test("case 13: a boat still at its island's shore has no arrow; it fades in as the boat sails off", () => {
    const shore = shorePoint(castles[0] as Castle, castles[1] as Castle);
    // Just launched: the boat is drawn at castle 0's shore, whatever its true position.
    expect(computeHeadings(castles, [group(1, -3.9, 0, 0, 1)])).toEqual([]);
    const fade = map(style.launchFade);
    const [half] = computeHeadings(castles, [
      group(1, shore.x + fade / 2, 0, 0, 1),
    ]);
    expect(half?.opacity).toBeCloseTo(0.5, 9);
    const [whole] = computeHeadings(castles, [
      group(1, shore.x + fade, 0, 0, 1),
    ]);
    expect(whole?.opacity).toBeCloseTo(1, 9);
    expect(whole?.start.x).toBeCloseTo(
      shore.x + fade + map(BOAT_BOW + style.startGap),
      9,
    );
  });

  test("case 11: the line starts clear of the boat's bow at the boat's scale", () => {
    for (const scale of [1, 1.3, 1.7]) {
      const [line] = computeHeadings(castles, [group(1, -2, 0, 0, 1)], {
        boatScale: () => scale,
      });
      if (!line) throw new Error("no line");
      const ahead = toWorldDistance(line.start.x + 2);
      expect(ahead).toBeCloseTo(BOAT_BOW * scale + style.startGap, 9);
      expect(ahead).toBeGreaterThan(BOAT_BOW * scale);
    }
  });

  test("case 12: a line leaves a gap where it passes another castle", () => {
    const [line] = computeHeadings(castles, [group(1, -2.5, 0, 0, 1)]);
    if (!line) throw new Error("no line");
    expect(line.passesUnder).toEqual([2]);
    expect(line.gaps).toHaveLength(1);
    const [from, to] = line.gaps[0] as [number, number];
    // The gap is centred on castle 2 and as wide as its castle-gap circle.
    const centre = -2.5 + toMapDistance(BOAT_BOW + style.startGap);
    expect((from + to) / 2 + centre).toBeCloseTo(0, 9);
    expect(to - from).toBeCloseTo(2 * map(style.castleGap), 9);
    // The ribbon has two pieces of shaft, none of them near castle 2.
    const ribbon = ribbonGeometry(line, style);
    expect(ribbon.indices).toHaveLength(6 * 2 + 3);
    for (let index = 0; index < ribbon.points.length - 2; index += 2) {
      expect(Math.abs(ribbon.points[index] ?? 0)).toBeGreaterThan(
        map(style.castleGap) - 1e-9,
      );
    }
    // Its destination never cuts it.
    expect(
      castleGaps({ x: -2, y: 0 }, { x: 3, y: 0 }, castles, 1, 0.5).castles,
    ).toEqual([2]);
  });

  test("case 14: the line of a group that is not moving is dimmed", () => {
    const lines = byId(
      computeHeadings(castles, [group(1, -2, 0, 0, 1), group(2, 2, 2, 3, 1)], {
        held: new Set([2]),
      }),
    );
    expect(lines.get(1)?.opacity).toBe(1);
    expect(lines.get(2)?.opacity).toBe(style.heldOpacity);
  });

  test("case 15: lines draw at one height in a fixed order, so they never flicker", () => {
    expect("stackStep" in style).toBe(false);
    for (let rank = 0; rank < 5; rank++) {
      const order = headingDrawOrder(rank);
      const next = headingDrawOrder(rank + 1);
      expect(order.casing).toBeLessThan(order.line);
      expect(order.line).toBeLessThan(next.casing);
    }
  });

  test("spreading angles moves only those too close, by as little as needed", () => {
    const step = 0.2;
    const spread = spreadAngles(
      [0, 0.05, 2, Math.PI - 0.02, -Math.PI + 0.02],
      step,
    );
    expect(spread[2]).toBe(2);
    // The two near zero move apart about their middle.
    expect(((spread[0] ?? 0) + (spread[1] ?? 0)) / 2).toBeCloseTo(0.025, 9);
    expect((spread[1] ?? 0) - (spread[0] ?? 0)).toBeCloseTo(step, 9);
    // The two either side of 180 degrees, round the seam.
    const across = Math.abs(
      Math.atan2(
        Math.sin((spread[4] ?? 0) - (spread[3] ?? 0)),
        Math.cos((spread[4] ?? 0) - (spread[3] ?? 0)),
      ),
    );
    expect(across).toBeCloseTo(step, 9);
  });

  test("the ribbon is a shaft and a head, and the casing is wider", () => {
    const [line] = computeHeadings(castles, [group(1, 1, 2, 3, 1)]);
    if (!line) throw new Error("no line");
    expect(line.gaps).toEqual([]);
    const plain = ribbonGeometry(line, style);
    const casing = ribbonGeometry(line, style, style.casingWidth);
    expect(plain.indices).toHaveLength(9);
    expect(plain.points).toHaveLength(14);
    const width = (points: number[]) =>
      Math.hypot(
        (points[2] ?? 0) - (points[0] ?? 0),
        (points[3] ?? 0) - (points[1] ?? 0),
      );
    expect(width(casing.points)).toBeGreaterThan(width(plain.points));
    const headless = ribbonGeometry({ ...line, arrowhead: false }, style);
    expect(headless.indices).toHaveLength(6);
  });

  test("a ribbon keeps its drawn width and head length in world units", () => {
    const segment = {
      start: { x: 0, y: 0 },
      base: { x: 3 - map(style.arrowLength), y: 0 },
      tip: { x: 3, y: 0 },
      arrowhead: true,
      gaps: [],
    };
    const { points } = ribbonGeometry(segment, style);
    const world = (value: number) => toWorldDistance(value);
    // The shaft's two start corners are lineWidth apart; the head's wings are arrowWidth apart.
    expect(world((points[3] ?? 0) - (points[1] ?? 0))).toBeCloseTo(
      style.lineWidth,
      9,
    );
    expect(world((points[11] ?? 0) - (points[9] ?? 0))).toBeCloseTo(
      style.arrowWidth,
      9,
    );
    expect(world((points[12] ?? 0) - (points[4] ?? 0))).toBeCloseTo(
      style.arrowLength,
      9,
    );
  });
});
