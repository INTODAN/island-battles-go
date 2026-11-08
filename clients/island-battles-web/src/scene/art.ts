import { StandardMaterial } from "@babylonjs/core/Materials/standardMaterial";
import { Color3 } from "@babylonjs/core/Maths/math.color";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { AbstractMesh } from "@babylonjs/core/Meshes/abstractMesh";
import { CreateBox } from "@babylonjs/core/Meshes/Builders/boxBuilder";
import { CreateCylinder } from "@babylonjs/core/Meshes/Builders/cylinderBuilder";
import { CreateDisc } from "@babylonjs/core/Meshes/Builders/discBuilder";
import { CreateTorus } from "@babylonjs/core/Meshes/Builders/torusBuilder";
import type { Mesh } from "@babylonjs/core/Meshes/mesh";
import { Mesh as MeshClass } from "@babylonjs/core/Meshes/mesh";
import { VertexData } from "@babylonjs/core/Meshes/mesh.vertexData";
import { TransformNode } from "@babylonjs/core/Meshes/transformNode";
import type { Scene } from "@babylonjs/core/scene";
import { ISLAND_RADIUS, ISLAND_SIDES, islandRadii } from "./islands";

// Placeholder art, each piece a few primitives. Replace these builders to change the look; the
// rest of the scene only uses their return values.

export const SEA_COLOR = "#5db2d8";
export const SAND_COLOR = "#ecdba6";
export const GRASS_COLOR = "#86bd68";
export const STONE_COLOR = "#d6ccb4";
/** The top of an island, where castles stand. */
export const ISLAND_TOP = 0.12;
const ISLAND_BOTTOM = -0.14;

export class MaterialCache {
  private readonly materials = new Map<string, StandardMaterial>();
  constructor(private readonly scene: Scene) {}

  /** A lit colour; a little emissive so the team colours stay close to the palette. */
  lit(hex: string): StandardMaterial {
    return this.get(`lit:${hex}`, hex, (material, color) => {
      material.emissiveColor = color.scale(0.22);
    });
  }

  /** For meshes that carry their own vertex colours; lighting shades them. */
  vertexColored(): StandardMaterial {
    return this.get("vertex-colors", "#ffffff", (material) => {
      material.emissiveColor = new Color3(0.22, 0.22, 0.22);
    });
  }

  /** A flat colour that lighting never changes, for lines and the sea. */
  flat(hex: string, alpha = 1): StandardMaterial {
    return this.get(`flat:${hex}:${alpha}`, hex, (material, color) => {
      material.disableLighting = true;
      material.emissiveColor = color;
      material.alpha = alpha;
    });
  }

  private get(
    key: string,
    hex: string,
    configure: (material: StandardMaterial, color: Color3) => void,
  ): StandardMaterial {
    const cached = this.materials.get(key);
    if (cached) return cached;
    const color = Color3.FromHexString(hex);
    const material = new StandardMaterial(key, this.scene);
    material.diffuseColor = color;
    material.specularColor = Color3.Black();
    material.backFaceCulling = false;
    configure(material, color);
    this.materials.set(key, material);
    return material;
  }
}

/** Marks meshes as pickable and tells the picker what they belong to. */
export interface PickTarget {
  kind: "castle" | "group";
  id: number;
}

export function markPickable(
  meshes: readonly AbstractMesh[],
  target: PickTarget,
): void {
  for (const mesh of meshes) {
    mesh.isPickable = true;
    mesh.metadata = target;
  }
}

function hexToRgba(hex: string): [number, number, number, number] {
  const color = Color3.FromHexString(hex);
  return [color.r, color.g, color.b, 1];
}

/**
 * An island blob: a sandy shore rising to a grassy top, with a rim shaped by `radii`. `stretch`
 * lengthens it along its local x axis, which land bridges use to join two castles' islands.
 */
export function createIslandBlob(
  scene: Scene,
  name: string,
  radii: readonly number[],
  radius: number,
  stretch = 1,
): Mesh {
  const sides = radii.length;
  const positions: number[] = [];
  const colors: number[] = [];
  const indices: number[] = [];
  const sand = hexToRgba(SAND_COLOR);
  const grass = hexToRgba(GRASS_COLOR);
  const ring = (scale: number, height: number, color: number[]) => {
    const start = positions.length / 3;
    for (let index = 0; index < sides; index++) {
      const angle = (index / sides) * Math.PI * 2;
      const length = (radii[index] ?? 1) * radius * scale;
      positions.push(
        Math.cos(angle) * length * stretch,
        height,
        Math.sin(angle) * length,
      );
      colors.push(...color);
    }
    return start;
  };
  const bottom = ring(1, ISLAND_BOTTOM, sand);
  const shoreTop = ring(0.88, ISLAND_TOP - 0.02, sand);
  const grassRim = ring(0.88, ISLAND_TOP - 0.02, grass);
  const topRim = ring(0.74, ISLAND_TOP, grass);
  const centre = positions.length / 3;
  positions.push(0, ISLAND_TOP, 0);
  colors.push(...grass);
  for (let index = 0; index < sides; index++) {
    const next = (index + 1) % sides;
    indices.push(bottom + index, shoreTop + index, shoreTop + next);
    indices.push(bottom + index, shoreTop + next, bottom + next);
    indices.push(grassRim + index, topRim + index, topRim + next);
    indices.push(grassRim + index, topRim + next, grassRim + next);
    indices.push(topRim + index, centre, topRim + next);
  }
  const normals: number[] = [];
  VertexData.ComputeNormals(positions, indices, normals);
  const data = new VertexData();
  data.positions = positions;
  data.indices = indices;
  data.normals = normals;
  data.colors = colors;
  const mesh = new MeshClass(name, scene);
  data.applyToMesh(mesh);
  mesh.isPickable = false;
  return mesh;
}

export function createIsland(
  scene: Scene,
  castleId: number,
  materials: MaterialCache,
): Mesh {
  const mesh = createIslandBlob(
    scene,
    `island-${castleId}`,
    islandRadii(castleId, ISLAND_SIDES),
    ISLAND_RADIUS,
  );
  mesh.material = materials.vertexColored();
  return mesh;
}

export interface CastleParts {
  root: TransformNode;
  roof: Mesh;
  flag: Mesh;
  meshes: Mesh[];
}

/**
 * A round keep with a conical roof and a banner; the roof and banner carry the team colour. It
 * stands in for a castle until the castle models of castles.ts have loaded.
 */
export function createPlaceholderCastle(
  scene: Scene,
  id: number,
  materials: MaterialCache,
): CastleParts {
  const root = new TransformNode(`castle-${id}`, scene);
  const stone = materials.lit(STONE_COLOR);
  const base = CreateCylinder(
    `castle-${id}-base`,
    { diameter: 0.9, height: 0.28, tessellation: 20 },
    scene,
  );
  base.position.y = ISLAND_TOP + 0.14;
  const keep = CreateCylinder(
    `castle-${id}-keep`,
    { diameterTop: 0.46, diameterBottom: 0.56, height: 0.3, tessellation: 16 },
    scene,
  );
  keep.position.y = ISLAND_TOP + 0.28 + 0.15;
  const roof = CreateCylinder(
    `castle-${id}-roof`,
    { diameterTop: 0, diameterBottom: 0.72, height: 0.34, tessellation: 16 },
    scene,
  );
  roof.position.y = ISLAND_TOP + 0.58 + 0.17;
  const pole = CreateCylinder(
    `castle-${id}-pole`,
    { diameter: 0.025, height: 0.34, tessellation: 6 },
    scene,
  );
  pole.position.y = ISLAND_TOP + 0.92 + 0.12;
  const flag = CreateBox(
    `castle-${id}-flag`,
    { width: 0.26, height: 0.14, depth: 0.012 },
    scene,
  );
  flag.position.set(0.14, ISLAND_TOP + 1.12, 0);
  for (const part of [base, keep, pole]) part.material = stone;
  const meshes = [base, keep, roof, pole, flag];
  for (const mesh of meshes) mesh.parent = root;
  return { root, roof, flag, meshes };
}

export interface BoatParts {
  root: TransformNode;
  /** Scales with the soldier count; the root is placed and turned. */
  body: TransformNode;
  hull: Mesh[];
  meshes: Mesh[];
}

/** A small sailing boat whose bow points along local +z. */
export function createBoat(
  scene: Scene,
  id: number,
  materials: MaterialCache,
): BoatParts {
  const root = new TransformNode(`boat-${id}`, scene);
  const body = new TransformNode(`boat-${id}-body`, scene);
  body.parent = root;
  const hullBack = CreateBox(
    `boat-${id}-hull`,
    { width: 0.2, height: 0.1, depth: 0.28 },
    scene,
  );
  hullBack.position.z = -0.04;
  const bow = CreateBox(
    `boat-${id}-bow`,
    { width: 0.14, height: 0.1, depth: 0.14 },
    scene,
  );
  bow.rotation.y = Math.PI / 4;
  bow.position.z = 0.1;
  const mast = CreateCylinder(
    `boat-${id}-mast`,
    { diameter: 0.02, height: 0.34, tessellation: 6 },
    scene,
  );
  mast.position.y = 0.2;
  mast.position.z = -0.02;
  const sail = CreateDisc(
    `boat-${id}-sail`,
    { radius: 0.17, tessellation: 3 },
    scene,
  );
  sail.rotation = new Vector3(0, Math.PI / 2, Math.PI / 2);
  sail.position.set(0, 0.24, -0.02);
  sail.material = materials.flat("#fbfbf6");
  mast.material = materials.lit("#6b5233");
  const meshes = [hullBack, bow, mast, sail];
  for (const mesh of meshes) mesh.parent = body;
  body.position.y = 0.1;
  return { root, body, hull: [hullBack, bow], meshes };
}

/** A flat ring on the sea, lying in the XZ plane. */
export function createFlatRing(
  scene: Scene,
  name: string,
  radius: number,
  thickness: number,
): Mesh {
  const ring = CreateTorus(
    name,
    { diameter: radius * 2, thickness, tessellation: 48 },
    scene,
  );
  ring.isPickable = false;
  return ring;
}
