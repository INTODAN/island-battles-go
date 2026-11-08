// The board's light (issue #102), as plain numbers: tools/environment/sky.ts reads the sun's
// direction to put its glow in the environment texture, and look.ts builds the lights.

/** The sun's light travels this way: from the upper left, a little behind the camera. */
export const SUN_DIRECTION: readonly [number, number, number] = [
  -0.45, -1, 0.42,
];
/** A warm afternoon sun. */
export const SUN_COLOR = "#fff0d8";
export const SUN_INTENSITY = 1.05;
/** The cool sky fill from above... */
export const SKY_COLOR = "#c4d8f2";
/** ...and the bounce from the sea below. */
export const GROUND_COLOR = "#4d6670";
export const FILL_INTENSITY = 0.55;
