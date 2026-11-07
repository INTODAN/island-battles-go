import { describe, expect, test } from "bun:test";
import type { components } from "../api/generated/game";
import { CASTLE_RADIUS } from "./board";
import { computeHeadings, headingStyle, ribbonGeometry } from "./headingLines";

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

const castles = [
  castle(0, -4, 0),
  castle(1, 4, 0, 2),
  castle(2, 0, 0, 0),
  castle(3, 0, 3),
];

describe("heading lines", () => {
  test("a line ends on the destination castle's edge, ahead of the group", () => {
    const [line] = computeHeadings(castles, [group(1, -2, 0, 0, 1)]);
    expect(line).toBeDefined();
    expect(Math.hypot((line?.tip.x ?? 0) - 4, line?.tip.y ?? 0)).toBeCloseTo(
      CASTLE_RADIUS,
      6,
    );
    expect(line?.tip.x).toBeCloseTo(3.5, 6);
    expect(line?.start.x).toBeGreaterThan(-2);
    expect(line?.arrowhead).toBe(true);
  });

  test("a group about to arrive has no line, stub or backward head", () => {
    const near = group(1, 4 - CASTLE_RADIUS - 0.25 - 0.1, 0, 0, 1);
    expect(computeHeadings(castles, [near])).toEqual([]);
    const far = group(2, 4 - CASTLE_RADIUS - 1, 0, 0, 1);
    expect(computeHeadings(castles, [far])).toHaveLength(1);
  });

  test("groups of one team on one route share the rearmost group's line", () => {
    const lines = computeHeadings(castles, [
      group(1, -1, 0, 0, 1),
      group(2, -3, 0, 0, 1),
      group(3, 0.5, 0, 0, 1),
    ]);
    expect(lines.map((line) => line.groupId)).toEqual([2]);
    expect(lines[0]?.start.x).toBeCloseTo(-3 + 0.25 + headingStyle.startGap, 6);
  });

  test("another team's group on the same route keeps its own line", () => {
    const lines = computeHeadings(castles, [
      group(1, -1, 0, 0, 1),
      group(2, -3, 0, 0, 1, 2),
    ]);
    expect(lines.map((line) => line.groupId)).toEqual([1, 2]);
  });

  test("opposite directions on one route are offset to opposite lanes", () => {
    const lines = computeHeadings(castles, [
      group(1, -2, 0, 0, 1),
      group(2, 2, 0, 1, 0, 2),
    ]);
    expect(lines).toHaveLength(2);
    const [forward, back] = lines;
    // Each travels with its lane to its right: east-bound is south of the route, west-bound north.
    expect(forward?.start.y).toBeCloseTo(headingStyle.laneOffset, 6);
    expect(back?.start.y).toBeCloseTo(-headingStyle.laneOffset, 6);
    // Both arrowheads still sit on their castle's circle.
    expect(
      Math.hypot((forward?.tip.x ?? 0) - 4, forward?.tip.y ?? 0),
    ).toBeCloseTo(CASTLE_RADIUS, 6);
    expect(Math.hypot((back?.tip.x ?? 0) + 4, back?.tip.y ?? 0)).toBeCloseTo(
      CASTLE_RADIUS,
      6,
    );
  });

  test("same-team lines arriving from nearly one direction share one arrowhead", () => {
    const lines = computeHeadings(castles, [
      group(1, 4, 3, 3, 1),
      group(2, 4.6, 3, 3, 1),
      group(3, -2, 0, 0, 1),
    ]);
    const byId = new Map(lines.map((line) => [line.groupId, line]));
    expect(byId.get(1)?.arrowhead).toBe(true);
    expect(byId.get(2)?.arrowhead).toBe(false);
    expect(byId.get(3)?.arrowhead).toBe(true);
    expect(byId.get(2)?.tip).toEqual(byId.get(1)?.tip as never);
  });

  test("a line passing under another castle says so", () => {
    const [line] = computeHeadings(castles, [group(1, -3, 0, 0, 1)]);
    expect(line?.passesUnder).toEqual([2]);
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

  test("the ribbon is a shaft and a head, and the casing is wider", () => {
    const [line] = computeHeadings(castles, [group(1, -2, 0, 0, 1)]);
    if (!line) throw new Error("no line");
    const plain = ribbonGeometry(line, headingStyle);
    const casing = ribbonGeometry(line, headingStyle, headingStyle.casingWidth);
    expect(plain.indices).toHaveLength(9);
    expect(plain.points).toHaveLength(14);
    const width = (points: number[]) =>
      Math.abs((points[3] ?? 0) - (points[1] ?? 0));
    expect(width(casing.points)).toBeGreaterThan(width(plain.points));
    const headless = ribbonGeometry(
      { ...line, arrowhead: false },
      headingStyle,
    );
    expect(headless.indices).toHaveLength(6);
  });
});
