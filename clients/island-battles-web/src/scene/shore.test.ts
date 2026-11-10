import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { components } from "../api/generated/game";
import { toWorldDistance } from "./coordinates";
import { islandOutline } from "./landforms";
import {
  drawnPosition,
  insideWaterline,
  SHORE_MARGIN,
  sharesLandmass,
  shorePoint,
  shoreReach,
} from "./shore";

type Demo = components["schemas"]["Demo"];
type Generation = components["schemas"]["Generation"];

const FIXTURES = ["busy-turn.json", "generated-map.json", "six-teams.json"];

function fixture(name: string): Demo {
  return JSON.parse(
    readFileSync(
      new URL(`../../test/fixtures/${name}`, import.meta.url),
      "utf8",
    ),
  ) as Demo;
}

const around = (castle: { x: number; y: number }, angle: number, far = 3) => ({
  x: castle.x + Math.cos(angle) * far,
  y: castle.y + Math.sin(angle) * far,
});

describe("shorePoint", () => {
  test("lies outside the waterline, on the line towards the target, for several islands and directions", () => {
    for (const id of [0, 1, 2, 3, 7, 12, 40]) {
      const castle = { id, x: 2.5, y: -1.25 };
      for (let step = 0; step < 24; step++) {
        const angle = (step / 24) * Math.PI * 2 + 0.1;
        const towards = around(castle, angle);
        const point = shorePoint(castle, towards);
        expect(insideWaterline(castle, point)).toBe(false);
        // Just short of it is still in the water around the island: the point is the crossing
        // plus the margin, so a point margin / 2 nearer the castle is outside the waterline too
        // but one margin * 2 nearer is inside.
        const reach = Math.hypot(point.x - castle.x, point.y - castle.y);
        const toward = (distance: number) => ({
          x: castle.x + ((point.x - castle.x) / reach) * distance,
          y: castle.y + ((point.y - castle.y) / reach) * distance,
        });
        expect(
          insideWaterline(castle, toward(reach - (SHORE_MARGIN * 2) / 1.75)),
        ).toBe(true);
        // On the line towards the target.
        const cross =
          (point.x - castle.x) * (towards.y - castle.y) -
          (point.y - castle.y) * (towards.x - castle.x);
        expect(Math.abs(cross)).toBeLessThan(1e-9);
        expect(
          (point.x - castle.x) * (towards.x - castle.x) +
            (point.y - castle.y) * (towards.y - castle.y),
        ).toBeGreaterThan(0);
      }
    }
  });

  test("uses the island's own radius in each direction", () => {
    const castle = { id: 5, x: 0, y: 0 };
    const reaches = Array.from({ length: 24 }, (_, step) =>
      toWorldDistance(
        shoreReach(castle, around(castle, (step / 24) * Math.PI * 2)),
      ),
    );
    // The islands are not round: the reach differs by direction, and is at least the smallest
    // island radius plus the beach and the margin.
    expect(Math.max(...reaches) - Math.min(...reaches)).toBeGreaterThan(0.05);
    const smallest = Math.min(
      ...islandOutline(5).map(([x, z]) => Math.hypot(x, z)),
    );
    expect(Math.min(...reaches)).toBeGreaterThan(smallest + SHORE_MARGIN);
  });

  test("a trip within one landmass leaves the island's own outline", () => {
    const first = { id: 1, x: 0, y: 0 };
    const second = { id: 2, x: 1.2, y: 0 };
    expect(sharesLandmass(first, second)).toBe(true);
    const bridged = shoreReach(first, second, true);
    expect(bridged).toBeLessThan(shoreReach(first, second, false));
    expect(bridged).toBeLessThan(1.2);
  });

  test("towards the castle itself there is no direction", () => {
    const castle = { id: 1, x: 3, y: 4 };
    expect(shorePoint(castle, castle)).toEqual({ x: 3, y: 4 });
  });
});

describe("drawnPosition", () => {
  const origin = { id: 1, x: 0, y: 0 };
  const destination = { id: 2, x: 6, y: 0 };

  test("a boat in open sea is drawn where it is", () => {
    expect(drawnPosition({ x: 3, y: 0 }, origin, destination)).toEqual({
      x: 3,
      y: 0,
    });
  });

  test("a boat near the origin is drawn at the origin's shore, near the destination at its shore", () => {
    const start = shorePoint(origin, destination);
    const end = shorePoint(destination, origin);
    expect(drawnPosition(origin, origin, destination)).toEqual(start);
    expect(drawnPosition({ x: 0.2, y: 0 }, origin, destination)).toEqual(start);
    expect(drawnPosition(destination, origin, destination)).toEqual(end);
    expect(drawnPosition({ x: 5.9, y: 0 }, origin, destination)).toEqual(end);
  });

  test("a boat just past the shore point is not moved", () => {
    const start = shorePoint(origin, destination);
    const past = { x: start.x + 0.01, y: 0 };
    expect(drawnPosition(past, origin, destination)).toEqual(past);
  });

  test("an unknown castle leaves its side alone", () => {
    expect(drawnPosition({ x: 5.9, y: 0 }, origin, undefined)).toEqual({
      x: 5.9,
      y: 0,
    });
    expect(drawnPosition({ x: 5.9, y: 0 }, undefined, destination)).toEqual(
      shorePoint(destination, origin),
    );
  });
});

describe("the fixtures' boats", () => {
  for (const name of FIXTURES) {
    test(`${name}: no group is drawn inside the waterline of its origin or destination`, () => {
      const demo = fixture(name);
      let checked = 0;
      for (const frame of demo.frames as Generation[]) {
        const castles = new Map(frame.castles.map((c) => [c.id, c]));
        for (const group of frame.soldier_groups) {
          const origin = castles.get(group.origin_castle_id);
          const destination = castles.get(group.destination_castle_id);
          const drawn = drawnPosition(group, origin, destination);
          checked++;
          for (const castle of [origin, destination]) {
            if (!castle) continue;
            // Between castles of one landmass the boat crosses the bridge by design.
            if (origin && destination && sharesLandmass(origin, destination)) {
              continue;
            }
            expect(insideWaterline(castle, drawn)).toBe(false);
          }
        }
      }
      expect(checked).toBeGreaterThan(0);
    });
  }
});
