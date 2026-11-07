// The team palette, in the order of the lobby `order` (team_color 1..N). It matches
// backend/services/replay.html exactly (see palette.test.ts), so the 2D replay and the
// 3D board draw a team in the same colour. Teams past the sixth wrap around, as in the replay.
export const TEAM_COLORS = [
  "#3464e0",
  "#d94d5b",
  "#288562",
  "#9560cb",
  "#b37914",
  "#24828f",
] as const;

/** Neutral castles (team_color 0). */
export const NEUTRAL_COLOR = "#8290a4";

/** The clash ring colour of the replay's clash overlay. */
export const CLASH_COLOR = "#b37914";

/** The replay's dark loss text colour, used for the "-N" clash loss labels. */
export const LOSS_COLOR = "#824309";

export function teamColorHex(team: number): string {
  if (team < 1) return NEUTRAL_COLOR;
  return TEAM_COLORS[(team - 1) % TEAM_COLORS.length] ?? NEUTRAL_COLOR;
}

function channels(hex: string): [number, number, number] {
  const value = Number.parseInt(hex.slice(1), 16);
  return [(value >> 16) & 255, (value >> 8) & 255, value & 255];
}

function toHex(red: number, green: number, blue: number): string {
  const part = (channel: number) =>
    Math.round(Math.min(255, Math.max(0, channel)))
      .toString(16)
      .padStart(2, "0");
  return `#${part(red)}${part(green)}${part(blue)}`;
}

/** Mixes a colour toward another by `amount` (0 keeps `hex`, 1 gives `toward`). */
export function mix(hex: string, toward: string, amount: number): string {
  const from = channels(hex);
  const target = channels(toward);
  return toHex(
    from[0] + (target[0] - from[0]) * amount,
    from[1] + (target[1] - from[1]) * amount,
    from[2] + (target[2] - from[2]) * amount,
  );
}

/** A light tint of a colour: opaque, so overlapping lines never form a darker blot. */
export function tint(hex: string, amount: number): string {
  return mix(hex, "#ffffff", amount);
}
