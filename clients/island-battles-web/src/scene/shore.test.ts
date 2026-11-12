import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { components } from "../api/generated/game";
import { toWorldDistance } from "./coordinates";
import { ISLAND_TOP, islandOutline } from "./landforms";
import {
  BOARDING_SHARE,
  DECK_HEIGHT,
  drawnPosition,
  groupLook,
  insideOutline,
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

describe("groupLook: the land part of a trip (#211)", () => {
  const origin = { id: 1, x: 0, y: 0 };
  const destination = { id: 2, x: 6, y: 0 };
  const reachFrom = shoreReach(origin, destination);
  const reachTo = shoreReach(destination, origin);

  test("a group in open sea is a complete boat at its true position, with no squad", () => {
    expect(groupLook({ x: 3, y: 0 }, origin, destination)).toEqual({
      x: 3,
      y: 0,
      build: 1,
      boatScale: 1,
      squad: null,
    });
  });

  test("a group just sent is an unbuilt boat at the shore and a squad at the gate, still inside", () => {
    const look = groupLook(origin, origin, destination);
    expect(look).toMatchObject({
      ...shorePoint(origin, destination),
      build: 0,
      boatScale: 1,
    });
    expect(look.squad?.scale).toBe(0);
    expect(look.squad?.height).toBe(ISLAND_TOP);
    expect(look.squad?.facingX).toBeCloseTo(1, 9);
  });

  test("the boat is built as the squad walks to it, complete when they step down into it", () => {
    let build = 0;
    let walked = -1;
    for (let step = 1; step < 50; step++) {
      const share = step / 50;
      const look = groupLook(
        { x: reachFrom * share, y: 0 },
        origin,
        destination,
      );
      expect(look.build).toBeGreaterThanOrEqual(build);
      build = look.build;
      const squad = look.squad;
      if (!squad) throw new Error("no squad on land");
      expect(squad.x).toBeGreaterThan(walked);
      walked = squad.x;
      if (share <= 1 - BOARDING_SHARE) {
        expect(insideOutline(origin, squad)).toBe(true);
        expect(squad.height).toBe(ISLAND_TOP);
      } else {
        expect(look.build).toBe(1);
        // Stepping down into the boat: it hops off the edge and shrinks into it.
        expect(squad.scale).toBeLessThan(1);
      }
    }
    // Past the shore the squad has boarded.
    expect(
      groupLook({ x: reachFrom + 0.01, y: 0 }, origin, destination).squad,
    ).toBeNull();
  });

  test("an arriving boat docks and shrinks away while its squad walks on to the gate", () => {
    const shore = shorePoint(destination, origin);
    let scale = 1;
    let left = Number.POSITIVE_INFINITY;
    for (let step = 1; step < 50; step++) {
      const distance = reachTo * (1 - step / 50);
      const look = groupLook(
        { x: destination.x - distance, y: 0 },
        origin,
        destination,
      );
      expect(look).toMatchObject({ x: shore.x, y: shore.y, build: 1 });
      expect(look.boatScale).toBeLessThanOrEqual(scale);
      scale = look.boatScale;
      const squad = look.squad;
      if (!squad) throw new Error("no squad on land");
      const away = destination.x - squad.x;
      expect(away).toBeLessThan(left);
      left = away;
      expect(squad.facingX).toBeCloseTo(1, 9);
    }
    expect(scale).toBe(0);
    // Stepping off: it starts at the deck, at nothing, and grows.
    const off = groupLook(
      { x: destination.x - reachTo * 0.999, y: 0 },
      origin,
      destination,
    ).squad;
    expect(off?.height).toBeCloseTo(DECK_HEIGHT, 2);
    expect(off?.scale).toBeLessThan(0.01);
  });

  test("a trip within one landmass is walked the whole way, with no boat", () => {
    const near = { id: 3, x: 1.2, y: 0 };
    expect(sharesLandmass(origin, near)).toBe(true);
    for (const x of [0.1, 0.6, 1.1]) {
      const look = groupLook({ x, y: 0 }, origin, near);
      expect(look.build).toBe(0);
      expect(look.squad).toMatchObject({ x, y: 0 });
    }
  });
});

describe("the fixtures' squads", () => {
  for (const name of FIXTURES) {
    test(`${name}: every squad at rest stands on its island, and only groups near a castle have one`, () => {
      const demo = fixture(name);
      let squads = 0;
      for (const frame of demo.frames as Generation[]) {
        const castles = new Map(frame.castles.map((c) => [c.id, c]));
        for (const group of frame.soldier_groups) {
          const origin = castles.get(group.origin_castle_id);
          const destination = castles.get(group.destination_castle_id);
          const look = groupLook(group, origin, destination);
          const drawn = drawnPosition(group, origin, destination);
          // The boat is where it was drawn before #211.
          expect({ x: look.x, y: look.y }).toEqual(drawn);
          if (!origin || !destination || sharesLandmass(origin, destination)) {
            continue;
          }
          const atShore = drawn.x !== group.x || drawn.y !== group.y;
          expect(look.squad !== null).toBe(atShore);
          if (!look.squad || look.squad.height !== ISLAND_TOP) continue;
          squads++;
          expect(
            insideOutline(origin, look.squad) ||
              insideOutline(destination, look.squad),
          ).toBe(true);
        }
      }
      if (name !== "generated-map.json") expect(squads).toBeGreaterThan(0);
    });
  }
});
