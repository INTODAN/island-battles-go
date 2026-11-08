import type { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import type { Mesh } from "@babylonjs/core/Meshes/mesh";
import { Mesh as MeshClass } from "@babylonjs/core/Meshes/mesh";
import { VertexData } from "@babylonjs/core/Meshes/mesh.vertexData";
import { TransformNode } from "@babylonjs/core/Meshes/transformNode";
import type { Scene } from "@babylonjs/core/scene";
import type { components } from "../api/generated/game";
import {
  type BoatParts,
  createBoat,
  createBridge,
  createFlatRing,
  createIsland,
  createPropMeshes,
  disposeBoat,
  ISLAND_TOP,
  MaterialCache,
  markPickable,
  type PropMeshes,
  placeProps,
  SEA_COLOR,
  setBoatTeam,
} from "./art";
import { type BoardBounds, boardBounds } from "./board";
import { createBoardCamera, fitCamera } from "./camera";
import { castleDisplayLevel } from "./castleLevel";
import {
  type CastleView,
  castleLabelHeight,
  createCastleView,
  disposeCastleView,
  type PopHandle,
  popCastleView,
  setCastleView,
} from "./castles";
import { toWorld } from "./coordinates";
import { castleTitle, hasUpgrades, upgradeLevels } from "./format";
import {
  computeHeadings,
  type HeadingSegment,
  headingStyle,
  ribbonGeometry,
} from "./headingLines";
import { hashSeed, landmassLinks } from "./islands";
import { LabelLayer, type LabelSpec } from "./labelLayer";
import {
  bridgeOutline,
  islandOutline,
  islandProps,
  type Outline,
  waterline,
} from "./landforms";
import { createBoardLook } from "./look";
import { CLASH_COLOR, LOSS_COLOR, teamColorHex, tint } from "./palette";

type Generation = components["schemas"]["Generation"];
type GameTeam = components["schemas"]["GameTeam"];
type Castle = components["schemas"]["Castle"];
type SoldierGroup = components["schemas"]["SoldierGroup"];

export interface CastleEntity {
  /** The castle's model for its display level, or the placeholder keep while models load. */
  view: CastleView;
  team: number;
}

export interface GroupEntity {
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
  /** The trees and rocks, as thin instances. */
  props: PropMeshes;
  /** The castles the islands, props and shallows were last built for. */
  islandKey: string;
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
  createBoardLook(scene);
  const materials = new MaterialCache(scene);
  const bounds = boardBounds([]);
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
    props: createPropMeshes(scene, materials),
    islandKey: "",
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

export function setLabel(
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
export function anchorFor(
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

/**
 * Places, recolours and relabels a castle. It shows the model of `level`, by default the display
 * level of its upgrades: a new level swaps the model at once (seeking), and a capture changes only
 * its team material. The turn animation passes the level of the moment, so a castle keeps its old
 * model until its level-up pop.
 */
export function updateCastle(
  field: Battlefield,
  castle: Castle,
  entity: CastleEntity,
  level = castleDisplayLevel(castle.upgrades),
): void {
  const { scene, materials } = field;
  const world = toWorld(castle);
  const { view } = entity;
  view.root.position.set(world.x, 0, world.z);
  const color = teamColorHex(castle.team_color);
  setCastleView(view, level, castle.team_color, materials);
  entity.team = castle.team_color;

  const root = view.root;
  setLabel(field, `castle:${castle.id}:soldiers`, {
    lines: [String(castle.soldiers)],
    anchor: anchorFor(
      scene,
      `castle:${castle.id}:soldiers`,
      root,
      castleLabelHeight(scene, level),
    ),
    accent: color,
    textColor: color,
    fontSize: 15,
    bold: true,
    priority: 100,
    required: true,
  });
  // The levels show once a castle has an upgrade; every castle starts at level 1 of each.
  const upgrades = hasUpgrades(castle.upgrades)
    ? upgradeLevels(castle.upgrades)
    : "";
  setLabel(field, `castle:${castle.id}:info`, {
    lines: upgrades ? [castleTitle(castle), upgrades] : [castleTitle(castle)],
    fontSize: 11,
    anchor: anchorFor(scene, `castle:${castle.id}:info`, root, 0.05, -0.8),
    priority: 40,
    maxShift: 60,
  });
}

/** Boats grow gently with their soldiers: ~1.05 for 1, ~1.2 for 10, ~1.6 for 100. */
export function boatScale(soldiers: number): number {
  return Math.min(1.7, 1 + 0.18 * Math.log2(1 + soldiers / 10));
}

export function updateGroup(
  field: Battlefield,
  group: SoldierGroup,
  destination: Castle | undefined,
  entity: GroupEntity,
): void {
  const { scene, materials } = field;
  const { root, body } = entity.parts;
  const world = toWorld(group);
  root.position.set(world.x, 0, world.z);
  if (destination) {
    const target = toWorld(destination);
    root.rotation.y = Math.atan2(target.x - world.x, target.z - world.z);
  }
  body.scaling.setAll(boatScale(group.soldiers));
  const color = teamColorHex(group.team_color);
  setBoatTeam(entity.parts, group.team_color, materials);

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
  if (hasUpgrades(group.upgrades)) {
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
          entity.ghost.wake.setEnabled(false);
          entity.ghost.body.position.y = -0.04;
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
    if (entity.ghost) disposeBoat(entity.ghost);
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

export function updateHeadings(
  field: Battlefield,
  generation: Generation,
): void {
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

/** A polygon in local x and z, turned by `yaw` about y and moved to (`x`, `z`). */
function placeOutline(
  outline: Outline,
  x: number,
  z: number,
  yaw = 0,
): Outline {
  const cos = Math.cos(yaw);
  const sin = Math.sin(yaw);
  return outline.map(
    ([lx, lz]) => [x + lx * cos + lz * sin, z - lx * sin + lz * cos] as const,
  );
}

function updateIslands(field: Battlefield, castles: readonly Castle[]): void {
  const { scene, materials } = field;
  const key = castles
    .map((castle) => `${castle.id}:${castle.x}:${castle.y}`)
    .sort()
    .join(",");
  if (key === field.islandKey) return;
  field.islandKey = key;
  const waterlines: Outline[] = [];
  const ids = new Set<number>();
  for (const castle of castles) {
    ids.add(castle.id);
    const world = toWorld(castle);
    let island = field.islands.get(castle.id);
    if (!island) {
      island = createIsland(scene, castle.id, materials);
      field.islands.set(castle.id, island);
    }
    island.position.set(world.x, 0, world.z);
    waterlines.push(
      placeOutline(waterline(islandOutline(castle.id)), world.x, world.z),
    );
  }
  prune(field.islands, ids, (island) => island.dispose());
  // Neighbouring castles share a landmass: a low land bridge joins their islands.
  const byId = new Map(castles.map((castle) => [castle.id, castle]));
  const wanted = new Set<string>();
  for (const [firstId, secondId] of landmassLinks(castles)) {
    const first = byId.get(firstId);
    const second = byId.get(secondId);
    if (!first || !second) continue;
    const bridgeKey = `${firstId}-${secondId}`;
    wanted.add(bridgeKey);
    const start = toWorld(first);
    const end = toWorld(second);
    const length = Math.hypot(end.x - start.x, end.z - start.z);
    const seed = hashSeed(firstId, secondId);
    const yaw = -Math.atan2(end.z - start.z, end.x - start.x);
    const middle = { x: (start.x + end.x) / 2, z: (start.z + end.z) / 2 };
    let bridge = field.bridges.get(bridgeKey);
    if (!bridge) {
      bridge = createBridge(scene, bridgeKey, length, seed, materials);
      field.bridges.set(bridgeKey, bridge);
    }
    bridge.position.set(middle.x, 0, middle.z);
    bridge.rotation.y = yaw;
    waterlines.push(
      placeOutline(
        waterline(bridgeOutline(length, seed)),
        middle.x,
        middle.z,
        yaw,
      ),
    );
  }
  prune(field.bridges, wanted, (bridge) => bridge.dispose());
  createBoardLook(scene).setIslands(waterlines);
  placeProps(field.props, islandProps(castles));
}

/**
 * The level-up pop of a castle (about 0.4 s): it swaps to the model of `toLevel`, then the old
 * model sinks into the island while the new one rises with a slight overshoot. The turn animation
 * calls it at the start of a turn; `finish()` on the handle ends it at once, as seeking does.
 */
export function popCastle(
  scene: Scene,
  id: number,
  fromLevel: number,
  toLevel: number,
): PopHandle | null {
  const field = battlefields.get(scene);
  const entity = field?.castles.get(id);
  if (!field || !entity) return null;
  const { view } = entity;
  setCastleView(view, toLevel, view.team, field.materials);
  anchorFor(
    scene,
    `castle:${id}:soldiers`,
    view.root,
    castleLabelHeight(scene, toLevel),
  );
  return popCastleView(view, fromLevel, toLevel);
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
      const view = createCastleView(
        scene,
        castle.id,
        castleDisplayLevel(castle.upgrades),
        castle.team_color,
        field.materials,
        { kind: "castle", id: castle.id },
        (ready) => {
          // The models arrived: lift the soldier count to the model's height.
          anchorFor(
            scene,
            `castle:${ready.id}:soldiers`,
            ready.root,
            castleLabelHeight(scene, ready.level),
          );
        },
      );
      entity = { view, team: castle.team_color };
      field.castles.set(castle.id, entity);
    }
    updateCastle(field, castle, entity);
  }
  prune(field.castles, castleIds, (entity) => disposeCastleView(entity.view));

  const castleById = new Map(
    generation.castles.map((castle) => [castle.id, castle]),
  );
  const groupIds = new Set<number>();
  for (const group of generation.soldier_groups) {
    groupIds.add(group.id);
    let entity = field.groups.get(group.id);
    if (!entity) {
      const parts = createBoat(
        scene,
        group.id,
        field.materials,
        group.team_color,
      );
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
  prune(field.groups, groupIds, (entity) => disposeBoat(entity.parts));
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
