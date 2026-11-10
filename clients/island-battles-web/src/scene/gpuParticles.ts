import "@babylonjs/core/Particles/webgl2ParticleSystem";
import { GPUParticleSystem } from "@babylonjs/core/Particles/gpuParticleSystem";
import type { Scene } from "@babylonjs/core/scene";

// GPU particles for the turn's effects (issue #102), in a chunk of their own that loads only
// where WebGL 2 can run them. `emitRateControl` makes a system emit what its rate says and
// recycle its slots as a CPU system does, so one that is started again does not refill itself
// with the particles of its last use (issue #197).

export function gpuParticlesSupported(): boolean {
  return GPUParticleSystem.IsSupported;
}

export function createGpuParticles(
  name: string,
  capacity: number,
  scene: Scene,
): GPUParticleSystem {
  return new GPUParticleSystem(
    name,
    { capacity: Math.max(8, capacity), emitRateControl: true },
    scene,
  );
}
