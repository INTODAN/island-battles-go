import type { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import { DirectionalLight } from "@babylonjs/core/Lights/directionalLight";
import { HemisphericLight } from "@babylonjs/core/Lights/hemisphericLight";
import { Color3, Color4 } from "@babylonjs/core/Maths/math.color";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import { CreateGround } from "@babylonjs/core/Meshes/Builders/groundBuilder";
import type { Mesh } from "@babylonjs/core/Meshes/mesh";
import { Mesh as MeshClass } from "@babylonjs/core/Meshes/mesh";
import { VertexData } from "@babylonjs/core/Meshes/mesh.vertexData";
import { TransformNode } from "@babylonjs/core/Meshes/transformNode";
import type { Scene } from "@babylonjs/core/scene";
import type { components } from "../api/generated/game";
import {
  type BoatParts,
  type CastleParts,
  createBoat,
  createCastle,
  createFlatRing,
  createIsland,
  createIslandBlob,
  createUpgradeRing,
  ISLAND_TOP,
  MaterialCache,
  markPickable,
  SEA_COLOR,
} from "./art";
import { type BoardBounds, boardBounds } from "./board";
import { createBoardCamera, fitCamera } from "./camera";
import { toWorld } from "./coordinates";
import { castleTitle, upgradeLevels, upgradeRingCount } from "./format";
import {
  computeHeadings,
  type HeadingSegment,
  headingStyle,
  ribbonGeometry,
} from "./headingLines";
import { hashSeed, islandRadii, landmassLinks } from "./islands";
import { LabelLayer, type LabelSpec } from "./labelLayer";
import { CLASH_COLOR, LOSS_COLOR, teamColorHex, tint } from "./palette";

type Generation = components["schemas"]["Generation"];
type GameTeam = components["schemas"]["GameTeam"];
type Castle = components["schemas"]["Castle"];
type SoldierGroup = components["schemas"]["SoldierGroup"];

/** The most upgrade rings drawn around a castle; the label still gives the exact levels. */
const MAX_RINGS = 6;

interface CastleEntity {
  parts: CastleParts;
  rings: Mesh[];
  team: number;
}

interface GroupEntity {
  parts: BoatParts;
}

interface ClashEntity {
  ring: Mesh;
  ghost: BoatParts | null;
}

interface HeadingEntity {
  line: Mesh;
  casing: Mesh;
}

/** Everything the scene keeps between generations, found by entity ID so meshes are reused. */
export interface Battlefield {
  scene: Scene;
  camera: ArcRotateCamera;
  materials: MaterialCache;
  labels: LabelLayer;
  bounds: BoardBounds;
  generation: Generation | null;
  teams: readonly GameTeam[];
  castles: Map<number, CastleEntity>;
  islands: Map<number, Mesh>;
  bridges: Map<string, Mesh>;
  groups: Map<number, GroupEntity>;
  clashes: Map<string, ClashEntity>;
  headings: Map<number, HeadingEntity>;
  /** The headings of the last render, for tests. */
  headingSegments: HeadingSegment[];
}

const battlefields = new WeakMap<Scene, Battlefield>();

export function battlefieldOf(scene: Scene): Battlefield | undefined {
  return battlefields.get(scene);
}

function createBattlefield(scene: Scene): Battlefield {
  const canvas = scene.getEngine().getRenderingCanvas();
  if (!canvas) throw new Error("the scene's engine has no canvas");
  scene.clearColor = Color4.FromColor3(Color3.FromHexString(SEA_COLOR), 1);
  const materials = new MaterialCache(scene);
  const bounds = boardBounds([]);
  const sea = CreateGround("sea", { width: 400, height: 400 }, scene);
  sea.material = materials.flat(SEA_COLOR);
  sea.isPickable = false;
  const sky = new HemisphericLight("sky", new Vector3(0.2, 1, -0.3), scene);
  sky.intensity = 0.8;
  sky.groundColor = new Color3(0.6, 0.62, 0.66);
  const sun = new DirectionalLight("sun", new Vector3(-0.4, -1, 0.5), scene);
  sun.intensity = 0.45;
  const camera = createBoardCamera(scene, canvas, bounds);
  const battlefield: Battlefield = {
    scene,
    camera,
    materials,
    labels: new LabelLayer(scene),
    bounds,
    generation: null,
    teams: [],
    castles: new Map(),
    islands: new Map(),
    bridges: new Map(),
    groups: new Map(),
    clashes: new Map(),
    headings: new Map(),
    headingSegments: [],
  };
  battlefields.set(scene, battlefield);
  return battlefield;
}

/** Disposes the entries of `entities` whose IDs are not in `keep`. */
function prune<Key, Entity>(
  entities: Map<Key, Entity>,
  keep: ReadonlySet<Key>,
  dispose: (entity: Entity) => void,
): void {
  for (const [key, entity] of entities) {
    if (keep.has(key)) continue;
    dispose(entity);
    entities.delete(key);
  }
}

function setLabel(
  field: Battlefield,
  id: string,
  spec: Partial<LabelSpec> & Pick<LabelSpec, "lines" | "anchor">,
): void {
  field.labels.set(id, {
    accent: "#52667b",
    fill: "rgba(255,255,255,0.92)",
    textColor: "#202d40",
    fontSize: 13,
    bold: false,
    priority: 10,
    required: false,
    ...spec,
  });
}

/** A node at a height above `parent`, which a label links to. It is found by name and reused. */
function anchorFor(
  scene: Scene,
  name: string,
  parent: TransformNode,
  height: number,
  depth = 0,
): TransformNode {
  let anchor = parent
    .getChildren((node) => node.name === name, true)
    .find((node): node is TransformNode => node instanceof TransformNode);
  if (!anchor) {
    anchor = new TransformNode(name, scene);
    anchor.parent = parent;
  }
  anchor.position.set(0, height, depth);
  return anchor;
}

function updateCastle(
  field: Battlefield,
  castle: Castle,
  entity: CastleEntity,
): void {
  const { scene, materials } = field;
  const world = toWorld(castle);
  entity.parts.root.position.set(world.x, 0, world.z);
  const color = teamColorHex(castle.team_color);
  entity.parts.roof.material = materials.lit(color);
  entity.parts.flag.material = materials.lit(color);
  entity.team = castle.team_color;

  const rings = Math.min(MAX_RINGS, upgradeRingCount(castle.upgrades));
  while (entity.rings.length < rings) {
    const ring = createUpgradeRing(
      scene,
      castle.id,
      entity.rings.length,
      materials,
    );
    ring.parent = entity.parts.root;
    entity.rings.push(ring);
  }
  while (entity.rings.length > rings) entity.rings.pop()?.dispose();

  const root = entity.parts.root;
  setLabel(field, `castle:${castle.id}:soldiers`, {
    lines: [String(castle.soldiers)],
    anchor: anchorFor(scene, `castle:${castle.id}:soldiers`, root, 1.45),
    accent: color,
    textColor: color,
    fontSize: 15,
    bold: true,
    priority: 100,
    required: true,
  });
  // The levels show once a castle has an upgrade; every castle starts at level 1 of each.
  const upgrades =
    upgradeRingCount(castle.upgrades) > 0 ? upgradeLevels(castle.upgrades) : "";
  setLabel(field, `castle:${castle.id}:info`, {
    lines: upgrades ? [castleTitle(castle), upgrades] : [castleTitle(castle)],
    fontSize: 11,
    anchor: anchorFor(scene, `castle:${castle.id}:info`, root, 0.05, -0.8),
    priority: 40,
    maxShift: 60,
  });
}

function updateGroup(
  field: Battlefield,
  group: SoldierGroup,
  destination: Castle | undefined,
  entity: GroupEntity,
): void {
  const { scene, materials } = field;
  const { root, body, hull } = entity.parts;
  const world = toWorld(group);
  root.position.set(world.x, 0, world.z);
  if (destination) {
    const target = toWorld(destination);
    root.rotation.y = Math.atan2(target.x - world.x, target.z - world.z);
  }
  // Boats grow gently with their soldiers: ~1.05 for 1, ~1.2 for 10, ~1.6 for 100.
  body.scaling.setAll(
    Math.min(1.7, 1 + 0.18 * Math.log2(1 + group.soldiers / 10)),
  );
  const color = teamColorHex(group.team_color);
  for (const mesh of hull) mesh.material = materials.lit(color);

  setLabel(field, `group:${group.id}:soldiers`, {
    lines: [String(group.soldiers)],
    anchor: anchorFor(scene, `group:${group.id}:soldiers`, root, 0.62),
    accent: color,
    fill: color,
    textColor: "#ffffff",
    fontSize: 13,
    bold: true,
    priority: 80,
    required: true,
  });
  if (upgradeRingCount(group.upgrades) > 0) {
    setLabel(field, `group:${group.id}:upgrades`, {
      lines: [upgradeLevels(group.upgrades)],
      anchor: anchorFor(scene, `group:${group.id}:upgrades`, root, 0.05, -0.4),
      fontSize: 11,
      priority: 20,
      maxShift: 40,
    });
  } else {
    field.labels.remove(`group:${group.id}:upgrades`);
  }
}

function updateClashes(field: Battlefield, generation: Generation): void {
  const { scene, materials } = field;
  const seen = new Set<string>();
  const losses = new Map<number, number>();
  const groupIds = new Set(generation.soldier_groups.map((group) => group.id));
  for (const clash of generation.clashes) {
    for (const participant of clash.participants) {
      const key = `${clash.id}:${participant.group_id}`;
      seen.add(key);
      const defeated = participant.soldiers_after === 0;
      const color = teamColorHex(participant.team_color);
      let entity = field.clashes.get(key);
      if (!entity) {
        entity = {
          ring: createFlatRing(
            scene,
            `clash-${key}`,
            participant.clash_radius,
            defeated ? 0.025 : 0.04,
          ),
          ghost: null,
        };
        field.clashes.set(key, entity);
      }
      const world = toWorld(participant);
      entity.ring.position.set(world.x, 0.07, world.z);
      entity.ring.material = defeated
        ? materials.flat(tint(color, 0.35), 0.55)
        : materials.flat(CLASH_COLOR);
      if (defeated) {
        // The group is gone: its boat sinks, drawn faded where it was lost.
        if (!entity.ghost) {
          entity.ghost = createBoat(
            scene,
            100000 + clash.id * 1000 + participant.group_id,
            materials,
          );
          for (const mesh of entity.ghost.meshes) {
            mesh.material = materials.flat(tint(color, 0.2), 0.4);
          }
          entity.ghost.body.position.y = -0.02;
          entity.ghost.root.rotation.z = 0.35;
        }
        entity.ghost.root.position.set(world.x, 0, world.z);
        const ghostRoot = entity.ghost.root;
        setLabel(field, `clash:${key}:lost`, {
          lines: [`× ${participant.soldiers_before}`],
          anchor: anchorFor(scene, `clash:${key}:lost`, ghostRoot, 0.4),
          accent: color,
          textColor: color,
          fontSize: 12,
          bold: true,
          priority: 30,
        });
      } else if (groupIds.has(participant.group_id)) {
        losses.set(
          participant.group_id,
          (losses.get(participant.group_id) ?? 0) +
            participant.soldiers_before -
            participant.soldiers_after,
        );
      }
    }
  }
  prune(field.clashes, seen, (entity) => {
    entity.ring.dispose();
    if (entity.ghost) {
      for (const mesh of entity.ghost.meshes) mesh.dispose();
      entity.ghost.body.dispose();
      entity.ghost.root.dispose();
    }
  });
  for (const id of field.labels.ids()) {
    if (
      id.startsWith("clash:") &&
      !seen.has(id.split(":").slice(1, 3).join(":"))
    ) {
      field.labels.remove(id);
    }
  }
  // A surviving group's losses, as in the replay's "−N" label above it.
  for (const group of generation.soldier_groups) {
    const lost = losses.get(group.id);
    const id = `group:${group.id}:loss`;
    const entity = field.groups.get(group.id);
    if (!lost || !entity) {
      field.labels.remove(id);
      continue;
    }
    setLabel(field, id, {
      lines: [`−${lost}`],
      anchor: anchorFor(scene, id, entity.parts.root, 0.95),
      accent: LOSS_COLOR,
      textColor: LOSS_COLOR,
      fontSize: 13,
      bold: true,
      priority: 60,
    });
  }
}

function setRibbon(mesh: Mesh, points: number[], indices: number[]): void {
  const positions: number[] = [];
  const normals: number[] = [];
  for (let index = 0; index < points.length; index += 2) {
    const world = toWorld({ x: points[index] ?? 0, y: points[index + 1] ?? 0 });
    positions.push(world.x, 0, world.z);
    normals.push(0, 1, 0);
  }
  const data = new VertexData();
  data.positions = positions;
  data.normals = normals;
  data.indices = indices;
  data.applyToMesh(mesh, true);
}

function updateHeadings(field: Battlefield, generation: Generation): void {
  const { scene, materials } = field;
  const segments = computeHeadings(
    generation.castles,
    generation.soldier_groups,
  );
  field.headingSegments = segments;
  const seen = new Set<number>();
  for (const segment of segments) {
    seen.add(segment.groupId);
    let entity = field.headings.get(segment.groupId);
    if (!entity) {
      const line = new MeshClass(`heading-${segment.groupId}`, scene);
      const casing = new MeshClass(`heading-${segment.groupId}-casing`, scene);
      line.isPickable = false;
      casing.isPickable = false;
      entity = { line, casing };
      field.headings.set(segment.groupId, entity);
    }
    const shaft = ribbonGeometry(segment, headingStyle);
    setRibbon(entity.line, shaft.points, shaft.indices);
    entity.line.material = materials.flat(
      tint(teamColorHex(segment.teamColor), headingStyle.tint),
    );
    // Stable stacking: a later group ID is higher, so it reads as passing over an earlier one.
    const height =
      ISLAND_TOP + headingStyle.height + segment.rank * headingStyle.stackStep;
    entity.line.position.y = height;
    entity.casing.setEnabled(headingStyle.casingWidth > 0);
    if (headingStyle.casingWidth > 0) {
      const casing = ribbonGeometry(
        segment,
        headingStyle,
        headingStyle.casingWidth,
      );
      setRibbon(entity.casing, casing.points, casing.indices);
      entity.casing.material = materials.flat(SEA_COLOR);
      entity.casing.position.y = height - headingStyle.stackStep * 0.5;
    }
  }
  prune(field.headings, seen, (entity) => {
    entity.line.dispose();
    entity.casing.dispose();
  });
}

function updateIslands(field: Battlefield, castles: readonly Castle[]): void {
  const { scene, materials } = field;
  for (const castle of castles) {
    if (field.islands.has(castle.id)) continue;
    const island = createIsland(scene, castle.id, materials);
    const world = toWorld(castle);
    island.position.set(world.x, 0, world.z);
    field.islands.set(castle.id, island);
  }
  // Neighbouring castles share a landmass: a land bridge joins their islands.
  const byId = new Map(castles.map((castle) => [castle.id, castle]));
  const wanted = new Set<string>();
  for (const [firstId, secondId] of landmassLinks(castles)) {
    const first = byId.get(firstId);
    const second = byId.get(secondId);
    if (!first || !second) continue;
    const key = `${firstId}-${secondId}`;
    wanted.add(key);
    if (field.bridges.has(key)) continue;
    const start = toWorld(first);
    const end = toWorld(second);
    const length = Math.hypot(end.x - start.x, end.z - start.z);
    const radius = 0.62;
    const bridge = createIslandBlob(
      scene,
      `bridge-${key}`,
      islandRadii(hashSeed(firstId, secondId), 12),
      radius,
      (length / 2 + 0.45) / radius,
    );
    bridge.material = materials.vertexColored();
    bridge.position.set((start.x + end.x) / 2, 0, (start.z + end.z) / 2);
    bridge.rotation.y = -Math.atan2(end.z - start.z, end.x - start.x);
    field.bridges.set(key, bridge);
  }
  prune(field.bridges, wanted, (bridge) => bridge.dispose());
}

/**
 * Draws a generation. Meshes are found by castle and group ID and reused from one call to the
 * next, so a later call only moves, recolours and relabels them. `teams` names the teams for
 * tooltips; colours come from the team numbers.
 */
export function renderGeneration(
  scene: Scene,
  generation: Generation,
  teams: readonly GameTeam[],
): Battlefield {
  const field = battlefields.get(scene) ?? createBattlefield(scene);
  field.generation = generation;
  field.teams = teams;

  const bounds = boardBounds(generation.castles);
  if (
    bounds.halfWidth !== field.bounds.halfWidth ||
    bounds.halfDepth !== field.bounds.halfDepth ||
    field.castles.size === 0
  ) {
    field.bounds = bounds;
    fitCamera(scene, field.camera, bounds);
  }

  updateIslands(field, generation.castles);
  const castleIds = new Set<number>();
  for (const castle of generation.castles) {
    castleIds.add(castle.id);
    let entity = field.castles.get(castle.id);
    if (!entity) {
      const parts = createCastle(scene, castle.id, field.materials);
      markPickable(parts.meshes, { kind: "castle", id: castle.id });
      entity = { parts, rings: [], team: castle.team_color };
      field.castles.set(castle.id, entity);
    }
    updateCastle(field, castle, entity);
  }
  prune(field.castles, castleIds, (entity) => {
    for (const ring of entity.rings) ring.dispose();
    for (const mesh of entity.parts.meshes) mesh.dispose();
    entity.parts.root.dispose();
  });

  const castleById = new Map(
    generation.castles.map((castle) => [castle.id, castle]),
  );
  const groupIds = new Set<number>();
  for (const group of generation.soldier_groups) {
    groupIds.add(group.id);
    let entity = field.groups.get(group.id);
    if (!entity) {
      const parts = createBoat(scene, group.id, field.materials);
      markPickable(parts.meshes, { kind: "group", id: group.id });
      entity = { parts };
      field.groups.set(group.id, entity);
    }
    updateGroup(
      field,
      group,
      castleById.get(group.destination_castle_id),
      entity,
    );
  }
  prune(field.groups, groupIds, (entity) => {
    for (const mesh of entity.parts.meshes) mesh.dispose();
    entity.parts.body.dispose();
    entity.parts.root.dispose();
  });
  for (const id of field.labels.ids()) {
    const match = /^group:(\d+):/.exec(id);
    if (match && !groupIds.has(Number(match[1]))) {
      field.labels.remove(id);
    }
  }

  updateClashes(field, generation);
  updateHeadings(field, generation);
  return field;
}
