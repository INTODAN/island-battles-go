import "@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent";
import type { DirectionalLight } from "@babylonjs/core/Lights/directionalLight";
import { ShadowGenerator } from "@babylonjs/core/Lights/Shadows/shadowGenerator";
import type { Scene } from "@babylonjs/core/scene";

// The sun's soft shadows (issue #102), in a chunk of their own that loads only when the graphics
// setting turns them on.

export function createShadows(
  sun: DirectionalLight,
  scene: Scene,
  caster: RegExp,
): ShadowGenerator {
  const generator = new ShadowGenerator(1024, sun, true);
  // Percentage-closer filtering softens the edges (WebGL 2; Babylon falls back elsewhere).
  generator.usePercentageCloserFiltering = true;
  generator.filteringQuality = ShadowGenerator.QUALITY_MEDIUM;
  generator.bias = 0.004;
  generator.normalBias = 0.01;
  generator.darkness = 0.45;
  for (const mesh of scene.meshes) {
    if (caster.test(mesh.name)) generator.addShadowCaster(mesh, false);
  }
  return generator;
}
