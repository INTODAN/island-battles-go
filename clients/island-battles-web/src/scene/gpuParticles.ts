import "@babylonjs/core/Particles/webgl2ParticleSystem";
import { GPUParticleSystem } from "@babylonjs/core/Particles/gpuParticleSystem";
import type { Scene } from "@babylonjs/core/scene";

// GPU particles for the turn's effects (issue #102), in a chunk of their own that loads only
// where WebGL 2 can run them.

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
    { capacity: Math.max(8, capacity) },
    scene,
  );
}
