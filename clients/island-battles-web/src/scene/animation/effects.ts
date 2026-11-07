import { StandardMaterial } from "@babylonjs/core/Materials/standardMaterial";
import { Color3 } from "@babylonjs/core/Maths/math.color";
import { CreateDisc } from "@babylonjs/core/Meshes/Builders/discBuilder";
import type { Mesh } from "@babylonjs/core/Meshes/mesh";
import type { BoatParts } from "../art";
import type { Battlefield } from "../battlefield";
import { toWorld } from "../coordinates";
import type { FlashPose } from "./plan";

/**
 * What a turn shows besides movement. The placeholders below are a flash and a sink tween; the
 * art pass (#102) replaces this object with smoke, splashes and bubbles, and the castle models
 * (#140) replace `castleLevelUp` with their pop.
 */
export interface TurnEffects {
  /** Called every frame with the clash flashes alive at that moment; others are removed. */
  clash(field: Battlefield, flashes: readonly FlashPose[]): void;
  /** Poses a boat that is going under: `progress` 0 afloat to 1 gone. */
  sink(boat: BoatParts, progress: number): void;
  /** A castle's display level changed at the start of the turn. */
  castleLevelUp(
    field: Battlefield,
    castleId: number,
    from: number,
    to: number,
  ): void;
  /** Called every frame with the wall-clock time, for effects that run on their own. */
  tick(nowMs: number): void;
  /** Removes everything and puts every posed thing back, whenever a turn ends or is cut short. */
  clear(): void;
}

const FLASH_COLOR = "#ffb326";
const POP_MS = 400;

interface Flash {
  mesh: Mesh;
  material: StandardMaterial;
}

interface Pop {
  node: { scaling: { setAll(value: number): unknown } };
  start: number;
}

export function createPlaceholderEffects(): TurnEffects {
  const flashes = new Map<string, Flash>();
  const pops = new Map<number, Pop>();
  const sunk = new Set<BoatParts>();
  let now = 0;

  return {
    clash(field, active) {
      const alive = new Set<string>();
      for (const flash of active) {
        const key = `${flash.clashId}:${flash.groupId}`;
        alive.add(key);
        let entry = flashes.get(key);
        if (!entry) {
          const material = new StandardMaterial(`flash-${key}`, field.scene);
          material.disableLighting = true;
          material.emissiveColor = Color3.FromHexString(FLASH_COLOR);
          material.backFaceCulling = false;
          const mesh = CreateDisc(
            `flash-${key}`,
            { radius: 1, tessellation: 32 },
            field.scene,
          );
          mesh.rotation.x = Math.PI / 2;
          mesh.isPickable = false;
          mesh.material = material;
          entry = { mesh, material };
          flashes.set(key, entry);
        }
        const world = toWorld(flash, 0.3);
        entry.mesh.position.set(world.x, world.y, world.z);
        // A quick burst: it grows to about three clash radii while it fades.
        const grow = flash.radius * (0.6 + 2.4 * Math.sqrt(flash.age));
        entry.mesh.scaling.setAll(grow);
        entry.material.alpha = 0.95 * (1 - flash.age * flash.age);
      }
      for (const [key, entry] of flashes) {
        if (alive.has(key)) continue;
        entry.mesh.dispose();
        entry.material.dispose();
        flashes.delete(key);
      }
    },

    sink(boat, progress) {
      sunk.add(boat);
      boat.body.position.y = 0.1 - 0.3 * progress;
      boat.root.rotation.z = 0.7 * progress;
    },

    castleLevelUp(field, castleId) {
      const castle = field.castles.get(castleId);
      if (castle) pops.set(castleId, { node: castle.parts.root, start: now });
    },

    tick(nowMs) {
      now = nowMs;
      for (const [id, pop] of pops) {
        const s = (nowMs - pop.start) / POP_MS;
        if (s >= 1) {
          pop.node.scaling.setAll(1);
          pops.delete(id);
        } else {
          pop.node.scaling.setAll(1 + 0.25 * Math.sin(Math.PI * s));
        }
      }
    },

    clear() {
      for (const entry of flashes.values()) {
        entry.mesh.dispose();
        entry.material.dispose();
      }
      flashes.clear();
      for (const pop of pops.values()) pop.node.scaling.setAll(1);
      pops.clear();
      for (const boat of sunk) {
        boat.body.position.y = 0.1;
        boat.root.rotation.z = 0;
      }
      sunk.clear();
    },
  };
}
