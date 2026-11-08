import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { VertexBuffer } from "@babylonjs/core/Buffers/buffer";
import { NullEngine } from "@babylonjs/core/Engines/nullEngine";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { AbstractMesh } from "@babylonjs/core/Meshes/abstractMesh";
import { InstancedMesh } from "@babylonjs/core/Meshes/instancedMesh";
import { Scene } from "@babylonjs/core/scene";
import type { components } from "../api/generated/game";
import { ISLAND_TOP, MaterialCache } from "./art";
import { CASTLE_ATLAS_URL, CASTLE_MODEL_URLS } from "./castleAssets";
import { baseAtlasMaterial, teamAtlas } from "./castleAtlas";
import { castleDisplayLevel } from "./castleLevel";
import {
  type CastleModels,
  type CastleView,
  castleAnimationEnabled,
  castleLabelHeight,
  castleViewMeshes,
  createCastleView,
  disposeCastleView,
  loadCastleModels,
  popCastleView,
  setCastleAnimation,
  setCastleView,
} from "./castles";
import { ISLAND_RADIUS, islandRadii } from "./islands";
import { decodePng } from "./testing/png";

type Demo = components["schemas"]["Demo"];
type Castle = components["schemas"]["Castle"];

// In `bun test` an asset import is the file's path, so the tests load the committed GLBs.
const sources = {
  models: CASTLE_MODEL_URLS.map((path) => new Uint8Array(readFileSync(path))),
  atlas: async () => decodePng(readFileSync(CASTLE_ATLAS_URL)).pixels,
};

function fixture(name: string): Demo {
  return JSON.parse(
    readFileSync(
      new URL(`../../test/fixtures/${name}`, import.meta.url),
      "utf8",
    ),
  ) as Demo;
}

function castleIn(demo: Demo, frame: number, id: number): Castle {
  const castle = demo.frames[frame]?.castles.find((entry) => entry.id === id);
  if (!castle) throw new Error(`no castle ${id} in frame ${frame}`);
  return castle;
}

/** The source mesh each drawn mesh of a castle comes from, by name. */
function sourcesOf(view: CastleView): string[] {
  return castleViewMeshes(view)
    .map((mesh) =>
      mesh instanceof InstancedMesh ? mesh.sourceMesh.name : mesh.name,
    )
    .sort();
}

let engine: NullEngine;
let scene: Scene;
let models: CastleModels;
let materials: MaterialCache;

beforeAll(async () => {
  engine = new NullEngine();
  scene = new Scene(engine);
  materials = new MaterialCache(scene);
  models = await loadCastleModels(scene, sources);
});

afterAll(() => {
  scene.dispose();
  engine.dispose();
});

describe("the castle models", () => {
  test("four levels: 1 and 2 static, 3 and 4 skinned with one clip", () => {
    expect(models.levels.map((level) => level.animated)).toEqual([
      false,
      false,
      true,
      true,
    ]);
    for (const level of models.levels.slice(2)) {
      expect(
        level.container.animationGroups.map((group) => group.name),
      ).toEqual(["Take 001"]);
      expect(level.container.skeletons.length).toBeGreaterThan(0);
    }
  });

  test("each level grows from about 0.9 across, and stands on y = 0", () => {
    const across = models.levels.map((level) => {
      let low = Number.POSITIVE_INFINITY;
      let high = Number.NEGATIVE_INFINITY;
      let bottom = Number.POSITIVE_INFINITY;
      for (const mesh of level.container.meshes) {
        if (mesh.getTotalVertices() < 1000) continue;
        const box = mesh.getBoundingInfo().boundingBox;
        low = Math.min(low, box.minimumWorld.x);
        high = Math.max(high, box.maximumWorld.x);
        bottom = Math.min(bottom, box.minimumWorld.y);
      }
      expect(bottom).toBeCloseTo(0, 2);
      return high - low;
    });
    expect(across[0]).toBeCloseTo(0.9, 2);
    expect(across[1]).toBeCloseTo(0.96, 1);
    expect(across[2]).toBeCloseTo(1.16, 1);
    expect(across[3]).toBeCloseTo(1.19, 1);
    // Taller castles lift their soldier count.
    expect(castleLabelHeight(scene, 4)).toBeGreaterThan(
      castleLabelHeight(scene, 1),
    );
  });

  test("every footprint fits inside the smallest island of every fixture", () => {
    // How far the base of each model reaches from the castle's centre, in any direction.
    const reach = models.levels.map((level) => {
      let farthest = 0;
      for (const mesh of level.container.meshes) {
        const positions = mesh.getVerticesData(VertexBuffer.PositionKind);
        if (!positions || mesh.skeleton) continue;
        const world = mesh.computeWorldMatrix(true);
        for (let index = 0; index < positions.length; index += 3) {
          const point = Vector3.TransformCoordinates(
            Vector3.FromArray(positions, index),
            world,
          );
          if (point.y < 0.1) {
            farthest = Math.max(farthest, Math.hypot(point.x, point.z));
          }
        }
      }
      return farthest;
    });
    expect(reach[3]).toBeGreaterThan(0.55);
    const footprint = Math.max(...reach);
    for (const name of [
      "busy-turn.json",
      "generated-map.json",
      "six-teams.json",
    ]) {
      for (const castle of fixture(name).frames[0]?.castles ?? []) {
        const smallest = Math.min(...islandRadii(castle.id)) * ISLAND_RADIUS;
        expect(footprint).toBeLessThan(smallest);
      }
    }
    expect(footprint).toBeLessThan(0.75);
  });
});

describe("a castle on the board", () => {
  test("shows a new model when its display level changes between two fixture frames", () => {
    const demo = fixture("six-teams.json");
    const before = castleIn(demo, 0, 2);
    const after = castleIn(demo, 1, 2);
    expect(castleDisplayLevel(before.upgrades)).toBe(1);
    expect(castleDisplayLevel(after.upgrades)).toBe(2);
    const view = createCastleView(
      scene,
      before.id,
      castleDisplayLevel(before.upgrades),
      before.team_color,
      materials,
      { kind: "castle", id: before.id },
    );
    const first = sourcesOf(view);
    expect(first.every((name) => name.startsWith("castle-level-1-"))).toBe(
      true,
    );
    setCastleView(
      view,
      castleDisplayLevel(after.upgrades),
      after.team_color,
      materials,
    );
    const second = sourcesOf(view);
    expect(second.every((name) => name.startsWith("castle-level-2-"))).toBe(
      true,
    );
    // Picking still finds the castle on its new model.
    for (const mesh of castleViewMeshes(view)) {
      expect(mesh.isPickable).toBe(true);
      expect(mesh.metadata).toEqual({ kind: "castle", id: before.id });
    }
    disposeCastleView(view);
  });

  test("a capture between two fixture frames changes only the team material", () => {
    const demo = fixture("busy-turn.json");
    const before = castleIn(demo, 1, 0);
    const after = castleIn(demo, 2, 0);
    expect(before.team_color).not.toBe(after.team_color);
    const level = castleDisplayLevel(before.upgrades);
    expect(castleDisplayLevel(after.upgrades)).toBe(level);
    const view = createCastleView(
      scene,
      before.id,
      level,
      before.team_color,
      materials,
      { kind: "castle", id: before.id },
    );
    const base = baseAtlasMaterial(scene);
    const baseMeshes = castleViewMeshes(view).filter(
      (mesh) => mesh.material === base,
    );
    expect(baseMeshes.length).toBeGreaterThan(0);
    const model = view.model;
    setCastleView(view, level, after.team_color, materials);
    // The same model, the same base meshes; the team faces now use the new team's atlas.
    expect(view.model).toBe(model);
    const now = castleViewMeshes(view);
    expect(now.filter((mesh) => mesh.material === base)).toEqual(baseMeshes);
    const team = now.filter((mesh) => mesh.material !== base);
    expect(team.length).toBeGreaterThan(0);
    for (const mesh of team) {
      expect(mesh.material).toBe(teamAtlas(scene, after.team_color));
    }
    disposeCastleView(view);
  });

  for (const level of [3, 4]) {
    test(`level ${level}: a capture recolours the flags by material alone`, () => {
      const view = createCastleView(scene, 50 + level, level, 1, materials, {
        kind: "castle",
        id: 50 + level,
      });
      const meshes = [...castleViewMeshes(view)];
      const skinned = meshes.filter((mesh) => mesh.skeleton);
      expect(skinned.length).toBe(level === 3 ? 1 : 3);
      for (const flag of skinned) {
        expect(flag.material).toBe(teamAtlas(scene, 1));
      }
      setCastleView(view, level, 4, materials);
      expect(castleViewMeshes(view)).toEqual(meshes);
      for (const flag of skinned) {
        expect(flag.material).toBe(teamAtlas(scene, 4));
      }
      disposeCastleView(view);
    });
  }
});

describe("animation", () => {
  test("levels 3 and 4 loop, seeded per castle, and can be frozen", () => {
    const views = [101, 102].map((id) =>
      createCastleView(scene, id, 4, 2, materials, null),
    );
    const groups = views.map((view) => {
      const group = view.model?.loop?.group;
      if (!group) throw new Error("no loop");
      return group;
    });
    expect(
      groups.every((group) => group.isPlaying && group.loopAnimation),
    ).toBe(true);
    // Different castles start at different points of the clip.
    expect(views[0]?.model?.loop?.phase).not.toBe(views[1]?.model?.loop?.phase);
    expect(castleAnimationEnabled()).toBe(true);
    setCastleAnimation(false);
    expect(groups.some((group) => group.isPlaying)).toBe(false);
    setCastleAnimation(true);
    expect(groups.every((group) => group.isPlaying)).toBe(true);
    for (const view of views) disposeCastleView(view);
  });

  test("static levels have no loop", () => {
    const view = createCastleView(scene, 103, 2, 2, materials, null);
    expect(view.model?.loop).toBeNull();
    disposeCastleView(view);
  });

  test("a pop ends with the new model in place, and finish() ends it at once", () => {
    const view = createCastleView(scene, 104, 3, 1, materials, null);
    const pop = popCastleView(view, 2, 3);
    const meshes: AbstractMesh[] = scene.meshes.filter((mesh) =>
      mesh.name.startsWith("castle-104-"),
    );
    // The old level 2 model sinks beside the new one while the pop runs.
    expect(meshes.some((mesh) => mesh instanceof InstancedMesh)).toBe(true);
    expect(view.holder.position.y).toBeLessThan(0);
    pop.finish();
    expect(view.pop).toBeNull();
    expect(view.holder.position.y).toBeCloseTo(ISLAND_TOP, 5);
    expect(
      scene.meshes.some(
        (mesh) =>
          mesh.name.startsWith("castle-104-") && mesh instanceof InstancedMesh,
      ),
    ).toBe(false);
    disposeCastleView(view);
  });
});
