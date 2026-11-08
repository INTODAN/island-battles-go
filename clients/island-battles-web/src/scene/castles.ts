import "@babylonjs/core/Meshes/instancedMesh";
// Only the glTF 2.0 loader: it registers the .glb plugin. No Draco or meshopt, whose decoders
// would load from a CDN; the GLBs are quantised with KHR_mesh_quantization instead, which is
// plain vertex data (tools/castles/convert.py).
import "@babylonjs/loaders/glTF/2.0/glTFLoader";
import "@babylonjs/loaders/glTF/2.0/Extensions/KHR_mesh_quantization";
import type { AnimationGroup } from "@babylonjs/core/Animations/animationGroup";
import type { AssetContainer } from "@babylonjs/core/assetContainer";
import { LoadAssetContainerAsync } from "@babylonjs/core/Loading/sceneLoader";
import type { Quaternion, Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { AbstractMesh } from "@babylonjs/core/Meshes/abstractMesh";
import type { InstancedMesh } from "@babylonjs/core/Meshes/instancedMesh";
import { Mesh } from "@babylonjs/core/Meshes/mesh";
import { TransformNode } from "@babylonjs/core/Meshes/transformNode";
import type { Observer } from "@babylonjs/core/Misc/observable";
import type { Scene } from "@babylonjs/core/scene";
import {
  type CastleParts,
  createPlaceholderCastle,
  ISLAND_TOP,
  type MaterialCache,
  type PickTarget,
} from "./art";
import { CASTLE_ATLAS_URL, CASTLE_MODEL_URLS } from "./castleAssets";
import {
  baseAtlasMaterial,
  hasAtlasPixels,
  loadAtlasPixels,
  setAtlasPixels,
  teamAtlas,
} from "./castleAtlas";
import { MAX_CASTLE_LEVEL } from "./castleLevel";
import { hashSeed, seededRandom } from "./islands";
import { teamColorHex } from "./palette";

// The castles of the Unity game, one model per display level (issue #140). The four GLBs from
// tools/castles/convert.py are loaded once per scene; until they are ready a castle shows the
// placeholder keep of art.ts, and swaps to its model when they arrive. Levels 1 and 2 are static
// and drawn as instances of one source mesh per level and team; levels 3 and 4 are skinned, so
// each castle gets its own copy with its own skeleton and its own loop of the `Take 001` clip.

/**
 * The turn about y that makes a castle's gate face the default camera (alpha -π/2, looking along
 * +z), the same for every castle, as Unity's prefab turned each castle to face its camera.
 */
export const CASTLE_YAW = Math.PI;
/** Room between the top of a castle and its soldier count. */
const LABEL_GAP = 0.14;
/** The soldier count's height above a placeholder keep. */
const PLACEHOLDER_LABEL_HEIGHT = 1.45;
/** The conversion script names each mesh after its material. */
const BASE_MESH = "castle-base";
const TEAM_MESH = "castle-team";
/** How long a level-up pop takes, in milliseconds. */
export const POP_MS = 400;

/** Where the models come from: URLs in the app, bytes in tests. */
export interface CastleSources {
  models: readonly (string | ArrayBufferView)[];
  atlas: () => Promise<Uint8Array | Uint8ClampedArray>;
}

const defaultSources = (): CastleSources => ({
  models: CASTLE_MODEL_URLS,
  atlas: () => loadAtlasPixels(CASTLE_ATLAS_URL),
});

interface LevelModel {
  level: number;
  container: AssetContainer;
  animated: boolean;
  /** The top of the model above its base, in world units. */
  height: number;
  /** Static levels: the castle-base meshes every castle of the level instances. */
  baseSources: Mesh[];
  /** Static levels: the castle-team meshes, as templates for each team's sources. */
  teamTemplates: Mesh[];
  /** Static levels: the castle-team sources of each team colour, made when first needed. */
  teamSources: Map<string, Mesh[]>;
}

/** The loaded models of one scene. */
export interface CastleModels {
  scene: Scene;
  levels: LevelModel[];
}

interface SceneModels {
  promise: Promise<CastleModels>;
  models: CastleModels | null;
}

const byScene = new WeakMap<Scene, SceneModels>();
const bytes = new Map<string, Promise<ArrayBuffer>>();

/** Fetches a model's bytes once for the page, however many scenes use it. */
function fetchOnce(url: string): Promise<ArrayBuffer> {
  let request = bytes.get(url);
  if (!request) {
    request = fetch(url).then((response) => {
      if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
      return response.arrayBuffer();
    });
    bytes.set(url, request);
  }
  return request;
}

async function loadLevel(
  scene: Scene,
  level: number,
  source: string | ArrayBufferView,
): Promise<LevelModel> {
  const data =
    typeof source === "string"
      ? new Uint8Array(await fetchOnce(source))
      : source;
  const container = await LoadAssetContainerAsync(data, scene, {
    pluginExtension: ".glb",
    name: `castle-level-${level}.glb`,
    pluginOptions: {
      gltf: {
        // The loops start per castle, with their own offsets.
        animationStartMode: 0,
        // The atlas materials of castleAtlas.ts take the place of the file's two materials, so
        // the loader never needs (or downloads) Babylon's PBR materials.
        skipMaterials: true,
      },
    },
  });
  // Every mesh is named after its material by the conversion script: `castle-base` for base
  // faces, `castle-team…` for team faces, flags and blades. A team mesh starts out neutral.
  const base = baseAtlasMaterial(scene);
  const neutral = teamAtlas(scene, 0);
  for (const mesh of container.meshes) {
    if (mesh.getTotalVertices() === 0) continue;
    if (mesh.name.includes(BASE_MESH)) mesh.material = base;
    else if (mesh.name.includes(TEAM_MESH)) mesh.material = neutral;
    else throw new Error(`castle level ${level}: unknown mesh ${mesh.name}`);
  }

  const animated = container.animationGroups.length > 0;
  const model: LevelModel = {
    level,
    container,
    animated,
    height: 0,
    baseSources: [],
    teamTemplates: [],
    teamSources: new Map(),
  };
  let top = 0;
  for (const mesh of container.meshes) {
    if (mesh.getTotalVertices() === 0) continue;
    mesh.computeWorldMatrix(true);
    mesh.refreshBoundingInfo({});
    top = Math.max(top, mesh.getBoundingInfo().boundingBox.maximumWorld.y);
  }
  model.height = top;
  if (!animated) {
    // One copy of the level in the scene, with its transforms baked into the vertices, as the
    // hidden source of every castle's instances.
    const entries = container.instantiateModelsToScene(
      (name) => `castle-level-${level}-source-${name}`,
      false,
      { doNotInstantiate: true },
    );
    for (const root of entries.rootNodes) {
      for (const mesh of root.getChildMeshes(false)) {
        if (!(mesh instanceof Mesh) || mesh.getTotalVertices() === 0) continue;
        const world = mesh.computeWorldMatrix(true).clone();
        mesh.parent = null;
        mesh.position.setAll(0);
        mesh.rotationQuaternion = null;
        mesh.rotation.setAll(0);
        mesh.scaling.setAll(1);
        mesh.makeGeometryUnique();
        // Flips the faces too when the transform mirrors (glTF's right-handed root does).
        mesh.bakeTransformIntoVertices(world);
        mesh.isPickable = false;
        mesh.isVisible = false;
        (mesh.material === base ? model.baseSources : model.teamTemplates).push(
          mesh,
        );
      }
      root.dispose();
    }
  }
  return model;
}

async function loadModels(
  scene: Scene,
  sources: CastleSources,
): Promise<CastleModels> {
  if (sources.models.length !== MAX_CASTLE_LEVEL) {
    throw new Error(`expected ${MAX_CASTLE_LEVEL} castle models`);
  }
  // The four models download while the atlas decodes.
  for (const source of sources.models) {
    if (typeof source === "string") fetchOnce(source).catch(() => {});
  }
  if (!hasAtlasPixels(scene)) setAtlasPixels(scene, await sources.atlas());
  const levels = await Promise.all(
    sources.models.map((source, index) => loadLevel(scene, index + 1, source)),
  );
  return { scene, levels };
}

function entryOf(scene: Scene, sources: CastleSources): SceneModels {
  let entry = byScene.get(scene);
  if (!entry) {
    const promise = loadModels(scene, sources);
    const created: SceneModels = { promise, models: null };
    promise.then(
      (models) => {
        created.models = models;
      },
      (error) => {
        // The placeholder castles stay, and the board is still usable.
        console.error("castle models failed to load", error);
      },
    );
    entry = created;
    byScene.set(scene, entry);
  }
  return entry;
}

/**
 * The scene's castle models, or null while they load. The first call starts loading the four
 * models in parallel; `onReady` is called once they are ready (not if they already are).
 */
export function requestCastleModels(
  scene: Scene,
  onReady?: (models: CastleModels) => void,
  sources: CastleSources = defaultSources(),
): CastleModels | null {
  const entry = entryOf(scene, sources);
  if (entry.models) return entry.models;
  if (onReady) {
    entry.promise.then(
      (models) => {
        if (!scene.isDisposed) onReady(models);
      },
      () => {},
    );
  }
  return null;
}

/** Loads the models into `scene` (once) and resolves when they are ready. */
export function loadCastleModels(
  scene: Scene,
  sources: CastleSources = defaultSources(),
): Promise<CastleModels> {
  return entryOf(scene, sources).promise;
}

function teamSourcesOf(
  models: CastleModels,
  model: LevelModel,
  team: number,
): Mesh[] {
  const hex = teamColorHex(team);
  let sources = model.teamSources.get(hex);
  if (!sources) {
    const material = teamAtlas(models.scene, team);
    sources = model.teamTemplates.map((template) => {
      const mesh = template.clone(`${template.name}-${hex.slice(1)}`);
      mesh.material = material;
      mesh.isVisible = false;
      mesh.isPickable = false;
      return mesh;
    });
    model.teamSources.set(hex, sources);
  }
  return sources;
}

// ---- animation -------------------------------------------------------------------------------

interface Loop {
  group: AnimationGroup;
  /** Where in the clip this castle starts, from 0 to 1, seeded by its ID. */
  phase: number;
  rest: {
    node: TransformNode;
    position: Vector3;
    rotation: Quaternion | null;
    scaling: Vector3;
  }[];
}

let animationEnabled = true;
const loops = new Set<Loop>();

function startLoop(loop: Loop): void {
  const { group } = loop;
  group.start(true, 1, group.from, group.to);
  group.goToFrame(group.from + loop.phase * (group.to - group.from));
}

function holdRest(loop: Loop): void {
  loop.group.stop(true);
  for (const { node, position, rotation, scaling } of loop.rest) {
    node.position.copyFrom(position);
    if (rotation) node.rotationQuaternion?.copyFrom(rotation);
    node.scaling.copyFrom(scaling);
  }
}

/**
 * Turns the level 3 and 4 loops on or off for every castle (#102's slow-device setting and the
 * dev page's `?still=1`). Off, castles hold their bind pose.
 */
export function setCastleAnimation(enabled: boolean): void {
  if (enabled === animationEnabled) return;
  animationEnabled = enabled;
  for (const loop of loops) {
    if (enabled) startLoop(loop);
    else holdRest(loop);
  }
}

export function castleAnimationEnabled(): boolean {
  return animationEnabled;
}

function createLoop(group: AnimationGroup, castleId: number): Loop {
  const rest: Loop["rest"] = [];
  const seen = new Set<TransformNode>();
  for (const { target } of group.targetedAnimations) {
    if (!(target instanceof TransformNode) || seen.has(target)) continue;
    seen.add(target);
    rest.push({
      node: target,
      position: target.position.clone(),
      rotation: target.rotationQuaternion?.clone() ?? null,
      scaling: target.scaling.clone(),
    });
  }
  return { group, phase: seededRandom(hashSeed(castleId, 140))(), rest };
}

// ---- one castle's model ----------------------------------------------------------------------

interface BuiltModel {
  level: number;
  team: number;
  node: TransformNode;
  /** Everything drawn, for picking. */
  meshes: AbstractMesh[];
  /** Static levels: the team instances, replaced when the team changes. */
  teamInstances: InstancedMesh[];
  /** Skinned levels: the team meshes, whose material changes with the team. */
  teamMeshes: AbstractMesh[];
  loop: Loop | null;
  dispose(): void;
}

function buildModel(
  models: CastleModels,
  level: number,
  team: number,
  castleId: number,
  parent: TransformNode,
  pick: PickTarget | null,
  animate: boolean,
): BuiltModel {
  const { scene } = models;
  const model = models.levels[level - 1];
  if (!model) throw new Error(`no castle model for level ${level}`);
  const node = new TransformNode(`castle-${castleId}-level-${level}`, scene);
  node.parent = parent;
  const built: BuiltModel = {
    level,
    team,
    node,
    meshes: [],
    teamInstances: [],
    teamMeshes: [],
    loop: null,
    dispose() {
      if (built.loop) {
        loops.delete(built.loop);
        built.loop.group.dispose();
      }
      node.dispose();
    },
  };
  if (!model.animated) {
    for (const source of model.baseSources) {
      const instance = source.createInstance(
        `castle-${castleId}-${source.name}`,
      );
      instance.parent = node;
      built.meshes.push(instance);
    }
    addTeamInstances(models, model, built, castleId);
  } else {
    const entries = model.container.instantiateModelsToScene(
      (name) => `castle-${castleId}-${name}`,
      false,
      { doNotInstantiate: true },
    );
    const base = baseAtlasMaterial(scene);
    const material = teamAtlas(scene, team);
    for (const root of entries.rootNodes) {
      root.parent = node;
      for (const mesh of root.getChildMeshes(false)) {
        if (mesh.getTotalVertices() === 0) continue;
        built.meshes.push(mesh);
        if (mesh.material !== base) {
          mesh.material = material;
          built.teamMeshes.push(mesh);
        }
      }
    }
    const group = entries.animationGroups[0];
    for (const extra of entries.animationGroups.slice(1)) extra.dispose();
    if (group && animate) {
      const loop = createLoop(group, castleId);
      built.loop = loop;
      loops.add(loop);
      if (animationEnabled) startLoop(loop);
    } else {
      group?.dispose();
    }
  }
  for (const mesh of built.meshes) {
    mesh.isPickable = pick !== null;
    mesh.metadata = pick;
  }
  return built;
}

function addTeamInstances(
  models: CastleModels,
  model: LevelModel,
  built: BuiltModel,
  castleId: number,
): void {
  for (const source of teamSourcesOf(models, model, built.team)) {
    const instance = source.createInstance(`castle-${castleId}-${source.name}`);
    instance.parent = built.node;
    instance.isPickable = built.meshes[0]?.isPickable ?? false;
    instance.metadata = built.meshes[0]?.metadata ?? null;
    built.teamInstances.push(instance);
    built.meshes.push(instance);
  }
}

/** Recolours a model for another team: only the team material (or team source) changes. */
function setModelTeam(
  models: CastleModels,
  built: BuiltModel,
  team: number,
  castleId: number,
): void {
  if (built.team === team) return;
  built.team = team;
  const model = models.levels[built.level - 1];
  if (!model) return;
  if (model.animated) {
    const material = teamAtlas(models.scene, team);
    for (const mesh of built.teamMeshes) mesh.material = material;
    return;
  }
  for (const instance of built.teamInstances) {
    built.meshes.splice(built.meshes.indexOf(instance), 1);
    instance.dispose();
  }
  built.teamInstances = [];
  addTeamInstances(models, model, built, castleId);
}

// ---- the castle on the board -----------------------------------------------------------------

/** A castle on the board: the placeholder keep until the models load, then its level's model. */
export interface CastleView {
  id: number;
  scene: Scene;
  /** Placed on the castle by the board. */
  root: TransformNode;
  /** Carries the model: on the island's top, turned to face the camera; a pop moves it. */
  holder: TransformNode;
  level: number;
  team: number;
  pick: PickTarget | null;
  placeholder: CastleParts | null;
  model: BuiltModel | null;
  pop: PopHandle | null;
}

/**
 * A castle at `level` (1 to 4) for `team`. `pick` marks its meshes for the board's picking, or
 * leaves them unpickable when null. `onModel` is called when the models arrive and the
 * placeholder is swapped, so the board can move labels to the model's height.
 */
export function createCastleView(
  scene: Scene,
  id: number,
  level: number,
  team: number,
  materials: MaterialCache,
  pick: PickTarget | null,
  onModel?: (view: CastleView) => void,
): CastleView {
  const root = new TransformNode(`castle-${id}`, scene);
  const holder = new TransformNode(`castle-${id}-holder`, scene);
  holder.parent = root;
  holder.position.y = ISLAND_TOP;
  holder.rotation.y = CASTLE_YAW;
  const view: CastleView = {
    id,
    scene,
    root,
    holder,
    level,
    team,
    pick,
    placeholder: null,
    model: null,
    pop: null,
  };
  const models = requestCastleModels(scene, (ready) => {
    if (root.isDisposed()) return;
    showModel(view, ready);
    onModel?.(view);
  });
  if (models) {
    showModel(view, models);
  } else {
    view.placeholder = createPlaceholderCastle(scene, id, materials);
    view.placeholder.root.parent = root;
    colourPlaceholder(view.placeholder, team, materials);
    for (const mesh of view.placeholder.meshes) {
      mesh.isPickable = pick !== null;
      mesh.metadata = pick;
    }
  }
  return view;
}

function colourPlaceholder(
  parts: CastleParts,
  team: number,
  materials: MaterialCache,
): void {
  const colour = materials.lit(teamColorHex(team));
  parts.roof.material = colour;
  parts.flag.material = colour;
}

function showModel(view: CastleView, models: CastleModels): void {
  if (view.placeholder) {
    for (const mesh of view.placeholder.meshes) mesh.dispose();
    view.placeholder.root.dispose();
    view.placeholder = null;
  }
  view.model?.dispose();
  view.model = buildModel(
    models,
    view.level,
    view.team,
    view.id,
    view.holder,
    view.pick,
    true,
  );
}

/**
 * Shows `level` for `team` at once (the seek behaviour). A new level swaps the model; a new team
 * changes only the team material, or the team's source mesh for instances.
 */
export function setCastleView(
  view: CastleView,
  level: number,
  team: number,
  materials: MaterialCache,
): void {
  const models = byScene.get(view.scene)?.models ?? null;
  const levelChanged = view.level !== level;
  view.level = level;
  view.team = team;
  if (!models) {
    if (view.placeholder) colourPlaceholder(view.placeholder, team, materials);
    return;
  }
  if (levelChanged || !view.model) {
    showModel(view, models);
  } else {
    setModelTeam(models, view.model, team, view.id);
  }
}

/** The height above the castle's root for its soldier count: just over its level's model. */
export function castleLabelHeight(scene: Scene, level: number): number {
  const model = byScene.get(scene)?.models?.levels[level - 1];
  if (!model) return PLACEHOLDER_LABEL_HEIGHT;
  return ISLAND_TOP + model.height + LABEL_GAP;
}

/** Every mesh the castle draws now. */
export function castleViewMeshes(view: CastleView): AbstractMesh[] {
  return view.model?.meshes ?? view.placeholder?.meshes ?? [];
}

export function disposeCastleView(view: CastleView): void {
  view.pop?.finish();
  view.model?.dispose();
  if (view.placeholder) {
    for (const mesh of view.placeholder.meshes) mesh.dispose();
  }
  view.root.dispose();
}

// ---- level-up pop ----------------------------------------------------------------------------

export interface PopHandle {
  /** Ends the pop at once, with the new model in place. */
  finish(): void;
}

const easeIn = (x: number) => x * x * x;
/** Rises past 1 a little and settles back: the overshoot of the new model. */
const easeOutBack = (x: number) => {
  const c = 1.2;
  return 1 + (c + 1) * (x - 1) ** 3 + c * (x - 1) ** 2;
};

/**
 * The level-up pop, about 0.4 s: the old model sinks into the island and the new one, already in
 * place (`setCastleView` swapped it), rises with a slight overshoot. Placeholder castles only
 * bounce. The returned handle ends it at once, as seeking does.
 */
export function popCastleView(
  view: CastleView,
  fromLevel: number,
  toLevel: number,
): PopHandle {
  view.pop?.finish();
  const { scene, holder } = view;
  const models = byScene.get(scene)?.models ?? null;
  let old: BuiltModel | null = null;
  let oldHolder: TransformNode | null = null;
  if (models && fromLevel !== toLevel && fromLevel >= 1) {
    oldHolder = new TransformNode(`castle-${view.id}-sinking`, scene);
    oldHolder.parent = view.root;
    oldHolder.position.y = ISLAND_TOP;
    oldHolder.rotation.y = CASTLE_YAW;
    old = buildModel(
      models,
      fromLevel,
      view.team,
      view.id,
      oldHolder,
      null,
      false,
    );
  }
  const depth = (level: number) =>
    ISLAND_TOP + (models?.levels[level - 1]?.height ?? 1.3) + 0.05;
  const sinkBy = depth(fromLevel);
  const riseFrom = depth(toLevel);
  // Time runs with the render loop, as the turn player does.
  let elapsed = 0;
  let observer: Observer<Scene> | null = null;

  const finish = () => {
    if (observer) scene.onBeforeRenderObservable.remove(observer);
    observer = null;
    old?.dispose();
    oldHolder?.dispose();
    old = null;
    oldHolder = null;
    holder.position.y = ISLAND_TOP;
    holder.scaling.setAll(1);
    if (view.pop === handle) view.pop = null;
  };
  const pose = () => {
    const s = elapsed / POP_MS;
    if (s >= 1) return finish();
    if (!models) {
      holder.scaling.setAll(1 + 0.25 * Math.sin(Math.PI * s));
      return;
    }
    if (oldHolder) {
      const sink = Math.min(1, s / 0.5);
      oldHolder.position.y = ISLAND_TOP - sinkBy * easeIn(sink);
      oldHolder.setEnabled(sink < 1);
    }
    const rise = Math.max(0, (s - 0.3) / 0.7);
    holder.position.y = ISLAND_TOP - riseFrom * (1 - easeOutBack(rise));
  };
  const handle: PopHandle = { finish };
  view.pop = handle;
  pose();
  observer = scene.onBeforeRenderObservable.add(() => {
    elapsed += scene.getEngine().getDeltaTime();
    pose();
  });
  return handle;
}
