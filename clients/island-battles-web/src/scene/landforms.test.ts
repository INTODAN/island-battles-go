import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { components } from "../api/generated/game";
import { ATLAS_SWATCHES } from "./atlasUv";
import { ATLAS_SIZE } from "./castleAtlas";
import { toWorld } from "./coordinates";
import { computeHeadings, distanceToSegment } from "./headingLines";
import {
  BRIDGE_TOP,
  bridgeOutline,
  buildLandform,
  ISLAND_TOP,
  inPropFreeSector,
  islandOutline,
  islandProps,
  LIP,
  type Outline,
  offsetOutline,
  PLATEAU_RADIUS,
  PROP_CLEARANCE,
  waterline,
} from "./landforms";

type Demo = components["schemas"]["Demo"];

const FIXTURES = ["busy-turn.json", "generated-map.json", "six-teams.json"];

function fixture(name: string): Demo {
  return JSON.parse(
    readFileSync(
      new URL(`../../test/fixtures/${name}`, import.meta.url),
      "utf8",
    ),
  ) as Demo;
}

/** The nearest an outline's edges come to its centre. */
function innerReach(outline: Outline): number {
  let nearest = Number.POSITIVE_INFINITY;
  for (let index = 0; index < outline.length; index++) {
    const [ax, az] = outline[index] as [number, number];
    const [bx, bz] = outline[(index + 1) % outline.length] as [number, number];
    nearest = Math.min(
      nearest,
      distanceToSegment({ x: 0, y: 0 }, { x: ax, y: az }, { x: bx, y: bz }),
    );
  }
  return nearest;
}

describe("islands (#102)", () => {
  test("the plateau is wide enough for the level 4 castle, whose base reaches 0.745", () => {
    expect(PLATEAU_RADIUS).toBeGreaterThanOrEqual(0.75);
  });

  test("every island's flat grass reaches past the plateau in every direction", () => {
    for (let id = 0; id < 300; id++) {
      const flat = offsetOutline(islandOutline(id), -LIP);
      expect(innerReach(flat)).toBeGreaterThanOrEqual(PLATEAU_RADIUS);
    }
  });

  test("the flat grass is flat: every vertex within the plateau is at the island's top", () => {
    for (const id of [0, 3, 17, 42]) {
      const geometry = buildLandform(islandOutline(id), ISLAND_TOP, id);
      const { positions } = geometry;
      for (let index = 0; index < positions.length; index += 3) {
        const x = positions[index] ?? 0;
        const y = positions[index + 1] ?? 0;
        const z = positions[index + 2] ?? 0;
        if (Math.hypot(x, z) <= PLATEAU_RADIUS)
          expect(y).toBeCloseTo(ISLAND_TOP, 10);
      }
    }
  });

  test("an island is the same every time it is built", () => {
    const first = buildLandform(islandOutline(9), ISLAND_TOP, 9);
    const second = buildLandform(islandOutline(9), ISLAND_TOP, 9);
    expect(first.positions).toEqual(second.positions);
    expect(first.uvs).toEqual(second.uvs);
    expect(first.colors).toEqual(second.colors);
  });

  test("faces are flat-shaded and samples stay on the atlas's swatches", () => {
    const geometry = buildLandform(islandOutline(5), ISLAND_TOP, 5);
    const { normals, uvs } = geometry;
    for (let triangle = 0; triangle < geometry.triangleCount; triangle++) {
      const base = triangle * 9;
      for (let corner = 1; corner < 3; corner++) {
        for (let axis = 0; axis < 3; axis++) {
          expect(normals[base + corner * 3 + axis]).toBe(
            normals[base + axis] as number,
          );
        }
      }
    }
    const rects = Object.values(ATLAS_SWATCHES);
    for (let index = 0; index < uvs.length; index += 2) {
      const x = (uvs[index] ?? 0) * ATLAS_SIZE;
      const y = (uvs[index + 1] ?? 0) * ATLAS_SIZE;
      expect(
        rects.some(
          (rect) => x > rect.x0 && x < rect.x1 && y >= rect.y0 && y <= rect.y1,
        ),
      ).toBe(true);
    }
  });

  test("the grass faces up, the cliffs face out, and nothing faces down", () => {
    const geometry = buildLandform(islandOutline(11), ISLAND_TOP, 11);
    const { normals, positions } = geometry;
    for (let index = 0; index < normals.length; index += 9) {
      const nx = normals[index] ?? 0;
      const ny = normals[index + 1] ?? 0;
      const nz = normals[index + 2] ?? 0;
      expect(ny).toBeGreaterThan(-0.01);
      // Facing away from the island's middle, or up.
      const x = positions[index] ?? 0;
      const z = positions[index + 2] ?? 0;
      expect(nx * x + nz * z + ny).toBeGreaterThan(0);
    }
  });

  test("the waterline lies outside the cliff, where the beach meets the sea", () => {
    const outline = islandOutline(2);
    const shore = waterline(outline);
    outline.forEach(([x, z], index) => {
      const [wx, wz] = shore[index] as [number, number];
      expect(Math.hypot(wx, wz)).toBeGreaterThan(Math.hypot(x, z));
    });
  });

  test("a land bridge is a low capsule along x, as long as its two castles are apart", () => {
    const outline = bridgeOutline(1.2, 77);
    const xs = outline.map(([x]) => x);
    expect(Math.max(...xs)).toBeGreaterThan(0.6);
    expect(Math.min(...xs)).toBeLessThan(-0.6);
    expect(bridgeOutline(1.2, 77)).toEqual(outline);
    const geometry = buildLandform(outline, BRIDGE_TOP, 77);
    const heights = geometry.positions.filter((_, index) => index % 3 === 1);
    expect(Math.max(...heights)).toBeCloseTo(BRIDGE_TOP, 10);
    expect(BRIDGE_TOP).toBeLessThan(ISLAND_TOP);
  });
});

describe("props (#102)", () => {
  for (const name of FIXTURES) {
    const demo = fixture(name);
    const castles = demo.frames[0]?.castles ?? [];
    const props = islandProps(castles);

    test(`${name}: seeded, so the same castles get the same props`, () => {
      expect(islandProps([...castles].reverse())).toEqual(props);
      expect(props.length).toBeGreaterThan(0);
    });

    test(`${name}: no prop on any castle's plateau or in its label sectors`, () => {
      for (const prop of props) {
        for (const castle of castles) {
          const world = toWorld(castle);
          const distance = Math.hypot(prop.x - world.x, prop.z - world.z);
          expect(distance).toBeGreaterThanOrEqual(PROP_CLEARANCE);
          // The name label hangs 0.8 in front of the castle.
          expect(
            Math.hypot(prop.x - world.x, prop.z - (world.z - 0.8)),
          ).toBeGreaterThan(0.25);
        }
        const own = castles.find((castle) => castle.id === prop.castleId);
        if (!own) throw new Error("a prop with no castle");
        const world = toWorld(own);
        expect(
          inPropFreeSector(Math.atan2(prop.z - world.z, prop.x - world.x)),
        ).toBe(false);
      }
    });

    test(`${name}: no prop under the end of any heading line`, () => {
      for (const frame of demo.frames) {
        for (const segment of computeHeadings(
          frame.castles,
          frame.soldier_groups,
        )) {
          const tip = toWorld(segment.tip);
          for (const prop of props) {
            expect(Math.hypot(prop.x - tip.x, prop.z - tip.z)).toBeGreaterThan(
              0.2,
            );
          }
        }
      }
    });
  }
});
