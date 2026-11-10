import { describe, expect, test } from "bun:test";
import { SystemPool } from "./particlePool";

// A fake system: what the pool hands out, so no GPU is needed.
interface FakeSystem {
  id: number;
  resting: boolean;
}

function makePool(systems = 2) {
  let built = 0;
  const pool = new SystemPool<FakeSystem>(
    [
      { key: "smoke", systems },
      { key: "flash", systems: 1 },
    ],
    () => ({ id: built++, resting: true }),
    (system) => {
      system.resting = true;
    },
  );
  return { pool, built: () => built };
}

describe("SystemPool (#197)", () => {
  test("builds every system up front and never again", () => {
    const { pool, built } = makePool(3);
    expect(built()).toBe(4);
    expect(pool.systems).toHaveLength(4);
    for (let round = 0; round < 20; round++) {
      const lease = pool.take("smoke", round + 1);
      expect(lease).not.toBeNull();
      pool.update(round + 1);
    }
    expect(built()).toBe(4);
    expect(pool.systems).toHaveLength(4);
  });

  test("takes a free system of the kind and gets it back when its time comes", () => {
    const { pool } = makePool(1);
    const first = pool.take("smoke", 1);
    expect(first).not.toBeNull();
    if (first) first.system.resting = false;
    expect(pool.busy("smoke")).toBe(1);
    pool.update(0.99);
    expect(first?.active).toBe(true);
    expect(pool.busy("smoke")).toBe(1);
    pool.update(1);
    expect(first?.active).toBe(false);
    expect(first?.system.resting).toBe(true);
    expect(pool.busy("smoke")).toBe(0);
    // The same system serves the next use.
    expect(pool.take("smoke", 2)?.system).toBe(first?.system);
  });

  test("drops a use when the kind is full and lets the ones playing carry on", () => {
    const { pool, built } = makePool(2);
    const a = pool.take("smoke", 5);
    const b = pool.take("smoke", 6);
    expect(pool.take("smoke", 7)).toBeNull();
    expect(pool.take("smoke", 8)).toBeNull();
    expect(pool.dropped).toBe(2);
    expect(a?.active).toBe(true);
    expect(b?.active).toBe(true);
    expect(a?.until).toBe(5);
    expect(built()).toBe(3);
    // Another kind is not affected.
    expect(pool.take("flash", 1)).not.toBeNull();
  });

  test("a kind that does not exist is a drop, not a new system", () => {
    const { pool, built } = makePool();
    expect(pool.take("rain", 1)).toBeNull();
    expect(pool.dropped).toBe(1);
    expect(built()).toBe(3);
  });

  test("a stream is held until its time is set", () => {
    const { pool } = makePool(1);
    const stream = pool.take("smoke", Number.POSITIVE_INFINITY);
    pool.update(1e9);
    expect(stream?.active).toBe(true);
    if (stream) stream.until = 50;
    pool.update(49);
    expect(stream?.active).toBe(true);
    pool.update(50);
    expect(stream?.active).toBe(false);
  });

  test("releaseAll gives every system back, and counts the busiest moment", () => {
    const { pool } = makePool(3);
    pool.take("smoke", 10);
    pool.take("smoke", 10);
    pool.take("smoke", 10, false);
    pool.take("flash", 10);
    pool.releaseAll();
    expect(pool.busy("smoke")).toBe(0);
    expect(pool.busy("flash")).toBe(0);
    // The use that asked not to be counted (the warm-up) is left out.
    expect(pool.highWater.get("smoke")).toBe(2);
    expect(pool.highWater.get("flash")).toBe(1);
    expect(pool.take("smoke", 1)).not.toBeNull();
  });
});
