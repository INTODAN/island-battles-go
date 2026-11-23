import type { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import type { Scene } from "@babylonjs/core/scene";
import type { ScenePlayer } from "./animation";
import { battlefieldOf } from "./battlefield";
import { type Activity, cameraWatcher, paceScene } from "./framePacer";

/**
 * Paces a board's render loop (#403, `framePacer.ts`): full rate while a turn plays (or
 * `playing()` says one is about to), the camera moves, particles are alive or boats still turn,
 * and IDLE_FPS otherwise. The game view and the dev page use it.
 */
export function paceBoard(
  scene: Scene,
  { player, stage }: ScenePlayer,
  playing: () => boolean = () => false,
): ReturnType<typeof paceScene> {
  let watched: ArcRotateCamera | null = null;
  let moved: () => boolean = () => false;
  // One object, filled again after every frame.
  const activity: Activity = {};
  return paceScene(scene, () => {
    const field = battlefieldOf(scene);
    if (field && field.camera !== watched) {
      watched = field.camera;
      moved = cameraWatcher(field.camera);
    }
    activity.turn = player.busy || playing();
    activity.camera = moved();
    activity.particles = stage.effects.particles;
    activity.steering = (field?.steering.size ?? 0) > 0;
    return activity;
  });
}
