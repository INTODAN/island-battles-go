import { Color3, Color4 } from "@babylonjs/core/Maths/math.color";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { Scene } from "@babylonjs/core/scene";
import { BOAT_FLOAT, type BoatParts, ISLAND_TOP } from "../art";
import { type Battlefield, popCastle } from "../battlefield";
import type { PopHandle } from "../castles";
import { toWorld, toWorldDistance } from "../coordinates";
import { graphicsOf } from "../graphics";
import { teamColorHex, tint } from "../palette";
import type { Bursts, Stream } from "../particles";
import type { FlashPose } from "./plan";

/**
 * What a turn shows besides movement (issue #102 replaced #100's placeholder flash and sink tween):
 * cannon smoke, splashes and a flash at each clash; a sinking boat that tilts, slides under and
 * leaves bubbles; a burst of the new team's colour when a castle is captured; and a sparkle on
 * the castle models' level-up pop (#140, `popCastle`).
 */
export interface TurnEffects {
  /** Called every frame with the clash flashes alive at that moment; others are removed. */
  clash(field: Battlefield, flashes: readonly FlashPose[]): void;
  /** Poses a boat that is going under: `progress` 0 afloat to 1 gone. */
  sink(boat: BoatParts, progress: number): void;
  /**
   * Rocks a boat that a clash holds, like one hit by a wave: `progress` 0 at the start of the
   * rattle to 1 at its end. Poses the boat's body only. Does nothing with Effects off.
   */
  rattle(boat: BoatParts, progress: number): void;
  /**
   * A castle's display level changed at the start of the turn. Until now the castle showed its
   * old model; call `popCastle` to swap it and play the pop.
   */
  castleLevelUp(
    field: Battlefield,
    castleId: number,
    from: number,
    to: number,
  ): void;
  /** A castle changed hands during the turn; `team` is its new team colour. */
  castleCaptured(field: Battlefield, castleId: number, team: number): void;
  /** Called every frame with the wall-clock time, for effects that run on their own. */
  tick(nowMs: number): void;
  /**
   * Puts every posed thing back, whenever a turn ends or is cut short. Short particle bursts
   * already under way finish on their own.
   */
  clear(): void;
}

const SMOKE = Color4.FromHexString("#ece8ded9");
const SMOKE_DARK = Color4.FromHexString("#b9b6aecc");
const FLASH = Color4.FromHexString("#ffd27aff");
const FLASH_HOT = Color4.FromHexString("#fff6dcff");
const SPRAY = Color4.FromHexString("#f4f1e6ff");
const GOLD = Color4.FromHexString("#ffe08aff");
const CLEAR = new Color4(1, 1, 1, 0);

function colour(hex: string, alpha = 1): Color4 {
  const value = Color3.FromHexString(hex);
  return new Color4(value.r, value.g, value.b, alpha);
}

const easeIn = (x: number) => x * x;

/** Peak roll of a rattle, in radians (about 8 degrees). */
export const RATTLE_ROLL = 0.14;
/** Peak sideways shake of a rattle, in world units. */
export const RATTLE_SHAKE = 0.025;

/**
 * The body pose of a rattle at `progress`: a roll that swings three times and a quicker, smaller
 * shake, both fading out quickly, so the boat is hit hard first and then settles.
 */
export function rattlePose(progress: number): { roll: number; shake: number } {
  const p = Math.min(1, Math.max(0, progress));
  const fade = (1 - p) * (1 - p);
  return {
    roll: RATTLE_ROLL * fade * Math.sin(p * Math.PI * 6),
    shake: RATTLE_SHAKE * fade * Math.sin(p * Math.PI * 11),
  };
}

type ParticlesModule = typeof import("../particles");
let particles: ParticlesModule | null = null;
let loadingParticles: Promise<ParticlesModule> | null = null;

/** The particle code loads on its own, once, as the board starts; effects wait for it. */
function loadParticles(): Promise<ParticlesModule> {
  loadingParticles ??= import("../particles").then((module) => {
    particles = module;
    return module;
  });
  return loadingParticles;
}

/**
 * The art pass's turn effects for one scene. Given the scene, it builds the particle pool and
 * compiles the particles' shaders at once rather than at the first clash (#197).
 */
export function createTurnEffects(scene?: Scene): TurnEffects {
  const pops = new Map<number, PopHandle>();
  /** Boats posed as sinking, with their bubbles (null once those stopped or before they load). */
  const sunk = new Map<BoatParts, Stream | null>();
  /** Boats whose bubbles have stopped: they get no more. */
  const finished = new Set<BoatParts>();
  /** Boats posed as rattled, to put back. */
  const rattled = new Set<BoatParts>();
  const seenFlashes = new Set<string>();
  let bursts: Bursts | null = null;
  let burstScene: Scene | null = null;

  /**
   * The scene's bursts, or null while the particle code loads and whenever Effects is off: with
   * Effects off no particle code is loaded, no pool is built and no burst plays. Asked again on
   * every effect and every frame (`tick`), so switching Effects on builds the pool at once.
   */
  const burstsFor = (scene: Scene): Bursts | null => {
    if (graphicsOf(scene) !== "high") return null;
    if (!particles) {
      void loadParticles();
      return null;
    }
    if (!bursts || burstScene !== scene) {
      bursts?.dispose();
      bursts = new particles.Bursts(scene);
      burstScene = scene;
      // Builds the pool now if the GPU or CPU choice is made; a no-op when called again.
      bursts.warmUp();
    }
    return bursts;
  };

  if (scene && graphicsOf(scene) === "high") {
    loadParticles().then(
      () => {
        if (!scene.isDisposed) burstsFor(scene)?.warmUp();
      },
      () => {},
    );
  }

  const castlePosition = (field: Battlefield, castleId: number) => {
    const castle = field.generation?.castles.find(
      (entry) => entry.id === castleId,
    );
    const entity = field.castles.get(castleId);
    if (entity) return entity.view.root.position.clone();
    if (castle) {
      const world = toWorld(castle);
      return new Vector3(world.x, 0, world.z);
    }
    return null;
  };

  return {
    clash(field, active) {
      const emitter = burstsFor(field.scene);
      if (!emitter) return;
      for (const flash of active) {
        const key = `${flash.clashId}:${flash.groupId}`;
        if (seenFlashes.has(key)) continue;
        seenFlashes.add(key);
        const world = toWorld(flash, 0.14);
        const at = new Vector3(world.x, world.y, world.z);
        const scale = Math.max(
          0.6,
          Math.min(1.6, toWorldDistance(flash.radius) / 0.3),
        );
        // The muzzle flash: a few big, short-lived glows.
        emitter.emit({
          name: `clash-flash-${key}`,
          position: at,
          count: 4,
          life: [0.12, 0.22],
          size: [0.35 * scale, 0.6 * scale],
          grow: 1.6,
          speed: [0, 0.2],
          direction1: new Vector3(-1, 0.5, -1),
          direction2: new Vector3(1, 1, 1),
          color1: FLASH_HOT,
          color2: FLASH,
          colorDead: new Color4(1, 0.6, 0.2, 0),
          shape: "soft",
          additive: true,
        });
        // Cannon smoke that rises and spreads.
        emitter.emit({
          name: `clash-smoke-${key}`,
          position: at,
          count: 14,
          duration: 0.18,
          life: [0.9, 1.5],
          size: [0.14 * scale, 0.24 * scale],
          grow: 2.1,
          speed: [0.15, 0.45],
          direction1: new Vector3(-0.8, 0.6, -0.8),
          direction2: new Vector3(0.8, 1.2, 0.8),
          spread: 0.08 * scale,
          gravity: new Vector3(0, 0.12, 0),
          color1: SMOKE,
          color2: SMOKE_DARK,
          colorDead: new Color4(0.85, 0.84, 0.8, 0),
          shape: "soft",
        });
        // Splashes of spray that fall back into the sea.
        emitter.emit({
          name: `clash-spray-${key}`,
          position: new Vector3(world.x, 0.02, world.z),
          count: 16,
          duration: 0.1,
          life: [0.4, 0.7],
          size: [0.035, 0.07],
          speed: [0.9, 1.5],
          direction1: new Vector3(-0.6, 1.4, -0.6),
          direction2: new Vector3(0.6, 2, 0.6),
          spread: 0.1 * scale,
          gravity: new Vector3(0, -5, 0),
          color1: SPRAY,
          color2: new Color4(0.85, 0.95, 0.95, 1),
          colorDead: CLEAR,
          shape: "soft",
        });
      }
    },

    sink(boat, progress) {
      const s = Math.min(1, Math.max(0, progress));
      // The bow lifts, the boat rolls, slides forward and goes under.
      boat.body.position.y = BOAT_FLOAT - 0.36 * easeIn(s);
      boat.body.position.z = 0.12 * s;
      boat.body.rotation.x = -0.55 * Math.min(1, s * 1.6);
      boat.root.rotation.z = 0.45 * s;
      let stream = sunk.get(boat) ?? null;
      if (!stream && s < 1 && !finished.has(boat)) {
        stream =
          burstsFor(boat.root.getScene())?.stream(
            {
              name: `bubbles-${boat.root.name}`,
              position: boat.root.position.clone(),
              count: 0,
              life: [0.45, 0.8],
              size: [0.025, 0.05],
              speed: [0.12, 0.3],
              direction1: new Vector3(-0.3, 1, -0.3),
              direction2: new Vector3(0.3, 1, 0.3),
              spread: 0.08,
              color1: new Color4(0.95, 1, 1, 0.95),
              color2: new Color4(0.8, 0.95, 0.95, 0.85),
              colorDead: CLEAR,
              shape: "soft",
            },
            36,
          ) ?? null;
      }
      sunk.set(boat, stream);
      stream?.move(boat.root.position);
      if (s >= 1 && stream) {
        stream.stop();
        sunk.set(boat, null);
        finished.add(boat);
      }
    },

    rattle(boat, progress) {
      if (graphicsOf(boat.root.getScene()) !== "high") return;
      const { roll, shake } = rattlePose(progress);
      boat.body.rotation.z = roll;
      boat.body.position.x = shake;
      rattled.add(boat);
    },

    castleLevelUp(field, castleId, from, to) {
      pops.get(castleId)?.finish();
      const pop = popCastle(field.scene, castleId, from, to);
      if (pop) pops.set(castleId, pop);
      const at = castlePosition(field, castleId);
      if (!at) return;
      // Sparkles that rise around the new castle as it pops up.
      burstsFor(field.scene)?.emit({
        name: `upgrade-${castleId}`,
        position: at.add(new Vector3(0, ISLAND_TOP + 0.35, 0)),
        count: 64,
        duration: 0.35,
        life: [0.6, 1.1],
        size: [0.1, 0.2],
        speed: [0.35, 0.9],
        direction1: new Vector3(-0.5, 1, -0.5),
        direction2: new Vector3(0.5, 1.6, 0.5),
        spread: 0.45,
        gravity: new Vector3(0, -0.4, 0),
        color1: GOLD,
        color2: FLASH_HOT,
        colorDead: new Color4(1, 0.85, 0.4, 0),
        shape: "spark",
        additive: true,
      });
    },

    castleCaptured(field, castleId, team) {
      const at = castlePosition(field, castleId);
      if (!at) return;
      const hex = teamColorHex(team);
      const emitter = burstsFor(field.scene);
      if (!emitter) return;
      // A burst of the new team's colour from the castle, and a few bright sparks with it.
      emitter.emit({
        name: `capture-${castleId}`,
        position: at.add(new Vector3(0, ISLAND_TOP + 0.45, 0)),
        count: 80,
        duration: 0.12,
        life: [0.45, 0.8],
        size: [0.12, 0.22],
        grow: 0.3,
        speed: [1.1, 2],
        direction1: new Vector3(-1, 0.3, -1),
        direction2: new Vector3(1, 1.1, 1),
        spread: 0.2,
        gravity: new Vector3(0, -1.2, 0),
        color1: colour(hex),
        color2: colour(tint(hex, 0.3)),
        colorDead: colour(tint(hex, 0.5), 0),
        shape: "soft",
      });
      emitter.emit({
        name: `capture-sparks-${castleId}`,
        position: at.add(new Vector3(0, ISLAND_TOP + 0.6, 0)),
        count: 18,
        duration: 0.1,
        life: [0.4, 0.7],
        size: [0.08, 0.14],
        speed: [1, 1.8],
        direction1: new Vector3(-1, 0.5, -1),
        direction2: new Vector3(1, 1.2, 1),
        spread: 0.15,
        color1: colour(tint(hex, 0.55)),
        color2: colour(hex),
        colorDead: colour(hex, 0),
        shape: "spark",
      });
    },

    tick() {
      // Effects switched on during a game: the pool is built here, before the first burst.
      if (scene && !scene.isDisposed) burstsFor(scene);
    },

    clear() {
      // Bursts of smoke, spray and sparkle play out on their own, across the turn's end; only
      // the bubbles under a sinking boat stop.
      for (const stream of sunk.values()) stream?.stop();
      seenFlashes.clear();
      // Seeking never leaves a castle half-way out of the island.
      for (const pop of pops.values()) pop.finish();
      pops.clear();
      for (const boat of sunk.keys()) {
        boat.body.position.y = BOAT_FLOAT;
        boat.body.position.z = 0;
        boat.body.rotation.x = 0;
        boat.root.rotation.z = 0;
      }
      sunk.clear();
      finished.clear();
      for (const boat of rattled) {
        boat.body.rotation.z = 0;
        boat.body.position.x = 0;
      }
      rattled.clear();
    },
  };
}
