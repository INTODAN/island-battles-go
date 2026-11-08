import { StandardMaterial } from "@babylonjs/core/Materials/standardMaterial";
import { Color3 } from "@babylonjs/core/Maths/math.color";
import { CreateDisc } from "@babylonjs/core/Meshes/Builders/discBuilder";
import type { Mesh } from "@babylonjs/core/Meshes/mesh";
import type { BoatParts } from "../art";
import { type Battlefield, popCastle } from "../battlefield";
import type { PopHandle } from "../castles";
import { toWorld } from "../coordinates";
import type { FlashPose } from "./plan";

/**
 * What a turn shows besides movement. The placeholders below are a flash and a sink tween; the
 * art pass (#102) replaces this object with smoke, splashes and bubbles, and adds its sparkle to
 * the castle models' level-up pop (#140, `popCastle`).
 */
export interface TurnEffects {
  /** Called every frame with the clash flashes alive at that moment; others are removed. */
  clash(field: Battlefield, flashes: readonly FlashPose[]): void;
  /** Poses a boat that is going under: `progress` 0 afloat to 1 gone. */
  sink(boat: BoatParts, progress: number): void;
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
  /** Called every frame with the wall-clock time, for effects that run on their own. */
  tick(nowMs: number): void;
  /** Removes everything and puts every posed thing back, whenever a turn ends or is cut short. */
  clear(): void;
}

const FLASH_COLOR = "#ffb326";

interface Flash {
  mesh: Mesh;
  material: StandardMaterial;
}

export function createPlaceholderEffects(): TurnEffects {
  const flashes = new Map<string, Flash>();
  const pops = new Map<number, PopHandle>();
  const sunk = new Set<BoatParts>();

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

    castleLevelUp(field, castleId, from, to) {
      pops.get(castleId)?.finish();
      const pop = popCastle(field.scene, castleId, from, to);
      if (pop) pops.set(castleId, pop);
    },

    tick() {},

    clear() {
      for (const entry of flashes.values()) {
        entry.mesh.dispose();
        entry.material.dispose();
      }
      flashes.clear();
      // Seeking never leaves a castle half-way out of the island.
      for (const pop of pops.values()) pop.finish();
      pops.clear();
      for (const boat of sunk) {
        boat.body.position.y = 0.1;
        boat.root.rotation.z = 0;
      }
      sunk.clear();
    },
  };
}
