// Fetches the board's asset files once per page and counts their bytes as they arrive, so a
// loading screen can show progress (issue #102). Files come from the page's own origin: the
// build copies them under /play/assets/ with hashed names, and the desktop app serves them too.

interface Download {
  loaded: number;
  /** From Content-Length, or 0 until known. */
  total: number;
  done: boolean;
  promise: Promise<ArrayBuffer>;
}

export interface AssetProgress {
  /** From 0 to 1. */
  fraction: number;
  files: number;
  filesDone: number;
}

const downloads = new Map<string, Download>();
const listeners = new Set<() => void>();

function notify(): void {
  for (const listener of listeners) listener();
}

async function readAll(
  response: Response,
  entry: Download,
): Promise<ArrayBuffer> {
  const length = Number(response.headers.get("content-length") ?? "0");
  entry.total = Number.isFinite(length) ? length : 0;
  notify();
  const reader = response.body?.getReader();
  if (!reader) {
    const buffer = await response.arrayBuffer();
    entry.loaded = buffer.byteLength;
    return buffer;
  }
  const chunks: Uint8Array[] = [];
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
    entry.loaded += value.byteLength;
    notify();
  }
  const bytes = new Uint8Array(entry.loaded);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return bytes.buffer;
}

/** The file's bytes, fetched once for the page however often they are asked for. */
export function fetchAsset(url: string): Promise<ArrayBuffer> {
  const existing = downloads.get(url);
  if (existing) return existing.promise;
  const entry: Download = {
    loaded: 0,
    total: 0,
    done: false,
    promise: Promise.resolve(new ArrayBuffer(0)),
  };
  entry.promise = fetch(url)
    .then((response) => {
      if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
      return readAll(response, entry);
    })
    .then(
      (buffer) => {
        entry.done = true;
        entry.total = Math.max(entry.total, buffer.byteLength);
        notify();
        return buffer;
      },
      (error) => {
        // A failed file can be asked for again.
        downloads.delete(url);
        notify();
        throw error;
      },
    );
  downloads.set(url, entry);
  notify();
  return entry.promise;
}

/** How far the given files have come: by bytes once every size is known, by files before. */
export function assetProgress(urls: readonly string[]): AssetProgress {
  let loaded = 0;
  let total = 0;
  let filesDone = 0;
  let sizesKnown = true;
  let perFile = 0;
  for (const url of urls) {
    const entry = downloads.get(url);
    if (entry?.done) filesDone++;
    if (!entry || entry.total === 0) {
      sizesKnown = false;
      continue;
    }
    loaded += Math.min(entry.loaded, entry.total);
    total += entry.total;
    perFile += Math.min(1, entry.loaded / entry.total);
  }
  const fraction =
    urls.length === 0
      ? 1
      : sizesKnown && total > 0
        ? loaded / total
        : perFile / urls.length;
  return { fraction, files: urls.length, filesDone };
}

/** Calls `listener` whenever any download moves on; returns a function that stops it. */
export function onAssetProgress(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
