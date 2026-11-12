import { Material } from "@babylonjs/core/Materials/material";
import { StandardMaterial } from "@babylonjs/core/Materials/standardMaterial";
import { Color3 } from "@babylonjs/core/Maths/math.color";
import { Matrix, Quaternion, Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { AbstractMesh } from "@babylonjs/core/Meshes/abstractMesh";
import { CreateBox } from "@babylonjs/core/Meshes/Builders/boxBuilder";
import { CreateCylinder } from "@babylonjs/core/Meshes/Builders/cylinderBuilder";
import { CreateTorusVertexData } from "@babylonjs/core/Meshes/Builders/torusBuilder";
import { Mesh } from "@babylonjs/core/Meshes/mesh";
import type { VertexData } from "@babylonjs/core/Meshes/mesh.vertexData";
import "@babylonjs/core/Meshes/thinInstanceMesh";
import { TransformNode } from "@babylonjs/core/Meshes/transformNode";
import type { Scene } from "@babylonjs/core/scene";
import {
  baseAtlasMaterial,
  hasAtlasPixels,
  onAtlasPixels,
  teamAtlas,
} from "./castleAtlas";
import { BOAT_BOW } from "./headingLines";
import { hashSeed, seededRandom } from "./islands";
import {
  BRIDGE_TOP,
  bridgeOutline,
  buildLandform,
  ISLAND_TOP,
  islandOutline,
  type PropPlacement,
} from "./landforms";
import { FlatGeometry, type Point3, type SwatchPoint, solid } from "./lowPoly";
import { teamColorHex } from "./palette";
import { attachWake } from "./wakes";

// The board's own art (issue #102): islands, boats and props built in code, low-poly and
// flat-shaded, and coloured from the castle atlas's swatches with the castles' own `castle-base`
// and `castle-team` materials (castleAtlas.ts), so everything lights and recolours the same way.

export { ISLAND_TOP } from "./landforms";

/** The open sea: the issue's shade, so the board matches the reference video. */
export const DEEP_SEA_COLOR = "#1f4f63";
/** The shallows around each island: the issue's shade. */
export const SHALLOWS_COLOR = "#4fa3a5";
/** The foam line at the shore. */
export const FOAM_COLOR = "#f4f1e6";
/** The colour behind everything: the deep sea. */
export const SEA_COLOR = DEEP_SEA_COLOR;
/** The colour the castle placeholders stand on before the atlas arrives. */
export const STONE_COLOR = "#d6ccb4";
/** The wood of a boat before the atlas arrives. */
const WOOD_COLOR = "#9b6a47";

/** How far a boat's body sits above the sea, so the hull's keel is just under the water. */
export const BOAT_FLOAT = 0.04;

export class MaterialCache {
  private readonly materials = new Map<string, StandardMaterial>();
  /** Meshes waiting for the atlas, with the team their material is for (null: castle-base). */
  private readonly pending = new Map<AbstractMesh, number | null>();

  constructor(private readonly scene: Scene) {
    onAtlasPixels(scene, () => this.flush());
  }

  /** A lit colour; a little emissive so the team colours stay close to the palette. */
  lit(hex: string): StandardMaterial {
    return this.get(`lit:${hex}`, hex, (material, color) => {
      material.emissiveColor = color.scale(0.22);
    });
  }

  /** A flat colour that lighting never changes, for lines and rings. */
  flat(hex: string, alpha = 1): StandardMaterial {
    return this.get(`flat:${hex}:${alpha}`, hex, (material, color) => {
      material.disableLighting = true;
      material.emissiveColor = color;
      material.alpha = alpha;
    });
  }

  /**
   * A flat colour drawn over the scene in a fixed order: alpha-blended, so meshes using it are
   * sorted by `alphaIndex`, and writing no depth, so they never hide one another by depth.
   */
  overlay(hex: string): StandardMaterial {
    return this.get(`overlay:${hex}`, hex, (material, color) => {
      material.disableLighting = true;
      material.emissiveColor = color;
      material.transparencyMode = Material.MATERIAL_ALPHABLEND;
      material.disableDepthWrite = true;
    });
  }

  /** Gives `mesh` the shared `castle-base` atlas material, now or when the atlas arrives. */
  useBase(mesh: AbstractMesh, fallback = STONE_COLOR): void {
    if (hasAtlasPixels(this.scene)) {
      this.pending.delete(mesh);
      mesh.material = baseAtlasMaterial(this.scene);
      return;
    }
    mesh.material = this.lit(fallback);
    this.pending.set(mesh, null);
  }

  /** Gives `mesh` the team's `castle-team` atlas material, now or when the atlas arrives. */
  useTeam(mesh: AbstractMesh, team: number): void {
    if (hasAtlasPixels(this.scene)) {
      this.pending.delete(mesh);
      mesh.material = teamAtlas(this.scene, team);
      return;
    }
    mesh.material = this.lit(teamColorHex(team));
    this.pending.set(mesh, team);
  }

  private flush(): void {
    for (const [mesh, team] of this.pending) {
      if (mesh.isDisposed()) continue;
      mesh.material =
        team === null
          ? baseAtlasMaterial(this.scene)
          : teamAtlas(this.scene, team);
    }
    this.pending.clear();
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

function meshOf(scene: Scene, name: string, geometry: FlatGeometry): Mesh {
  const mesh = new Mesh(name, scene);
  geometry.toVertexData().applyToMesh(mesh);
  mesh.isPickable = false;
  return mesh;
}

/** A castle's island: faceted cliffs, a grass top with a flat plateau, and a sand beach. */
export function createIsland(
  scene: Scene,
  castleId: number,
  materials: MaterialCache,
): Mesh {
  const mesh = meshOf(
    scene,
    `island-${castleId}`,
    buildLandform(islandOutline(castleId), ISLAND_TOP, hashSeed(castleId, 7)),
  );
  materials.useBase(mesh, "#5b7a45");
  mesh.receiveShadows = true;
  return mesh;
}

/**
 * The low land joining two castles of one landmass, `length` apart, along local x. The board
 * places it at their midpoint and turns it to face from one to the other.
 */
export function createBridge(
  scene: Scene,
  key: string,
  length: number,
  seed: number,
  materials: MaterialCache,
): Mesh {
  const mesh = meshOf(
    scene,
    `bridge-${key}`,
    buildLandform(bridgeOutline(length, seed), BRIDGE_TOP, seed),
  );
  materials.useBase(mesh, "#5b7a45");
  mesh.receiveShadows = true;
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

// ---- boats -----------------------------------------------------------------------------------

export interface BoatParts {
  root: TransformNode;
  /** Scales with the soldier count; the root is placed and turned. */
  body: TransformNode;
  /** The wooden hull and mast, on `castle-base`. */
  hull: Mesh[];
  /** The sail and pennant, on the team's `castle-team`. */
  sail: Mesh[];
  /** The foam trail behind the boat, shown while it moves. */
  wake: Mesh;
  meshes: Mesh[];
}

/** The hull's cross-sections from stern to bow: z, half-width at the deck and at the bottom. */
const HULL_SECTIONS: readonly [number, number, number][] = [
  [-0.17, 0.085, 0.05],
  [-0.03, 0.105, 0.062],
  [0.1, 0.082, 0.042],
  [BOAT_BOW, 0.0, 0.0],
];
/**
 * How far along the team's stone swatch the sails sample, at their top and bottom: close to its
 * top row, the team colour itself, as roofs are.
 */
export const SAIL_ALONG: readonly [number, number] = [0.005, 0.035];
const DECK_Y = 0.085;
const KEEL_Y = -0.04;
const MAST_Z = -0.015;
const MAST_TOP = 0.5;

let cachedBoat: { hull: FlatGeometry; sail: FlatGeometry } | null = null;

/** The boat's two pieces, built once: the wooden hull and mast, and the sail and pennant. */
export function boatGeometry(): { hull: FlatGeometry; sail: FlatGeometry } {
  if (cachedBoat) return cachedBoat;
  const hull = new FlatGeometry();
  const wood = (along: number): SwatchPoint => ({ swatch: "wood", along });
  const side = (y: number) =>
    wood(0.32 + ((DECK_Y - y) / (DECK_Y - KEEL_Y)) * 0.5);
  for (let index = 0; index < HULL_SECTIONS.length - 1; index++) {
    const [z0, deck0, bottom0] = HULL_SECTIONS[index] as [
      number,
      number,
      number,
    ];
    const [z1, deck1, bottom1] = HULL_SECTIONS[index + 1] as [
      number,
      number,
      number,
    ];
    const keel1 = index === HULL_SECTIONS.length - 2 ? 0.0 : KEEL_Y;
    for (const sign of [1, -1]) {
      const a: Point3 = [sign * deck0, DECK_Y, z0];
      const b: Point3 = [sign * deck1, DECK_Y + (index === 2 ? 0.02 : 0), z1];
      const c: Point3 = [sign * bottom1, keel1, z1];
      const d: Point3 = [sign * bottom0, KEEL_Y, z0];
      hull.quad(
        a,
        b,
        c,
        d,
        [side(a[1]), side(b[1]), side(c[1]), side(d[1])],
        [sign, 0, 0],
      );
    }
    // The bottom and the deck.
    hull.quad(
      [bottom0, KEEL_Y, z0],
      [bottom1, keel1, z1],
      [-bottom1, keel1, z1],
      [-bottom0, KEEL_Y, z0],
      solid("wood", 0.9),
      [0, -1, 0],
    );
    hull.quad(
      [deck0, DECK_Y, z0],
      [deck1, DECK_Y + (index === 2 ? 0.02 : 0), z1],
      [-deck1, DECK_Y + (index === 2 ? 0.02 : 0), z1],
      [-deck0, DECK_Y, z0],
      solid("wood", 0.12),
      [0, 1, 0],
    );
  }
  const [sternZ, sternDeck, sternBottom] = HULL_SECTIONS[0] as [
    number,
    number,
    number,
  ];
  hull.quad(
    [sternDeck, DECK_Y, sternZ],
    [-sternDeck, DECK_Y, sternZ],
    [-sternBottom, KEEL_Y, sternZ],
    [sternBottom, KEEL_Y, sternZ],
    solid("wood", 0.55),
    [0, 0, -1],
  );
  // The mast: a thin square post.
  const m = 0.012;
  const corners: [number, number][] = [
    [m, MAST_Z + m],
    [-m, MAST_Z + m],
    [-m, MAST_Z - m],
    [m, MAST_Z - m],
  ];
  for (let index = 0; index < 4; index++) {
    const [ax, az] = corners[index] as [number, number];
    const [bx, bz] = corners[(index + 1) % 4] as [number, number];
    hull.quad(
      [ax, DECK_Y, az],
      [bx, DECK_Y, bz],
      [bx, MAST_TOP, bz],
      [ax, MAST_TOP, az],
      solid("wood", 0.7),
      [(ax + bx) / 2, 0, (az + bz) / 2 - MAST_Z],
    );
  }

  // A square sail across the hull, billowing forward, and a pennant streaming back from the
  // masthead. Both are drawn from both sides.
  const sail = new FlatGeometry();
  const cloth = (along: number): SwatchPoint => ({ swatch: "stone", along });
  const top = 0.44;
  const bottom = 0.17;
  const half = 0.13;
  const billow = 0.05;
  const z = MAST_Z + 0.02;
  const panels: [Point3, Point3, Point3, Point3][] = [
    [
      [0, top, z + billow * 0.6],
      [half, top - 0.02, z],
      [half * 1.08, bottom, z],
      [0, bottom, z + billow],
    ],
    [
      [-half, top - 0.02, z],
      [0, top, z + billow * 0.6],
      [0, bottom, z + billow],
      [-half * 1.08, bottom, z],
    ],
  ];
  const [clothTop, clothBottom] = SAIL_ALONG;
  for (const [a, b, c, d] of panels) {
    const colours: [SwatchPoint, SwatchPoint, SwatchPoint, SwatchPoint] = [
      cloth(clothTop),
      cloth(clothTop),
      cloth(clothBottom),
      cloth(clothBottom),
    ];
    sail.quad(a, b, c, d, colours, [0, 0, 1]);
    sail.quad(a, b, c, d, colours, [0, 0, -1]);
  }
  // A jib from the masthead to the bow, along the hull, so a boat sailing across the screen
  // shows its colour too.
  const jib: [Point3, Point3, Point3] = [
    [0.01, top - 0.01, MAST_Z + 0.03],
    [0.02, DECK_Y + 0.05, MAST_Z + 0.06],
    [0.0, DECK_Y + 0.04, 0.2],
  ];
  const jibColours: [SwatchPoint, SwatchPoint, SwatchPoint] = [
    cloth(clothTop),
    cloth(clothBottom),
    cloth(clothBottom),
  ];
  sail.triangle(...jib, jibColours, [1, 0, 0]);
  sail.triangle(...jib, jibColours, [-1, 0, 0]);
  const flag = { swatch: "flags", along: 0.1 } as const;
  const pennant: [Point3, Point3, Point3] = [
    [0, MAST_TOP, MAST_Z],
    [0, MAST_TOP - 0.07, MAST_Z],
    [0, MAST_TOP - 0.03, MAST_Z - 0.14],
  ];
  sail.triangle(...pennant, [flag, flag, flag], [1, 0, 0]);
  sail.triangle(...pennant, [flag, flag, flag], [-1, 0, 0]);
  cachedBoat = { hull, sail };
  return cachedBoat;
}

/** A small sailing boat whose bow points along local +z, in the team's colours. */
export function createBoat(
  scene: Scene,
  id: number,
  materials: MaterialCache,
  team = 0,
): BoatParts {
  const root = new TransformNode(`boat-${id}`, scene);
  const body = new TransformNode(`boat-${id}-body`, scene);
  body.parent = root;
  body.position.y = BOAT_FLOAT;
  const geometry = boatGeometry();
  const hull = meshOf(scene, `boat-${id}-hull`, geometry.hull);
  const sail = meshOf(scene, `boat-${id}-sail`, geometry.sail);
  hull.parent = body;
  sail.parent = body;
  materials.useBase(hull, WOOD_COLOR);
  materials.useTeam(sail, team);
  const wake = attachWake(scene, id, root);
  return {
    root,
    body,
    hull: [hull],
    sail: [sail],
    wake,
    meshes: [hull, sail],
  };
}

/** Recolours a boat's sail and pennant for `team`; the hull stays wood. */
export function setBoatTeam(
  parts: BoatParts,
  team: number,
  materials: MaterialCache,
): void {
  for (const mesh of parts.sail) materials.useTeam(mesh, team);
}

export function disposeBoat(parts: BoatParts): void {
  for (const mesh of parts.meshes) mesh.dispose();
  parts.wake.dispose();
  parts.body.dispose();
  parts.root.dispose();
}

// ---- props -----------------------------------------------------------------------------------

/** A low-poly pine: a short trunk and two stacked cones of leaves, about 0.32 high. */
export function treeGeometry(): FlatGeometry {
  const geometry = new FlatGeometry();
  const sides = 6;
  const ring = (radius: number, y: number, turn = 0): Point3[] =>
    Array.from({ length: sides }, (_, index) => {
      const angle = ((index + turn) / sides) * Math.PI * 2;
      return [Math.cos(angle) * radius, y, Math.sin(angle) * radius] as const;
    });
  const trunkBottom = ring(0.026, -0.01);
  const trunkTop = ring(0.022, 0.08);
  for (let index = 0; index < sides; index++) {
    const next = (index + 1) % sides;
    const a = trunkBottom[index] as Point3;
    const b = trunkBottom[next] as Point3;
    const c = trunkTop[next] as Point3;
    const d = trunkTop[index] as Point3;
    geometry.quad(a, b, c, d, solid("wood", 0.75), [
      a[0] + b[0],
      0,
      a[2] + b[2],
    ]);
  }
  const cone = (base: number, radius: number, height: number, turn: number) => {
    const rim = ring(radius, base, turn);
    const tip: Point3 = [0, base + height, 0];
    const leaf = (y: number): SwatchPoint => ({
      swatch: "green",
      along: 0.12 + ((y - base) / height) * 0.45,
    });
    for (let index = 0; index < sides; index++) {
      const a = rim[index] as Point3;
      const b = rim[(index + 1) % sides] as Point3;
      geometry.triangle(
        a,
        b,
        tip,
        [leaf(a[1]), leaf(b[1]), leaf(tip[1])],
        [a[0] + b[0], radius * 0.6, a[2] + b[2]],
      );
      geometry.triangle(
        a,
        b,
        [0, base, 0],
        [leaf(base), leaf(base), leaf(base)],
        [0, -1, 0],
      );
    }
  };
  cone(0.06, 0.12, 0.17, 0);
  cone(0.16, 0.085, 0.16, 0.5);
  return geometry;
}

/** A low-poly boulder of grey cliff rock, about 0.14 across. */
export function rockGeometry(): FlatGeometry {
  const geometry = new FlatGeometry();
  const random = seededRandom(1020);
  const sides = 6;
  const base = Array.from({ length: sides }, (_, index) => {
    const angle = (index / sides) * Math.PI * 2;
    const radius = 0.065 * (0.8 + random() * 0.4);
    return [Math.cos(angle) * radius, -0.02, Math.sin(angle) * radius] as const;
  });
  const middle = Array.from({ length: sides }, (_, index) => {
    const angle = ((index + 0.5) / sides) * Math.PI * 2;
    const radius = 0.055 * (0.8 + random() * 0.4);
    return [
      Math.cos(angle) * radius,
      0.04 + random() * 0.02,
      Math.sin(angle) * radius,
    ] as const;
  });
  const top: Point3 = [0.01, 0.085, -0.005];
  const grey = (y: number): SwatchPoint => ({
    swatch: "cliff",
    along: 0.18 + y * 3,
  });
  for (let index = 0; index < sides; index++) {
    const next = (index + 1) % sides;
    const a = base[index] as Point3;
    const b = base[next] as Point3;
    const m = middle[index] as Point3;
    const n = middle[next] as Point3;
    geometry.triangle(
      a,
      b,
      m,
      [grey(a[1]), grey(b[1]), grey(m[1])],
      [m[0], 0.2, m[2]],
    );
    geometry.triangle(
      b,
      n,
      m,
      [grey(b[1]), grey(n[1]), grey(m[1])],
      [b[0] + n[0], 0.3, b[2] + n[2]],
    );
    geometry.triangle(
      m,
      n,
      top,
      [grey(m[1]), grey(n[1]), grey(top[1])],
      [m[0] + n[0], 0.6, m[2] + n[2]],
    );
  }
  return geometry;
}

/** The two prop meshes of a scene, drawn as thin instances: one draw call each. */
export interface PropMeshes {
  tree: Mesh;
  rock: Mesh;
}

export function createPropMeshes(
  scene: Scene,
  materials: MaterialCache,
): PropMeshes {
  const tree = meshOf(scene, "props-tree", treeGeometry());
  const rock = meshOf(scene, "props-rock", rockGeometry());
  for (const mesh of [tree, rock]) {
    materials.useBase(mesh, "#6d8a50");
    mesh.receiveShadows = true;
    mesh.alwaysSelectAsActiveMesh = true;
  }
  return { tree, rock };
}

/** Puts one thin instance of each prop at its placement, replacing any before. */
export function placeProps(
  meshes: PropMeshes,
  props: readonly PropPlacement[],
): void {
  for (const kind of ["tree", "rock"] as const) {
    const mesh = meshes[kind];
    const chosen = props.filter((prop) => prop.kind === kind);
    const matrices = new Float32Array(chosen.length * 16);
    chosen.forEach((prop, index) => {
      Matrix.Compose(
        new Vector3(prop.scale, prop.scale, prop.scale),
        Quaternion.RotationAxis(Vector3.Up(), prop.yaw),
        new Vector3(prop.x, prop.y, prop.z),
      ).copyToArray(matrices, index * 16);
    });
    mesh.thinInstanceSetBuffer("matrix", matrices, 16, true);
    mesh.setEnabled(chosen.length > 0);
  }
}

/** The shapes of the rings made so far, by size: a turn's clashes reuse them (#176). */
const ringShapes = new Map<string, VertexData>();

/** A flat ring on the sea, lying in the XZ plane. */
export function createFlatRing(
  scene: Scene,
  name: string,
  radius: number,
  thickness: number,
): Mesh {
  const key = `${radius}:${thickness}`;
  let shape = ringShapes.get(key);
  if (!shape) {
    shape = CreateTorusVertexData({
      diameter: radius * 2,
      thickness,
      tessellation: 48,
    });
    ringShapes.set(key, shape);
  }
  const ring = new Mesh(name, scene);
  shape.applyToMesh(ring);
  ring.isPickable = false;
  return ring;
}

// ---- squads (#211) ---------------------------------------------------------------------------

/** A group on land: a few small soldiers, drawn while it walks between its castle and boat. */
export interface SquadParts {
  /** Placed on the island and turned to face where the squad walks. */
  root: TransformNode;
  /** Scaled as the squad comes out of a gate or steps into a boat, and bobbed as it walks. */
  body: TransformNode;
  /** Legs, faces and spears, on `castle-base`. */
  base: Mesh;
  /** Tunics and helmet plumes, on the team's `castle-team`. */
  team: Mesh;
  /** How many soldiers it shows. */
  size: number;
}

/** The most soldiers a squad shows. */
export const MAX_SQUAD_SIZE = 5;

/** How many soldiers a squad of `soldiers` shows: 1 for 1–4, one more per doubling, 5 at most. */
export function squadSize(soldiers: number): number {
  let size = 1;
  for (let threshold = 5; soldiers >= threshold; threshold *= 2) size++;
  return Math.min(MAX_SQUAD_SIZE, size);
}

/** Where each soldier of a squad of `size` stands, in a small column facing +z. */
export function squadFormation(size: number): readonly [number, number][] {
  const step = 0.05;
  const layouts: Record<number, [number, number][]> = {
    1: [[0, 0]],
    2: [
      [-step / 2, 0],
      [step / 2, 0],
    ],
    3: [
      [0, step * 0.6],
      [-step / 2, -step * 0.4],
      [step / 2, -step * 0.4],
    ],
    4: [
      [-step / 2, step / 2],
      [step / 2, step / 2],
      [-step / 2, -step / 2],
      [step / 2, -step / 2],
    ],
    5: [
      [0, step * 1.1],
      [-step / 2, 0.1 * step],
      [step / 2, 0.1 * step],
      [-step / 2, -step * 0.9],
      [step / 2, -step * 0.9],
    ],
  };
  return layouts[Math.min(MAX_SQUAD_SIZE, Math.max(1, size))] ?? [[0, 0]];
}

/** An axis-aligned box from `low` to `high`, every face one atlas colour. */
function box(
  geometry: FlatGeometry,
  low: Point3,
  high: Point3,
  colour: SwatchPoint,
): void {
  const [x0, y0, z0] = low;
  const [x1, y1, z1] = high;
  const c = [colour, colour, colour, colour] as const;
  geometry.quad(
    [x0, y1, z0],
    [x1, y1, z0],
    [x1, y1, z1],
    [x0, y1, z1],
    c,
    [0, 1, 0],
  );
  geometry.quad(
    [x0, y0, z0],
    [x1, y0, z0],
    [x1, y0, z1],
    [x0, y0, z1],
    c,
    [0, -1, 0],
  );
  geometry.quad(
    [x0, y0, z1],
    [x1, y0, z1],
    [x1, y1, z1],
    [x0, y1, z1],
    c,
    [0, 0, 1],
  );
  geometry.quad(
    [x0, y0, z0],
    [x1, y0, z0],
    [x1, y1, z0],
    [x0, y1, z0],
    c,
    [0, 0, -1],
  );
  geometry.quad(
    [x1, y0, z0],
    [x1, y1, z0],
    [x1, y1, z1],
    [x1, y0, z1],
    c,
    [1, 0, 0],
  );
  geometry.quad(
    [x0, y0, z0],
    [x0, y1, z0],
    [x0, y1, z1],
    [x0, y0, z1],
    c,
    [-1, 0, 0],
  );
}

const squadGeometries = new Map<
  number,
  { base: FlatGeometry; team: FlatGeometry }
>();

/**
 * The soldiers of a squad of `size`, built once per size: on `castle-base` the legs, the face
 * and a spear held upright; on the team's `castle-team` the tunic and a plume on the helmet.
 * A soldier is about 0.12 world units tall, a third of a boat's length.
 */
export function squadGeometry(size: number): {
  base: FlatGeometry;
  team: FlatGeometry;
} {
  const cached = squadGeometries.get(size);
  if (cached) return cached;
  const base = new FlatGeometry();
  const team = new FlatGeometry();
  const legs = { swatch: "wood", along: 0.85 } as const;
  const face = { swatch: "cliff", along: 0.92 } as const;
  const spear = { swatch: "wood", along: 0.45 } as const;
  const helmet = { swatch: "cliff", along: 0.35 } as const;
  const tunic = { swatch: "stone", along: SAIL_ALONG[0] } as const;
  const plume = { swatch: "flags", along: 0.1 } as const;
  for (const [x, z] of squadFormation(size)) {
    box(base, [x - 0.013, 0, z - 0.009], [x - 0.002, 0.042, z + 0.009], legs);
    box(base, [x + 0.002, 0, z - 0.009], [x + 0.013, 0.042, z + 0.009], legs);
    box(
      team,
      [x - 0.019, 0.04, z - 0.012],
      [x + 0.019, 0.083, z + 0.012],
      tunic,
    );
    box(
      base,
      [x - 0.011, 0.083, z - 0.011],
      [x + 0.011, 0.103, z + 0.011],
      face,
    );
    box(
      base,
      [x - 0.013, 0.101, z - 0.013],
      [x + 0.013, 0.112, z + 0.013],
      helmet,
    );
    box(
      team,
      [x - 0.003, 0.112, z - 0.012],
      [x + 0.003, 0.126, z + 0.006],
      plume,
    );
    box(
      base,
      [x + 0.022, 0.02, z + 0.004],
      [x + 0.027, 0.16, z + 0.009],
      spear,
    );
  }
  const built = { base, team };
  squadGeometries.set(size, built);
  return built;
}

/** A squad of soldiers for group `id`, in the team's colours; hidden until it is placed. */
export function createSquad(
  scene: Scene,
  id: number,
  materials: MaterialCache,
  team: number,
  size: number,
): SquadParts {
  const root = new TransformNode(`squad-${id}`, scene);
  const body = new TransformNode(`squad-${id}-body`, scene);
  body.parent = root;
  const geometry = squadGeometry(size);
  const base = meshOf(scene, `squad-${id}-base`, geometry.base);
  const teamMesh = meshOf(scene, `squad-${id}-team`, geometry.team);
  base.parent = body;
  teamMesh.parent = body;
  materials.useBase(base, WOOD_COLOR);
  materials.useTeam(teamMesh, team);
  root.setEnabled(false);
  return { root, body, base, team: teamMesh, size };
}

/** Shows `size` soldiers: the squad's meshes take that size's geometry. */
export function setSquadSize(parts: SquadParts, size: number): void {
  if (parts.size === size) return;
  const geometry = squadGeometry(size);
  geometry.base.toVertexData().applyToMesh(parts.base);
  geometry.team.toVertexData().applyToMesh(parts.team);
  parts.size = size;
}

export function setSquadTeam(
  parts: SquadParts,
  team: number,
  materials: MaterialCache,
): void {
  materials.useTeam(parts.team, team);
}

export function disposeSquad(parts: SquadParts): void {
  parts.base.dispose();
  parts.team.dispose();
  parts.body.dispose();
  parts.root.dispose();
}
