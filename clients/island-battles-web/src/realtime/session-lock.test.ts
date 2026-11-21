import { describe, expect, test } from "bun:test";
import { createSessionLock, sessionLockName } from "./session-lock";
import { FakeLocks } from "./testing";

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("session lock", () => {
  test("one tab at a time holds a player, until it lets go", async () => {
    const locks = new FakeLocks();
    const first = createSessionLock(locks);
    const second = createSessionLock(locks);
    expect(await first.claim("p1")).toBe(true);
    expect(locks.held.has(sessionLockName("p1"))).toBe(true);
    expect(await second.claim("p1")).toBe(false);
    first.release();
    await flush();
    expect(locks.held.size).toBe(0);
    expect(await second.claim("p1")).toBe(true);
  });

  test("claiming the held player again keeps the lock", async () => {
    const locks = new FakeLocks();
    const lock = createSessionLock(locks);
    expect(await lock.claim("p1")).toBe(true);
    expect(await lock.claim("p1")).toBe(true);
    expect([...locks.held]).toEqual([sessionLockName("p1")]);
  });

  test("claiming another player lets go of the first", async () => {
    const locks = new FakeLocks();
    const lock = createSessionLock(locks);
    await lock.claim("p1");
    expect(await lock.claim("p2")).toBe(true);
    await flush();
    expect([...locks.held]).toEqual([sessionLockName("p2")]);
  });

  test("a refused claim is asked again next time", async () => {
    const locks = new FakeLocks();
    const other = createSessionLock(locks);
    const lock = createSessionLock(locks);
    await other.claim("p1");
    expect(await lock.claim("p1")).toBe(false);
    other.release();
    await flush();
    expect(await lock.claim("p1")).toBe(true);
  });

  test("letting go before the lock is granted holds nothing", async () => {
    const locks = new FakeLocks(true);
    const lock = createSessionLock(locks);
    const claimed = lock.claim("p1");
    lock.release();
    expect(await claimed).toBe(false);
    await flush();
    expect(locks.held.size).toBe(0);
  });

  test("a browser without Web Locks always gets the claim", async () => {
    const lock = createSessionLock(undefined);
    expect(await lock.claim("p1")).toBe(true);
  });
});
