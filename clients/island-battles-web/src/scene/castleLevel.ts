// Which castle model a castle shows (issue #140). Pure, with no Babylon imports, so the turn
// planner and tests use it as the scene does. A later per-type rule changes this one place.

/** Castle models exist for levels 1 to 4, one per upgrade level of the Unity game. */
export const MAX_CASTLE_LEVEL = 4;

/**
 * The model level a castle shows: the highest level across its upgrade types, from 1 to 4. A
 * type missing from `upgrades` is at level 1 (backend/models/upgrades.go), and a type that gets
 * more levels later is clamped to the last model.
 */
export function castleDisplayLevel(upgrades: Record<string, number>): number {
  let highest = 1;
  for (const level of Object.values(upgrades)) {
    if (Number.isFinite(level)) highest = Math.max(highest, Math.trunc(level));
  }
  return Math.min(MAX_CASTLE_LEVEL, highest);
}
