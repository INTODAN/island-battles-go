import "@babylonjs/core/Particles/particleSystemComponent";
import { RawTexture } from "@babylonjs/core/Materials/Textures/rawTexture";
import { Texture } from "@babylonjs/core/Materials/Textures/texture";
import { Color4 } from "@babylonjs/core/Maths/math.color";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { BoxParticleEmitter } from "@babylonjs/core/Particles/EmitterTypes/boxParticleEmitter";
import type { PointParticleEmitter } from "@babylonjs/core/Particles/EmitterTypes/pointParticleEmitter";
import type { IParticleSystem } from "@babylonjs/core/Particles/IParticleSystem";
import { ParticleSystem } from "@babylonjs/core/Particles/particleSystem";
import type { Scene } from "@babylonjs/core/scene";
import { type Lease, type PoolKind, SystemPool } from "./particlePool";

// Short particle bursts for the turn's effects (issue #102): smoke, splashes, flashes, bubbles
// and sparkles. Their two textures are drawn in code, so there is nothing to download. Where the
// browser supports them (WebGL 2), bursts run as GPU particles, loaded on first use; elsewhere
// they are CPU particles.
//
// Every system is built once, by `warmUp`, and reused (issue #197): a burst takes a free one of
// its kind, sets its position, colours, sizes and speeds (plain fields, which a GPU system sends
// to its shader on every frame, so nothing is compiled again), starts it, and gives it back once
// the last particle has died. What cannot change on a running system (the emitter's shape, the
// blend mode, the size curve and the capacity) is what makes a kind. When every system of a kind
// is busy the new burst is dropped and the ones playing carry on.

export type ParticleShape = "soft" | "spark";

export interface BurstOptions {
  name: string;
  position: Vector3;
  /** How many particles in all. */
  count: number;
  /** How long the burst emits, in seconds; 0.06 is a single puff. */
  duration?: number;
  life: [number, number];
  size: [number, number];
  /** How much a particle has grown by the end of its life (1 keeps its size). */
  grow?: number;
  speed: [number, number];
  direction1: Vector3;
  direction2: Vector3;
  /** The box particles start in, around `position`. */
  spread?: number;
  gravity?: Vector3;
  color1: Color4;
  color2: Color4;
  /** The colour a particle fades to; its alpha should be 0. */
  colorDead: Color4;
  shape: ParticleShape;
  /** Adds light (flashes, sparkles) rather than covering what is behind. */
  additive?: boolean;
}

/** A burst that keeps emitting until stopped (bubbles under a sinking boat). */
export interface Stream {
  move(position: Vector3): void;
  stop(): void;
}

/** A kind of system: what a running system cannot change, and how many exist. */
export interface BurstKind extends PoolKind {
  shape: ParticleShape;
  additive: boolean;
  /** A box emitter (the burst has a `spread`) rather than a point. */
  box: boolean;
  /** The size curve's end, as `BurstOptions.grow`. */
  grow: number;
  /** The most particles one use can have alive at once. */
  capacity: number;
}

function kindKeyOf(
  shape: ParticleShape,
  additive: boolean,
  box: boolean,
  grow: number,
): string {
  return `${shape}-${additive ? "add" : "std"}-${box ? "box" : "point"}-${grow}`;
}

/** The kind that serves `options`. */
export function kindKey(options: BurstOptions): string {
  const grow = options.grow && options.grow !== 1 ? options.grow : 1;
  return kindKeyOf(
    options.shape,
    options.additive ?? false,
    (options.spread ?? 0) > 0,
    grow,
  );
}

function kind(
  shape: ParticleShape,
  additive: boolean,
  box: boolean,
  grow: number,
  capacity: number,
  systems: number,
): BurstKind {
  return {
    key: kindKeyOf(shape, additive, box, grow),
    shape,
    additive,
    box,
    grow,
    capacity,
    systems,
  };
}

/**
 * Every kind of burst `animation/effects.ts` asks for, with its pool size (see the web client's
 * README for how the sizes were chosen): the clash's flash, smoke and spray, the bubbles under a
 * sinking boat (they share the spray's kind), the level-up sparkle, and the capture's burst and
 * sparks. A kind's capacity is its biggest burst.
 */
export const BURST_KINDS: readonly BurstKind[] = [
  kind("soft", true, false, 1.6, 4, 8),
  kind("soft", false, true, 2.1, 14, 10),
  kind("soft", false, true, 1, 36, 16),
  kind("spark", true, true, 1, 64, 6),
  kind("soft", false, true, 0.3, 80, 4),
  kind("spark", false, true, 1, 18, 4),
];

/** A pooled system with the parts a use changes. */
interface Slot {
  system: IParticleSystem;
  /** The emitter's position, which the system points at while it plays. */
  anchor: Vector3;
  box: BoxParticleEmitter | null;
  point: PointParticleEmitter | null;
}

type GpuModule = typeof import("./gpuParticles");

function shapePixels(shape: ParticleShape, size = 64): Uint8Array {
  const pixels = new Uint8Array(size * size * 4);
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const u = ((x + 0.5) / size) * 2 - 1;
      const v = ((y + 0.5) / size) * 2 - 1;
      const r2 = u * u + v * v;
      let alpha: number;
      if (shape === "soft") {
        alpha = Math.max(0, Math.exp(-r2 * 4.5) - Math.exp(-4.5));
      } else {
        const core = Math.exp(-r2 * 30);
        const rays =
          Math.exp(-Math.abs(u) * 3.5 - v * v * 260) +
          Math.exp(-Math.abs(v) * 3.5 - u * u * 260);
        alpha = Math.min(1, core + rays * 0.9) * Math.max(0, 1 - r2);
      }
      const offset = (y * size + x) * 4;
      pixels[offset] = 255;
      pixels[offset + 1] = 255;
      pixels[offset + 2] = 255;
      pixels[offset + 3] = Math.round(Math.min(1, alpha) * 255);
    }
  }
  return pixels;
}

/** Frames a system must have been ready for before it counts as warm. */
const WARM_UP_FRAMES = 3;
/** The longest a system is held for warming up, in seconds. */
const WARM_UP_LIMIT = 10;

const burstsByScene = new WeakMap<Scene, Bursts>();

/** The scene's bursts, if it has them yet (the dev page's counters read them). */
export function burstsOf(scene: Scene): Bursts | undefined {
  return burstsByScene.get(scene);
}

export class Bursts {
  private readonly textures = new Map<ParticleShape, Texture>();
  private pool: SystemPool<Slot> | null = null;
  private readonly kinds = new Map<string, BurstKind>();
  private elapsed = 0;
  private gpu: GpuModule | null = null;
  /** False until the choice between GPU and CPU particles is made. */
  private decided: boolean;
  /** Set by `warmUp` when it had to wait for the GPU code to load. */
  private wanted = false;
  private disposed = false;
  /** Systems still warming up, with the frames each has been ready for. */
  private warming: { lease: Lease<Slot>; ready: number }[] = [];

  constructor(
    private readonly scene: Scene,
    /** False keeps every burst on the CPU. */
    useGpu = true,
    kinds: readonly BurstKind[] = BURST_KINDS,
  ) {
    for (const entry of kinds) this.kinds.set(entry.key, entry);
    burstsByScene.set(scene, this);
    const observer = scene.onBeforeRenderObservable.add(() => {
      this.elapsed += scene.getEngine().getDeltaTime() / 1000;
      this.finishWarmUp();
      this.pool?.update(this.elapsed);
    });
    scene.onDisposeObservable.addOnce(() => {
      scene.onBeforeRenderObservable.remove(observer);
      // The scene disposes its particle systems and textures itself.
      this.pool = null;
      this.textures.clear();
      this.disposed = true;
    });
    this.decided = !(
      useGpu && scene.getEngine().getCaps().supportTransformFeedbacks
    );
    if (!this.decided) {
      const decide = (module: GpuModule | null) => {
        if (module?.gpuParticlesSupported() && !scene.isDisposed) {
          this.gpu = module;
        }
        this.decided = true;
        if (this.wanted) this.warmUp();
      };
      // Bursts stay on the CPU if the GPU code does not load.
      import("./gpuParticles").then(decide, () => decide(null));
    }
  }

  /**
   * Builds every system and plays one invisible particle on each, far below the sea, so that
   * their shaders compile and their buffers fill while the board loads rather than at the first
   * clash. It does nothing a second time, and waits until the GPU code has loaded or failed.
   */
  warmUp(): void {
    if (this.pool || this.disposed) return;
    if (!this.decided) {
      this.wanted = true;
      return;
    }
    this.pool = new SystemPool<Slot>(
      [...this.kinds.values()],
      (entry) => this.create(this.kinds.get(entry.key) as BurstKind),
      (slot) => this.rest(slot),
    );
    for (const slot of this.pool.systems) this.rest(slot);
    // One use on every system, so each is played once.
    for (const entry of this.kinds.values()) {
      for (let index = 0; index < entry.systems; index++) {
        const lease = this.play(
          entry.key,
          {
            name: "warm-up",
            position: new Vector3(0, -50, 0),
            count: 1,
            life: [0.05, 0.05],
            size: [0.01, 0.01],
            speed: [0, 0],
            direction1: Vector3.Up(),
            direction2: Vector3.Up(),
            color1: new Color4(1, 1, 1, 0),
            color2: new Color4(1, 1, 1, 0),
            colorDead: new Color4(1, 1, 1, 0),
            shape: entry.shape,
          },
          undefined,
          true,
        );
        // Held until the system has compiled and drawn a few frames, whatever that takes (but
        // not for ever): a system that is only used for the first time in a turn builds its
        // buffers and size curve then, in the middle of a frame.
        if (lease) {
          lease.until = this.elapsed + WARM_UP_LIMIT;
          this.warming.push({ lease, ready: 0 });
        }
      }
    }
  }

  /** Gives back each warming system once it has drawn a few frames ready. */
  private finishWarmUp(): void {
    if (this.warming.length === 0) return;
    this.warming = this.warming.filter((entry) => {
      if (!entry.lease.active) return false;
      if (entry.lease.system.system.isReady()) entry.ready++;
      if (entry.ready < WARM_UP_FRAMES) return true;
      entry.lease.until = this.elapsed;
      return false;
    });
  }

  /** How many uses were turned away, and the most systems busy at once for each kind. */
  stats(): {
    systems: number;
    dropped: number;
    highWater: Record<string, number>;
  } {
    return {
      systems: this.pool?.systems.length ?? 0,
      dropped: this.pool?.dropped ?? 0,
      highWater: Object.fromEntries(this.pool?.highWater ?? []),
    };
  }

  private texture(shape: ParticleShape): Texture {
    let texture = this.textures.get(shape);
    if (!texture) {
      const created = RawTexture.CreateRGBATexture(
        shapePixels(shape),
        64,
        64,
        this.scene,
        true,
        false,
        Texture.TRILINEAR_SAMPLINGMODE,
      );
      created.name = `particle-${shape}`;
      created.hasAlpha = true;
      texture = created;
      this.textures.set(shape, texture);
    }
    return texture;
  }

  /** Builds one system of `entry`'s kind: the parts that never change. */
  private create(entry: BurstKind): Slot {
    const name = `burst-${entry.key}`;
    const system: IParticleSystem =
      this.gpu?.createGpuParticles(name, entry.capacity, this.scene) ??
      new ParticleSystem(name, entry.capacity, this.scene);
    system.particleTexture = this.texture(entry.shape);
    const anchor = Vector3.Zero();
    system.emitter = anchor;
    let box: BoxParticleEmitter | null = null;
    let point: PointParticleEmitter | null = null;
    if (entry.box) {
      box = system.createBoxEmitter(
        Vector3.Up(),
        Vector3.Up(),
        Vector3.Zero(),
        Vector3.Zero(),
      ) as BoxParticleEmitter;
    } else {
      point = system.createPointEmitter(
        Vector3.Up(),
        Vector3.Up(),
      ) as PointParticleEmitter;
    }
    system.minAngularSpeed = -1.5;
    system.maxAngularSpeed = 1.5;
    system.minInitialRotation = 0;
    system.maxInitialRotation = Math.PI * 2;
    system.blendMode = entry.additive
      ? ParticleSystem.BLENDMODE_ADD
      : ParticleSystem.BLENDMODE_STANDARD;
    if (entry.grow !== 1) {
      system.addSizeGradient(0, 1);
      system.addSizeGradient(1, entry.grow);
    }
    system.updateSpeed = 1 / 60;
    return { system, anchor, box, point };
  }

  /** Puts a finished system to rest: stopped, and out of the scene's frame loop. */
  private rest(slot: Slot): void {
    slot.system.stop();
    slot.system.emitter = null;
  }

  /** Sets a pooled system up for `options` and starts it. */
  private configure(
    slot: Slot,
    options: BurstOptions,
    emitRate: number,
    stopAfter: number,
  ): void {
    const { system } = slot;
    slot.anchor.copyFrom(options.position);
    if (slot.box) {
      const spread = options.spread ?? 0;
      slot.box.direction1.copyFrom(options.direction1);
      slot.box.direction2.copyFrom(options.direction2);
      slot.box.minEmitBox.set(-spread, 0, -spread);
      slot.box.maxEmitBox.set(spread, spread * 0.4, spread);
    } else if (slot.point) {
      slot.point.direction1.copyFrom(options.direction1);
      slot.point.direction2.copyFrom(options.direction2);
    }
    system.minLifeTime = options.life[0];
    system.maxLifeTime = options.life[1];
    system.minSize = options.size[0];
    system.maxSize = options.size[1];
    system.minEmitPower = options.speed[0];
    system.maxEmitPower = options.speed[1];
    system.gravity = options.gravity ?? Vector3.Zero();
    system.color1 = options.color1;
    system.color2 = options.color2;
    system.colorDead = options.colorDead;
    system.emitRate = emitRate;
    system.targetStopDuration = stopAfter;
    system.emitter = slot.anchor;
    system.start();
  }

  /** Takes a free system of `key` and starts it, or returns null when none is free. */
  private play(
    key: string,
    options: BurstOptions,
    rate?: number,
    warm = false,
  ): Lease<Slot> | null {
    if (!this.pool) return null;
    const duration = options.duration ?? 0.06;
    const lease = this.pool.take(
      key,
      rate === undefined
        ? this.elapsed + duration + options.life[1] + 0.2
        : Number.POSITIVE_INFINITY,
      !warm,
    );
    const entry = this.kinds.get(key);
    if (!lease || !entry) return null;
    if (rate === undefined) {
      const count = Math.min(options.count, entry.capacity);
      this.configure(lease.system, options, count / duration, duration);
    } else {
      this.configure(lease.system, options, rate, 0);
    }
    return lease;
  }

  /**
   * Emits `count` particles over `duration` seconds on a pooled system, which goes back to the
   * pool after the last one dies. Dropped when none is free.
   */
  emit(options: BurstOptions): void {
    this.play(kindKey(options), options);
  }

  /** Emits `rate` particles a second until stopped; a stream that does nothing when none is free. */
  stream(options: BurstOptions, rate: number): Stream {
    const lease = this.play(kindKey(options), options, rate);
    return {
      move: (position) => {
        if (lease?.active) lease.system.anchor.copyFrom(position);
      },
      stop: () => {
        if (!lease?.active || lease.until !== Number.POSITIVE_INFINITY) return;
        lease.system.system.stop();
        lease.until = this.elapsed + options.life[1] + 0.2;
      },
    };
  }

  /** Stops every burst at once and gives the systems back. */
  clear(): void {
    this.warming = [];
    this.pool?.releaseAll();
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.pool?.releaseAll();
    // The textures are shared by every system, so they go last.
    for (const slot of this.pool?.systems ?? []) slot.system.dispose(false);
    this.pool = null;
    for (const texture of this.textures.values()) texture.dispose();
    this.textures.clear();
  }
}
