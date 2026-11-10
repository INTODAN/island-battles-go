// How a boat turns to a new heading. Pure, so `bun test` runs it.

/** A boat turns to a new heading over about this long, in milliseconds. */
export const TURN_MS = 200;

/** Closer than this, in radians, a turn is done. */
const DONE = 1e-3;

/** The signed angle from `from` to `to` the shorter way round, in (-π, π]. */
export function shortestAngle(from: number, to: number): number {
  const full = Math.PI * 2;
  let difference = (to - from) % full;
  if (difference > Math.PI) difference -= full;
  if (difference <= -Math.PI) difference += full;
  return difference;
}

/**
 * A step of `deltaMs` from heading `current` towards `target`, the shorter way round. The turn
 * eases out: it starts at its fastest and is 95% done after TURN_MS, and it ends exactly on
 * `target`.
 */
export function turnToward(
  current: number,
  target: number,
  deltaMs: number,
): number {
  const left = shortestAngle(current, target);
  if (Math.abs(left) < DONE) return target;
  const step = left * (1 - Math.exp((-3 * Math.max(0, deltaMs)) / TURN_MS));
  const next = current + step;
  return Math.abs(shortestAngle(next, target)) < DONE ? target : next;
}
