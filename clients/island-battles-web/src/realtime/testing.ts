// Test helpers for the realtime session.

import type { LockManagerLike } from "./session-lock";

/** One browser's Web Locks, shared by the tabs of a test; `delayed` grants a microtask later. */
export class FakeLocks implements LockManagerLike {
  held = new Set<string>();
  constructor(private readonly delayed = false) {}
  async request(
    name: string,
    _options: { ifAvailable: boolean },
    callback: (lock: unknown) => Promise<void> | undefined,
  ): Promise<unknown> {
    if (this.delayed) await Promise.resolve();
    if (this.held.has(name)) return callback(null);
    this.held.add(name);
    try {
      return await callback({ name });
    } finally {
      this.held.delete(name);
    }
  }
}
