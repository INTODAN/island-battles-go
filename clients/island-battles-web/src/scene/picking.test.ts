import { describe, expect, test } from "bun:test";
import { touchTapTarget } from "./picking";

describe("touch tap tooltip", () => {
  const castle = { kind: "castle", id: 3 } as const;

  test("a tap on a castle or boat shows its tooltip", () => {
    expect(touchTapTarget("touch", castle)).toEqual(castle);
  });

  test("a tap elsewhere clears the tooltip", () => {
    expect(touchTapTarget("touch", null)).toBeNull();
  });

  test("a mouse click leaves the hover tooltip alone", () => {
    expect(touchTapTarget("mouse", castle)).toBeUndefined();
    expect(touchTapTarget("pen", null)).toBeUndefined();
  });
});
