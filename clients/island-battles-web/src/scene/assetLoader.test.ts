import { afterEach, describe, expect, test } from "bun:test";
import { assetProgress, fetchAsset, onAssetProgress } from "./assetLoader";

const realFetch = globalThis.fetch;

/** A fake server: each file is sent in chunks the test releases one by one. */
function serve(files: Record<string, number>) {
  const controllers = new Map<
    string,
    ReadableStreamDefaultController<Uint8Array>
  >();
  let requests = 0;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    requests++;
    const url = String(input);
    const size = files[url];
    if (size === undefined) return new Response("missing", { status: 404 });
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controllers.set(url, controller);
      },
    });
    return new Response(body, { headers: { "content-length": String(size) } });
  }) as typeof fetch;
  return {
    send(url: string, bytes: number) {
      controllers.get(url)?.enqueue(new Uint8Array(bytes));
    },
    end(url: string) {
      controllers.get(url)?.close();
    },
    get requests() {
      return requests;
    },
  };
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 5));

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("asset downloads with progress (#102)", () => {
  test("counts bytes across files once their sizes are known, and fetches each once", async () => {
    const server = serve({ "/a.glb": 300, "/b.png": 100 });
    const updates: number[] = [];
    const stop = onAssetProgress(() =>
      updates.push(assetProgress(["/a.glb", "/b.png"]).fraction),
    );
    const a = fetchAsset("/a.glb");
    const b = fetchAsset("/b.png");
    expect(fetchAsset("/a.glb")).toBe(a);
    await tick();
    server.send("/a.glb", 150);
    await tick();
    expect(assetProgress(["/a.glb", "/b.png"]).fraction).toBeCloseTo(
      150 / 400,
      5,
    );
    server.send("/a.glb", 150);
    server.end("/a.glb");
    server.send("/b.png", 100);
    server.end("/b.png");
    expect((await a).byteLength).toBe(300);
    expect((await b).byteLength).toBe(100);
    const done = assetProgress(["/a.glb", "/b.png"]);
    expect(done.fraction).toBe(1);
    expect(done.filesDone).toBe(2);
    expect(server.requests).toBe(2);
    expect(updates.length).toBeGreaterThan(2);
    stop();
  });

  test("a file that fails can be asked for again", async () => {
    serve({});
    await expect(fetchAsset("/missing.glb")).rejects.toThrow("HTTP 404");
    const server = serve({ "/missing.glb": 4 });
    const again = fetchAsset("/missing.glb");
    await tick();
    server.send("/missing.glb", 4);
    server.end("/missing.glb");
    expect((await again).byteLength).toBe(4);
  });

  test("nothing to load is already done", () => {
    expect(assetProgress([]).fraction).toBe(1);
  });
});
