import { VertexBuffer } from "@babylonjs/core/Buffers/buffer";
import type { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import type { Mesh } from "@babylonjs/core/Meshes/mesh";
import { Mesh as MeshClass } from "@babylonjs/core/Meshes/mesh";
import { VertexData } from "@babylonjs/core/Meshes/mesh.vertexData";
import { TransformNode } from "@babylonjs/core/Meshes/transformNode";
import type { Scene } from "@babylonjs/core/scene";
import type { components } from "../api/generated/game";
import {
  clashHeld,
  planTurnHeadings,
  restingHeadings,
  sampleTurnHeadings,
  type TurnHeadings,
} from "./animation/headings";
import {
  facingOf,
  stoppedByClashAt,
  type TurnPlan,
  type TurnSample,
} from "./animation/plan";
import { turnToward } from "./animation/rotation";
import {
  type BoatParts,
  createBoat,
  createBridge,
  createFlatRing,
  createIsland,
  createPropMeshes,
  createSquad,
  disposeBoat,
  disposeSquad,
  ISLAND_TOP,
  MaterialCache,
  markPickable,
  type PropMeshes,
  placeProps,
  SEA_COLOR,
  type SquadParts,
  setBoatTeam,
  setSquadSize,
  setSquadTeam,
  squadSize,
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
import { toWorld, toWorldDistance } from "./coordinates";
import {
  type HeadingSegment,
  headingDrawOrder,
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
import { type GroupLook, groupLook, insideWaterline } from "./shore";

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
  /** The heading the boat turns to, about y; unset until it is first placed. */
  heading?: number;
  /** The soldiers walking on land (#211), made the first time the group is on land. */
  squad?: SquadParts;
}

/** Disposes a group's boat and its squad. */
export function disposeGroup(entity: GroupEntity): void {
  disposeBoat(entity.parts);
  if (entity.squad) disposeSquad(entity.squad);
  delete entity.squad;
}

/** Shows or hides a group's boat and squad together. */
export function setGroupEnabled(entity: GroupEntity, enabled: boolean): void {
  entity.parts.root.setEnabled(enabled);
  if (!enabled) entity.squad?.root.setEnabled(false);
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
  /** The groups drawn as not moving, by the generation they were drawn for (case 14 of #168). */
  headingHeld: Map<number, ReadonlySet<number>>;
  /** The arrows of the turn playing, decided when it began. */
  turnHeadings: TurnHeadings | null;
  /** Boats still turning to their heading. */
  steering: Set<GroupEntity>;
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
    headingHeld: new Map(),
    turnHeadings: null,
    steering: new Set(),
  };
  // Boats turn to a new heading over a few frames rather than at once.
  scene.onBeforeRenderObservable.add(() =>
    steerBoats(
      battlefield,
      Math.min(MAX_STEER_FRAME_MS, scene.getEngine().getDeltaTime()),
    ),
  );
  // The heading lines are drawn after everything else, tested against its depth but not
  // against each other: rendering group 1 keeps the scene's depth buffer (#168 case 15).
  scene.setRenderingAutoClearDepthStencil(HEADING_GROUP, false, false, false);
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
  });
}

/** A frame turns boats by at most this much time, so a tab coming back does not snap them. */
const MAX_STEER_FRAME_MS = 100;

/**
 * Points a boat at `world` (where it is drawn) towards its destination. A boat placed for the
 * first time faces there at once; after that it turns there over about TURN_MS (`steerBoats`).
 */
export function aimGroup(
  field: Battlefield,
  entity: GroupEntity,
  world: { x: number; z: number },
  destination: Castle | undefined,
): void {
  if (!destination) return;
  const target = toWorld(destination);
  if (target.x === world.x && target.z === world.z) return;
  const heading = Math.atan2(target.x - world.x, target.z - world.z);
  const root = entity.parts.root;
  if (entity.heading === undefined) root.rotation.y = heading;
  entity.heading = heading;
  if (root.rotation.y === heading) field.steering.delete(entity);
  else field.steering.add(entity);
}

/** Turns every boat that is not yet on its heading a step towards it. */
export function steerBoats(field: Battlefield, deltaMs: number): void {
  for (const entity of field.steering) {
    const root = entity.parts.root;
    if (root.isDisposed() || entity.heading === undefined) {
      field.steering.delete(entity);
      continue;
    }
    root.rotation.y = turnToward(root.rotation.y, entity.heading, deltaMs);
    if (root.rotation.y === entity.heading) field.steering.delete(entity);
  }
}

/** Puts every boat on its heading at once, as a jump to a generation does. */
export function snapBoats(field: Battlefield): void {
  for (const entity of field.steering) {
    if (entity.heading !== undefined) {
      entity.parts.root.rotation.y = entity.heading;
    }
  }
  field.steering.clear();
}

/** Boats grow gently with their soldiers: ~1.05 for 1, ~1.2 for 10, ~1.6 for 100. */
export function boatScale(soldiers: number): number {
  return Math.min(1.7, 1 + 0.18 * Math.log2(1 + soldiers / 10));
}

/** How far a squad's walk bobs, in world units, and how often, per world unit walked. */
const SQUAD_BOB = 0.012;
const SQUAD_STEPS_PER_UNIT = 28;
/** Squads are drawn larger than life, so they read beside the boats and castles. */
const SQUAD_DRAWN_SCALE = 1.5;
/** Height of a squad's label above its feet. */
const SQUAD_LABEL_HEIGHT = 0.3;
/** Height of a boat's label above the sea. */
const BOAT_LABEL_HEIGHT = 0.62;

/** The node a group's count label links to: above its boat, or above its squad on land. */
function groupLabelAnchor(
  scene: Scene,
  groupId: number,
  entity: GroupEntity,
): TransformNode {
  const name = `group:${groupId}:soldiers`;
  return (
    entity.squad?.root
      .getChildren((node) => node.name === name, true)
      .find((node): node is TransformNode => node instanceof TransformNode) ??
    anchorFor(scene, name, entity.parts.root, BOAT_LABEL_HEIGHT)
  );
}

/** What a squad on land is doing besides walking (#213). */
export interface SquadAction {
  /** 0 to 1: it falls and fades, defeated on land. */
  fall?: number;
  /** The direction of the opponents it faces (map units), held by a clash. */
  facing?: { x: number; y: number } | null;
  /** 0 to 1 while it scuffles with them, from the clash or the start of a held turn. */
  scuffle?: number;
}

/** How far a scuffling squad lunges at its opponents, in world units, and how many times. */
const SCUFFLE_LUNGE = 0.035;
const SCUFFLE_LUNGES = 5;

/**
 * Poses a group's boat and squad from its look (shore.ts `groupLook`, or a turn's pose): how
 * much of the boat is built, its size (`boatSize`, which multiplies the size its soldiers give
 * it), and the squad walking on land. The boat's root is placed and turned by the caller.
 */
export function poseGroup(
  field: Battlefield,
  entity: GroupEntity,
  group: Pick<SoldierGroup, "id" | "team_color" | "soldiers">,
  look: Pick<GroupLook, "build" | "squad">,
  boatSize: number,
  action: SquadAction = {},
): void {
  const { scene, materials } = field;
  const parts = entity.parts;
  const shown = look.build > 0 && boatSize > 0;
  parts.body.setEnabled(shown);
  // A walked trip moves the hidden boat over the bridge: it leaves no wake.
  parts.wake.scaling.setAll(shown ? 1 : 0);
  parts.body.scaling.setAll(boatScale(group.soldiers) * boatSize);
  // A boat being built rises from its keel, then hoists its sail.
  const hull = Math.min(1, look.build / 0.6);
  const sail = Math.max(0, Math.min(1, (look.build - 0.6) / 0.4));
  for (const mesh of parts.hull) {
    mesh.scaling.set(0.55 + 0.45 * hull, Math.max(0.02, hull), 1);
  }
  for (const mesh of parts.sail) mesh.scaling.set(1, Math.max(0.02, sail), 1);

  const anchor = groupLabelAnchor(scene, group.id, entity);
  const squad = look.squad;
  if (!squad || squad.scale <= 0) {
    entity.squad?.root.setEnabled(false);
    if (anchor.parent !== parts.root) {
      anchor.parent = parts.root;
      anchor.position.set(0, BOAT_LABEL_HEIGHT, 0);
    }
    return;
  }
  const size = squadSize(group.soldiers);
  if (!entity.squad) {
    entity.squad = createSquad(
      scene,
      group.id,
      materials,
      group.team_color,
      size,
    );
    markPickable([entity.squad.base, entity.squad.team], {
      kind: "group",
      id: group.id,
    });
  }
  const squadParts = entity.squad;
  setSquadSize(squadParts, size);
  setSquadTeam(squadParts, group.team_color, materials);
  squadParts.root.setEnabled(parts.root.isEnabled(false));
  const world = toWorld(squad);
  const facing = action.facing ?? { x: squad.facingX, y: squad.facingY };
  const { body } = squadParts;
  squadParts.root.position.set(world.x, squad.height, world.z);
  squadParts.root.rotation.y = Math.atan2(facing.x, -facing.y);
  const fall = action.fall ?? 0;
  const scuffle = action.scuffle ?? 0;
  let drawnSize = squad.scale;
  if (fall > 0) {
    // Defeated on land: it topples over and fades where it fought.
    const toppled = Math.min(1, fall * 2.5);
    body.position.set(0, 0, 0);
    body.rotation.set(-(Math.PI / 2) * toppled * toppled, 0, 0);
    drawnSize *= 1 - Math.max(0, Math.min(1, (fall - 0.4) / 0.6));
  } else if (scuffle > 0 && scuffle < 1) {
    // Held by a clash: it lunges at its opponents and bobs, dying down as the scuffle ends.
    const wave = Math.abs(Math.sin(scuffle * Math.PI * SCUFFLE_LUNGES));
    const strength = 1 - scuffle;
    body.position.set(
      0,
      SQUAD_BOB * 1.4 * wave * strength,
      SCUFFLE_LUNGE * wave * strength,
    );
    body.rotation.set(0.25 * wave * strength, 0, 0);
  } else {
    const bob = Math.abs(Math.sin(squad.walked * SQUAD_STEPS_PER_UNIT));
    body.position.set(0, SQUAD_BOB * bob, 0);
    body.rotation.set(
      0,
      0,
      0.06 * Math.sin(squad.walked * SQUAD_STEPS_PER_UNIT * 0.5),
    );
  }
  body.scaling.setAll(SQUAD_DRAWN_SCALE * Math.max(0.001, drawnSize));
  if (anchor.parent !== squadParts.root) {
    anchor.parent = squadParts.root;
    anchor.position.set(0, SQUAD_LABEL_HEIGHT, 0);
  }
}

export function updateGroup(
  field: Battlefield,
  group: SoldierGroup,
  destination: Castle | undefined,
  origin: Castle | undefined,
  entity: GroupEntity,
  facing?: { x: number; y: number },
): void {
  const { scene, materials } = field;
  const { root } = entity.parts;
  // A boat near either island is drawn at its shore, not inside it, and its soldiers walk on
  // the island (shore.ts).
  const look = groupLook(group, origin, destination);
  const world = toWorld(look);
  root.position.set(world.x, 0, world.z);
  aimGroup(field, entity, world, destination);
  poseGroup(field, entity, group, look, look.boatScale, {
    facing: facing ?? null,
  });
  const color = teamColorHex(group.team_color);
  setBoatTeam(entity.parts, group.team_color, materials);

  setLabel(field, `group:${group.id}:soldiers`, {
    lines: [String(group.soldiers)],
    anchor: groupLabelAnchor(scene, group.id, entity),
    accent: color,
    fill: color,
    textColor: "#ffffff",
    fontSize: 13,
    bold: true,
    priority: 80,
    // Two boats close together show both counts, side by side (#212).
    rowGroup: "boats",
  });
}

function updateClashes(field: Battlefield, generation: Generation): void {
  const { scene, materials } = field;
  const seen = new Set<string>();
  const losses = new Map<number, number>();
  const groupIds = new Set(generation.soldier_groups.map((group) => group.id));
  const castlesById = new Map(generation.castles.map((c) => [c.id, c]));
  const groupsById = new Map(generation.soldier_groups.map((g) => [g.id, g]));
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
            toWorldDistance(participant.clash_radius),
            defeated ? 0.025 : 0.04,
          ),
          ghost: null,
        };
        field.clashes.set(key, entity);
      }
      // The ring of a group that is still there goes where it is drawn (shore.ts): at its squad
      // on land (#213), else at its boat. A defeated group's castles are not known: it was lost
      // on land when its contact is on an island.
      const survivor = groupsById.get(participant.group_id);
      const look = survivor
        ? groupLook(
            participant,
            castlesById.get(survivor.origin_castle_id),
            castlesById.get(survivor.destination_castle_id),
          )
        : null;
      const onLand = look
        ? look.squad !== null
        : generation.castles.some((castle) =>
            insideWaterline(castle, participant),
          );
      const world = toWorld(look ? (look.squad ?? look) : participant);
      entity.ring.position.set(
        world.x,
        onLand ? ISLAND_TOP + 0.02 : 0.07,
        world.z,
      );
      entity.ring.material = defeated
        ? materials.flat(tint(color, 0.35), 0.55)
        : materials.flat(CLASH_COLOR);
      if (defeated && onLand) {
        // Lost on land: no sunk boat on the grass, only its ring and what it lost (#213).
        if (entity.ghost) disposeBoat(entity.ghost);
        entity.ghost = null;
        setLabel(field, `clash:${key}:lost`, {
          lines: [`× ${participant.soldiers_before}`],
          anchor: anchorFor(scene, `clash:${key}:lost`, entity.ring, 0.33),
          accent: color,
          textColor: color,
          fontSize: 12,
          bold: true,
          priority: 30,
        });
      } else if (defeated) {
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
      // Above the squad while the group is on land (#213), else above the boat.
      anchor: entity.squad?.root.isEnabled(false)
        ? anchorFor(scene, id, entity.squad.root, 0.6)
        : anchorFor(scene, id, entity.parts.root, 0.95),
      accent: LOSS_COLOR,
      textColor: LOSS_COLOR,
      fontSize: 13,
      bold: true,
      priority: 60,
    });
  }
}

/**
 * Sets a heading ribbon's shape. The arrows move every frame of a turn, so when the ribbon keeps
 * its number of corners and its triangles, only the positions are written into the existing
 * buffer; a new buffer is made only when the shape changes.
 */
function setRibbon(mesh: Mesh, points: number[], indices: number[]): void {
  const positions = new Array<number>((points.length / 2) * 3);
  for (let index = 0; index < points.length; index += 2) {
    const world = toWorld({ x: points[index] ?? 0, y: points[index + 1] ?? 0 });
    const offset = (index / 2) * 3;
    positions[offset] = world.x;
    positions[offset + 1] = 0;
    positions[offset + 2] = world.z;
  }
  const shown = mesh.getIndices();
  if (
    positions.length > 0 &&
    mesh.getTotalVertices() === positions.length / 3 &&
    shown !== null &&
    shown.length === indices.length &&
    indices.every((value, index) => shown[index] === value)
  ) {
    mesh.updateVerticesData(VertexBuffer.PositionKind, positions, true);
    return;
  }
  const data = new VertexData();
  data.positions = positions;
  data.normals = positions.map((_, index) => (index % 3 === 1 ? 1 : 0));
  data.indices = indices;
  data.applyToMesh(mesh, true);
}

/** The rendering group of the heading lines; it keeps the depth of group 0. */
const HEADING_GROUP = 1;

/**
 * Draws the heading lines of a generation at rest. `held` are the groups that are not moving;
 * by default those a clash of this generation stopped. The turn animation passes the groups that
 * stayed put over the turn too.
 */
export function updateHeadings(
  field: Battlefield,
  generation: Generation,
  held?: ReadonlySet<number>,
): void {
  const known = field.headingHeld.get(generation.id);
  const shown = held ?? known ?? clashHeld(generation);
  if (held || !known) {
    field.headingHeld.delete(generation.id);
    field.headingHeld.set(generation.id, shown);
    // Only the last few generations are ever asked for.
    for (const id of field.headingHeld.keys()) {
      if (field.headingHeld.size <= 4) break;
      field.headingHeld.delete(id);
    }
  }
  field.turnHeadings = null;
  drawHeadings(field, restingHeadings(generation, shown, boatScale));
}

/** Decides the arrows of the turn `plan` plays (#168 case 13); call before `applyTurnHeadings`. */
export function beginTurnHeadings(field: Battlefield, plan: TurnPlan): void {
  field.turnHeadings = planTurnHeadings(
    plan,
    field.headingHeld.get(plan.previous.id) ?? clashHeld(plan.previous),
    boatScale,
  );
}

/** Draws the turn's arrows at `sample`, moving them with the boats. */
export function applyTurnHeadings(
  field: Battlefield,
  sample: TurnSample,
): void {
  if (!field.turnHeadings) return;
  drawHeadings(
    field,
    sampleTurnHeadings(field.turnHeadings, sample, boatScale),
  );
}

function drawHeadings(field: Battlefield, segments: HeadingSegment[]): void {
  const { scene, materials } = field;
  field.headingSegments = segments;
  const seen = new Set<number>();
  const height = ISLAND_TOP + headingStyle.height;
  for (const segment of segments) {
    seen.add(segment.groupId);
    let entity = field.headings.get(segment.groupId);
    if (!entity) {
      const line = new MeshClass(`heading-${segment.groupId}`, scene);
      const casing = new MeshClass(`heading-${segment.groupId}-casing`, scene);
      for (const mesh of [line, casing]) {
        mesh.isPickable = false;
        mesh.renderingGroupId = HEADING_GROUP;
        mesh.position.y = height;
      }
      entity = { line, casing };
      field.headings.set(segment.groupId, entity);
    }
    const order = headingDrawOrder(segment.rank);
    const shaft = ribbonGeometry(segment, headingStyle);
    setRibbon(entity.line, shaft.points, shaft.indices);
    entity.line.material = materials.overlay(
      tint(teamColorHex(segment.teamColor), headingStyle.tint),
    );
    entity.line.alphaIndex = order.line;
    entity.line.visibility = segment.opacity;
    entity.casing.setEnabled(headingStyle.casingWidth > 0);
    if (headingStyle.casingWidth > 0) {
      const casing = ribbonGeometry(
        segment,
        headingStyle,
        headingStyle.casingWidth,
      );
      setRibbon(entity.casing, casing.points, casing.indices);
      entity.casing.material = materials.overlay(SEA_COLOR);
      entity.casing.alphaIndex = order.casing;
      entity.casing.visibility = segment.opacity;
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

export interface RenderOptions {
  /**
   * Groups that are not in the generation but keep their boats and labels: a turn that starts
   * still shows the boats that land or sink during it.
   */
  keepGroups?: ReadonlySet<number>;
  /** False leaves the heading arrows to the caller (a turn draws its own). */
  headings?: boolean;
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
  options: RenderOptions = {},
): Battlefield {
  const keep = options.keepGroups ?? new Set<number>();
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
    // A group a clash stopped this turn faces its opponents (#213).
    updateGroup(
      field,
      group,
      castleById.get(group.destination_castle_id),
      castleById.get(group.origin_castle_id),
      entity,
      stoppedByClashAt(generation, group.id) !== undefined
        ? facingOf(generation, group.id).facing
        : undefined,
    );
  }
  for (const id of keep) groupIds.add(id);
  prune(field.groups, groupIds, disposeGroup);
  for (const id of field.labels.ids()) {
    const match = /^group:(\d+):/.exec(id);
    if (match && !groupIds.has(Number(match[1]))) {
      field.labels.remove(id);
    }
  }

  updateClashes(field, generation);
  if (options.headings !== false) updateHeadings(field, generation);
  return field;
}
