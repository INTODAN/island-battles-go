import { describe, expect, test } from "bun:test";
import { looksSlow } from "./graphics";

describe("the graphics setting (#102)", () => {
  test("a device with two cores or 2 GB of memory starts with effects off", () => {
    expect(looksSlow({ hardwareConcurrency: 2, deviceMemory: 8 })).toBe(true);
    expect(looksSlow({ hardwareConcurrency: 8, deviceMemory: 2 })).toBe(true);
    expect(looksSlow({ hardwareConcurrency: 4, deviceMemory: 4 })).toBe(false);
    // Browsers that tell nothing are assumed to cope.
    expect(looksSlow({})).toBe(false);
  });
});
