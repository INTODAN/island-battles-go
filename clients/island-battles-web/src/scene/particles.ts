import "@babylonjs/core/Particles/particleSystemComponent";
import { RawTexture } from "@babylonjs/core/Materials/Textures/rawTexture";
import { Texture } from "@babylonjs/core/Materials/Textures/texture";
import { Color4 } from "@babylonjs/core/Maths/math.color";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { IParticleSystem } from "@babylonjs/core/Particles/IParticleSystem";
import { ParticleSystem } from "@babylonjs/core/Particles/particleSystem";
import type { Scene } from "@babylonjs/core/scene";

// Short particle bursts for the turn's effects (issue #102): smoke, splashes, flashes, bubbles
// and sparkles. Their two textures are drawn in code, so there is nothing to download. Where the
// browser supports them (WebGL 2), bursts run as GPU particles, loaded on first use; elsewhere
// they are CPU particles.

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

export class Bursts {
  private readonly textures = new Map<ParticleShape, Texture>();
  private readonly live = new Set<{ system: IParticleSystem; until: number }>();
  private elapsed = 0;
  private gpu: GpuModule | null = null;

  constructor(
    private readonly scene: Scene,
    /** False keeps every burst on the CPU. */
    useGpu = true,
  ) {
    const observer = scene.onBeforeRenderObservable.add(() => {
      this.elapsed += scene.getEngine().getDeltaTime() / 1000;
      for (const entry of this.live) {
        if (entry.until > this.elapsed) continue;
        // The textures are shared by every burst, so they stay.
        entry.system.dispose(false);
        this.live.delete(entry);
      }
    });
    scene.onDisposeObservable.addOnce(() => {
      scene.onBeforeRenderObservable.remove(observer);
    });
    if (useGpu && scene.getEngine().getCaps().supportTransformFeedbacks) {
      import("./gpuParticles").then(
        (module) => {
          if (!module.gpuParticlesSupported() || scene.isDisposed) return;
          this.gpu = module;
          this.warmUp();
        },
        // Bursts stay on the CPU.
        () => {},
      );
    }
  }

  /**
   * Emits one invisible particle of every kind, far below the sea, so their shaders compile
   * while the board loads rather than at the first clash.
   */
  warmUp(): void {
    for (const shape of ["soft", "spark"] as const) {
      for (const additive of [false, true]) {
        this.emit({
          name: `warm-up-${shape}-${additive}`,
          position: new Vector3(0, -50, 0),
          count: 1,
          life: [0.05, 0.05],
          size: [0.01, 0.01],
          grow: 0.5,
          speed: [0, 0],
          direction1: Vector3.Up(),
          direction2: Vector3.Up(),
          color1: new Color4(1, 1, 1, 0),
          color2: new Color4(1, 1, 1, 0),
          colorDead: new Color4(1, 1, 1, 0),
          shape,
          additive,
        });
      }
    }
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

  private create(options: BurstOptions, capacity: number): IParticleSystem {
    const system: IParticleSystem =
      this.gpu?.createGpuParticles(options.name, capacity, this.scene) ??
      new ParticleSystem(options.name, capacity, this.scene);
    system.particleTexture = this.texture(options.shape);
    system.emitter = options.position.clone();
    const spread = options.spread ?? 0;
    if (spread > 0) {
      system.createBoxEmitter(
        options.direction1,
        options.direction2,
        new Vector3(-spread, 0, -spread),
        new Vector3(spread, spread * 0.4, spread),
      );
    } else {
      system.createPointEmitter(options.direction1, options.direction2);
    }
    system.minLifeTime = options.life[0];
    system.maxLifeTime = options.life[1];
    system.minSize = options.size[0];
    system.maxSize = options.size[1];
    system.minEmitPower = options.speed[0];
    system.maxEmitPower = options.speed[1];
    system.minAngularSpeed = -1.5;
    system.maxAngularSpeed = 1.5;
    system.minInitialRotation = 0;
    system.maxInitialRotation = Math.PI * 2;
    system.gravity = options.gravity ?? Vector3.Zero();
    system.color1 = options.color1;
    system.color2 = options.color2;
    system.colorDead = options.colorDead;
    system.blendMode = options.additive
      ? ParticleSystem.BLENDMODE_ADD
      : ParticleSystem.BLENDMODE_STANDARD;
    if (options.grow && options.grow !== 1) {
      system.addSizeGradient(0, 1);
      system.addSizeGradient(1, options.grow);
    }
    system.updateSpeed = 1 / 60;
    return system;
  }

  /** Emits `count` particles over `duration` seconds, then cleans up after the last one dies. */
  emit(options: BurstOptions): void {
    const duration = options.duration ?? 0.06;
    const system = this.create(options, options.count);
    system.emitRate = options.count / duration;
    system.targetStopDuration = duration;
    system.start();
    this.live.add({
      system,
      until: this.elapsed + duration + options.life[1] + 0.2,
    });
  }

  /** Emits `rate` particles a second until stopped. */
  stream(options: BurstOptions, rate: number): Stream {
    const system = this.create(options, Math.ceil(rate * options.life[1]) + 4);
    system.emitRate = rate;
    system.start();
    const entry = { system, until: Number.POSITIVE_INFINITY };
    this.live.add(entry);
    return {
      move: (position) => {
        (system.emitter as Vector3).copyFrom(position);
      },
      stop: () => {
        if (entry.until !== Number.POSITIVE_INFINITY) return;
        system.stop();
        entry.until = this.elapsed + options.life[1] + 0.2;
      },
    };
  }

  /** Removes every burst at once. */
  clear(): void {
    for (const entry of this.live) entry.system.dispose(false);
    this.live.clear();
  }

  dispose(): void {
    this.clear();
    for (const texture of this.textures.values()) texture.dispose();
    this.textures.clear();
  }
}
