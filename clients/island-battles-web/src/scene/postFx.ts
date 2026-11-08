import type { Camera } from "@babylonjs/core/Cameras/camera";
import { ImageProcessingConfiguration } from "@babylonjs/core/Materials/imageProcessingConfiguration";
import { DefaultRenderingPipeline } from "@babylonjs/core/PostProcesses/RenderPipeline/Pipelines/defaultRenderingPipeline";
import type { Scene } from "@babylonjs/core/scene";

// The board's post-processing (issue #102), loaded only when it is turned on: tone mapping that
// keeps colours' hue (Khronos PBR Neutral), a light bloom on the brightest highlights, and FXAA.

export interface PostFx {
  dispose(): void;
}

export function createPostFx(scene: Scene, camera: Camera): PostFx {
  const caps = scene.getEngine().getCaps();
  const hdr = caps.textureHalfFloatRender || caps.textureFloatRender;
  const pipeline = new DefaultRenderingPipeline("board-fx", hdr, scene, [
    camera,
  ]);
  pipeline.imageProcessingEnabled = true;
  const processing = pipeline.imageProcessing;
  processing.toneMappingEnabled = true;
  processing.toneMappingType =
    ImageProcessingConfiguration.TONEMAPPING_KHR_PBR_NEUTRAL;
  // Neither brighter nor punchier than without post-processing, so colours stay as measured.
  processing.exposure = 1;
  processing.contrast = 1;
  pipeline.bloomEnabled = true;
  pipeline.bloomThreshold = 0.82;
  pipeline.bloomWeight = 0.22;
  pipeline.bloomKernel = 40;
  pipeline.bloomScale = 0.5;
  pipeline.fxaaEnabled = true;
  return {
    dispose() {
      pipeline.dispose();
    },
  };
}
