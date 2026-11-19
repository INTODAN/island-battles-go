import { describe, expect, test } from "bun:test";
import {
  controlRows,
  controlsSeenKey,
  controlTable,
  markControlsSeen,
  pointerRows,
  shouldOpenControls,
} from "./controls";

class MemoryStorage {
  values = new Map<string, string>();
  getItem(key: string) {
    return this.values.get(key) ?? null;
  }
  setItem(key: string, value: string) {
    this.values.set(key, value);
  }
}

describe("the controls table", () => {
  test("every row has a way with a mouse or on touch", () => {
    for (const row of controlTable) {
      expect(row.mouse !== null || row.touch !== null).toBe(true);
    }
  });

  test("a mouse and keyboard get the keys; a touch screen the slider's icons and no keys", () => {
    const mouse = controlRows(false);
    const touch = controlRows(true);
    const how = (rows: typeof mouse, action: string) =>
      rows.find((row) => row.action === action)?.how;
    expect(how(mouse, "All")).toBe("D while dragging");
    expect(how(mouse, "Half")).toBe("A while dragging");
    expect(how(mouse, "More, fewer")).toContain("W and S");
    expect(how(mouse, "An exact number")).toContain("Type it");
    expect(how(mouse, "Castle menu: upgrades and burst")).toBe(
      "Right-click or double-click your castle",
    );
    expect(how(mouse, "Cancel")).toContain("Esc");
    expect(how(mouse, "Fit the board")).toContain("F,");
    expect(how(touch, "All")).toBe("Tap the all icon, right of the slider");
    expect(how(touch, "Half")).toBe("Tap the half icon, left of the slider");
    expect(how(touch, "More, fewer")).toBeUndefined();
    expect(how(touch, "An exact number")).toBeUndefined();
    expect(how(touch, "Castle menu: upgrades and burst")).toBe(
      "Double-tap your castle",
    );
    expect(mouse).toHaveLength(10);
    expect(touch).toHaveLength(8);
  });

  test("the primary pointer chooses the rows; a device with both may switch", () => {
    const device = (queries: string[]) => (query: string) =>
      queries.includes(query);
    expect(
      pointerRows(device(["(pointer: fine)", "(any-pointer: fine)"])),
    ).toEqual({
      touch: false,
      canSwitch: false,
    });
    expect(
      pointerRows(device(["(pointer: coarse)", "(any-pointer: coarse)"])),
    ).toEqual({ touch: true, canSwitch: false });
    // A laptop with a touch screen.
    expect(
      pointerRows(
        device([
          "(pointer: fine)",
          "(any-pointer: fine)",
          "(any-pointer: coarse)",
        ]),
      ),
    ).toEqual({ touch: false, canSwitch: true });
  });
});

describe("the first hand game", () => {
  test("opens the panel once, remembered in storage", () => {
    const storage = new MemoryStorage() as unknown as Storage;
    expect(shouldOpenControls(() => storage)).toBe(true);
    markControlsSeen(() => storage);
    expect(storage.getItem(controlsSeenKey)).toBe("1");
    expect(shouldOpenControls(() => storage)).toBe(false);
    expect(controlsSeenKey).toBe("island-battles-web.controls-seen");
  });

  test("with storage blocked or missing it doesn't open by itself, and nothing throws", () => {
    const blocked = () => {
      throw new DOMException("blocked", "SecurityError");
    };
    expect(shouldOpenControls(blocked)).toBe(false);
    expect(() => markControlsSeen(blocked)).not.toThrow();
    expect(shouldOpenControls(() => null)).toBe(false);
    const full = {
      getItem: () => null,
      setItem: () => {
        throw new DOMException("full", "QuotaExceededError");
      },
    } as unknown as Storage;
    expect(() => markControlsSeen(() => full)).not.toThrow();
  });
});
