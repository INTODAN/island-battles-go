import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { NEUTRAL_COLOR, TEAM_COLORS, teamColorHex, tint } from "./palette";

describe("palette", () => {
  test("matches the 2D replay's colours exactly", () => {
    const replay = readFileSync(
      new URL("../../../../backend/services/replay.html", import.meta.url),
      "utf8",
    );
    const declaration = /const colors = \[([^\]]+)\];/.exec(replay);
    expect(declaration).not.toBeNull();
    const replayColors = [
      ...(declaration?.[1] ?? "").matchAll(/"(#[0-9a-f]{6})"/g),
    ].map((match) => String(match[1]));
    expect([...TEAM_COLORS] as string[]).toEqual(replayColors);
    expect(replay).toContain(`|| "${NEUTRAL_COLOR}"`);
  });

  test("team numbers pick colours as the replay does", () => {
    expect(teamColorHex(1)).toBe("#3464e0");
    expect(teamColorHex(6)).toBe("#24828f");
    expect(teamColorHex(7)).toBe("#3464e0");
    expect(teamColorHex(0)).toBe(NEUTRAL_COLOR);
  });

  test("a tint is lighter, opaque and keeps the hue's order", () => {
    expect(tint("#000000", 0.5)).toBe("#808080");
    expect(tint("#3464e0", 0)).toBe("#3464e0");
    expect(tint("#3464e0", 1)).toBe("#ffffff");
  });
});
