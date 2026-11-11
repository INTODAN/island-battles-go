// The Effects setting for tests: the saved choice decides whether particles play (issue #201),
// and without a saved choice it depends on the machine (`looksSlow`), so tests that need one say so.

/** Sets the saved Effects choice for the test, whatever the machine; returns a way to restore. */
export function stubGraphics(initial: "high" | "low") {
  const stored = new Map<string, string>([
    ["island-battles-web.graphics", initial],
  ]);
  const original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      getItem: (key: string) => stored.get(key) ?? null,
      setItem: (key: string, value: string) => stored.set(key, value),
    },
  });
  return {
    set: (value: "high" | "low") =>
      stored.set("island-battles-web.graphics", value),
    restore: () => {
      if (original) Object.defineProperty(globalThis, "localStorage", original);
      else Reflect.deleteProperty(globalThis, "localStorage");
    },
  };
}
