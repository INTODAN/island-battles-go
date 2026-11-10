// A fixed set of reusable things, grouped by kind (issue #197). The turn's particle bursts take a
// ready-made system from here instead of building one in the middle of a frame (building a GPU
// particle system costs 30 to 48 ms). It knows nothing about Babylon, so tests run it with a
// fake system.

export interface PoolKind {
  /** What a use asks for. */
  key: string;
  /** How many of this kind exist, so how many uses can run at once. */
  systems: number;
}

/** One use of a pooled system, from `take` until it is freed. */
export interface Lease<T> {
  readonly key: string;
  readonly system: T;
  /** The time it is freed at; `Infinity` while it is held open (a stream). */
  until: number;
  /** False once freed: a handle kept past that point must not touch the system. */
  active: boolean;
}

export class SystemPool<T> {
  private readonly idle = new Map<string, T[]>();
  private readonly leases = new Set<Lease<T>>();
  private readonly busyByKind = new Map<string, number>();
  private readonly all: T[] = [];
  /** Uses turned away because every system of the kind was busy. */
  dropped = 0;
  /** The most systems of each kind that were busy at once. */
  readonly highWater = new Map<string, number>();

  /**
   * Builds every system now, with `create(kind, index)`. This is the only place that builds one.
   * `free(system)` puts a finished system back to rest.
   */
  constructor(
    kinds: readonly PoolKind[],
    create: (kind: PoolKind, index: number) => T,
    private readonly free: (system: T) => void,
  ) {
    for (const kind of kinds) {
      const systems: T[] = [];
      for (let index = 0; index < kind.systems; index++) {
        systems.push(create(kind, index));
      }
      this.idle.set(kind.key, systems);
      this.busyByKind.set(kind.key, 0);
      this.highWater.set(kind.key, 0);
      this.all.push(...systems);
    }
  }

  /** Every system the pool owns, busy or not. */
  get systems(): readonly T[] {
    return this.all;
  }

  /**
   * A free system of `key` until time `until`, or null (counted in `dropped`) when the kind has
   * none free or does not exist. Systems already running are never cut short. `counted` false
   * leaves the use out of `highWater` (the warm-up).
   */
  take(key: string, until: number, counted = true): Lease<T> | null {
    const system = this.idle.get(key)?.pop();
    if (system === undefined) {
      this.dropped++;
      return null;
    }
    const lease: Lease<T> = { key, system, until, active: true };
    this.leases.add(lease);
    const busy = (this.busyByKind.get(key) ?? 0) + 1;
    this.busyByKind.set(key, busy);
    if (counted && busy > (this.highWater.get(key) ?? 0)) {
      this.highWater.set(key, busy);
    }
    return lease;
  }

  /** Frees every lease whose time has come at `now`. */
  update(now: number): void {
    for (const lease of this.leases) {
      if (lease.until <= now) this.release(lease);
    }
  }

  /** Frees every lease at once. */
  releaseAll(): void {
    for (const lease of this.leases) this.release(lease);
  }

  private release(lease: Lease<T>): void {
    this.leases.delete(lease);
    lease.active = false;
    this.free(lease.system);
    this.idle.get(lease.key)?.push(lease.system);
    this.busyByKind.set(lease.key, (this.busyByKind.get(lease.key) ?? 1) - 1);
  }

  busy(key: string): number {
    return this.busyByKind.get(key) ?? 0;
  }
}
