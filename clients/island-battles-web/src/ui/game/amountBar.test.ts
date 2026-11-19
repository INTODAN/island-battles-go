import { describe, expect, test } from "bun:test";
import type { SliderView } from "../../play/drag";
import { announcement } from "./amountBar";

const share: SliderView = {
  mode: "share",
  min: 0,
  max: 100,
  value: 50,
  text: "Send 50%",
  disabled: false,
};
const soldiers = (text: string): SliderView => ({
  mode: "soldiers",
  min: 1,
  max: 23,
  value: 12,
  text,
  disabled: false,
});

describe("what the amount bar announces", () => {
  test("a drag starting says what it sends", () => {
    expect(announcement(share, soldiers("12 of 23"))).toBe("Sending 12 of 23");
    expect(announcement(null, soldiers("12 of 23"))).toBe("Sending 12 of 23");
  });

  test("a drag ending, by a send, a cancel or leaving live, says so", () => {
    expect(announcement(soldiers("12 of 23"), share)).toBe("Drag ended");
    expect(announcement(soldiers("12 of 23"), null)).toBe("Drag ended");
  });

  test("the steps of w, s and the slider during a drag are not announced", () => {
    expect(announcement(soldiers("12 of 23"), soldiers("13 of 23"))).toBeNull();
  });

  test("changing the share between drags is not announced", () => {
    expect(announcement(share, { ...share, text: "Send all" })).toBeNull();
    expect(announcement(null, share)).toBeNull();
  });
});
