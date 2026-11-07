/** A readable value that tells its subscribers when it changes. */
export interface ReadableStore<T> {
  get(): T;
  /** Calls `listener` with each new value (not with the current one). Returns the unsubscribe function. */
  subscribe(listener: (value: T) => void): () => void;
}

export interface Store<T> extends ReadableStore<T> {
  /** Replaces the value. Setting the identical value (Object.is) notifies nobody. */
  set(value: T): void;
}

export function createStore<T>(initial: T): Store<T> {
  let value = initial;
  const listeners = new Set<(value: T) => void>();
  return {
    get: () => value,
    set(next) {
      if (Object.is(next, value)) return;
      value = next;
      for (const listener of [...listeners]) listener(next);
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
}
