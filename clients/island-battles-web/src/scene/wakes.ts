import { StandardMaterial } from "@babylonjs/core/Materials/standardMaterial";
import { Color3 } from "@babylonjs/core/Maths/math.color";
import { Mesh } from "@babylonjs/core/Meshes/mesh";
import { VertexData } from "@babylonjs/core/Meshes/mesh.vertexData";
import type { TransformNode } from "@babylonjs/core/Meshes/transformNode";
import type { Scene } from "@babylonjs/core/scene";

// Small wakes behind moving boats (issue #102): a foam V that trails each boat, faded in by how
// fast the boat moves, so a boat at rest leaves none. The wake is a child of the boat's root.

const WAKE_COLOR = "#f4f1e6";
/** World units per second at which a wake is fully shown. */
const FULL_SPEED = 0.5;
/** A boat that jumps further than this in one frame was placed, not sailed (seeking). */
const JUMP = 0.6;

interface Tracked {
  root: TransformNode;
  wake: Mesh;
  x: number;
  z: number;
  strength: number;
}

interface SceneWakes {
  material: StandardMaterial;
  geometry: VertexData;
  tracked: Set<Tracked>;
}

const byScene = new WeakMap<Scene, SceneWakes>();

/** The V: two arms from the stern, fading to nothing behind the boat. */
function wakeGeometry(): VertexData {
  const positions: number[] = [];
  const colors: number[] = [];
  const indices: number[] = [];
  const steps = 5;
  const length = 0.55;
  const spread = 0.2;
  for (const side of [1, -1]) {
    const start = positions.length / 3;
    for (let step = 0; step <= steps; step++) {
      const s = step / steps;
      const z = -0.16 - s * length;
      const x = side * (0.06 + s * spread);
      const width = 0.018 + s * 0.03;
      positions.push(x - width, 0, z, x + width, 0, z);
      const alpha = 0.9 * (1 - s) ** 1.4;
      colors.push(1, 1, 1, alpha, 1, 1, 1, alpha);
      if (step > 0) {
        const a = start + (step - 1) * 2;
        indices.push(a, a + 1, a + 3, a, a + 3, a + 2);
      }
    }
  }
  const data = new VertexData();
  data.positions = positions;
  data.colors = colors;
  data.indices = indices;
  data.normals = positions.map((_, index) => (index % 3 === 1 ? 1 : 0));
  return data;
}

function wakesOf(scene: Scene): SceneWakes {
  let entry = byScene.get(scene);
  if (entry) return entry;
  const material = new StandardMaterial("wake", scene);
  material.disableLighting = true;
  material.emissiveColor = Color3.FromHexString(WAKE_COLOR);
  material.backFaceCulling = false;
  const created: SceneWakes = {
    material,
    geometry: wakeGeometry(),
    tracked: new Set(),
  };
  entry = created;
  byScene.set(scene, created);
  scene.onBeforeRenderObservable.add(() => {
    const seconds = Math.max(0.001, scene.getEngine().getDeltaTime() / 1000);
    for (const item of created.tracked) {
      if (item.root.isDisposed()) {
        created.tracked.delete(item);
        continue;
      }
      const { x, z } = item.root.position;
      const moved = Math.hypot(x - item.x, z - item.z);
      item.x = x;
      item.z = z;
      const speed = moved > JUMP ? 0 : moved / seconds;
      const target = Math.min(1, speed / FULL_SPEED);
      // Ease toward the target, so a wake builds up and dies down over a few tenths of a second.
      const rate = Math.min(1, seconds * 6);
      item.strength += (target - item.strength) * rate;
      item.wake.visibility = item.strength;
      item.wake.isVisible = item.strength > 0.02;
    }
  });
  return created;
}

/** Gives the boat at `root` a wake, which shows while the boat moves. */
export function attachWake(
  scene: Scene,
  id: number,
  root: TransformNode,
): Mesh {
  const wakes = wakesOf(scene);
  const wake = new Mesh(`boat-${id}-wake`, scene);
  wakes.geometry.applyToMesh(wake);
  wake.hasVertexAlpha = true;
  wake.material = wakes.material;
  wake.parent = root;
  wake.position.y = 0.006;
  wake.isPickable = false;
  wake.isVisible = false;
  wake.visibility = 0;
  wakes.tracked.add({
    root,
    wake,
    x: root.position.x,
    z: root.position.z,
    strength: 0,
  });
  return wake;
}
