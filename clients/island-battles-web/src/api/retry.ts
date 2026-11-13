/** What the user reads when the server throttled a request and the retries did not help. */
export const serverBusyMessage = "The server is busy. Try again in a moment.";

/** The wait before each retry; three entries mean up to three retries. */
export const retryDelaysMs = [500, 1000, 2000];
/** Each wait grows by a random share of up to this fraction. */
const jitterFraction = 0.25;
/** No wait is longer than this, whatever Retry-After asks for. */
export const maxRetryWaitMs = 5000;

/**
 * Requests other than GET that are safe to send again: they set a value, so a
 * repeat changes nothing. Reopening a lobby after its game is one too: a
 * lobby that already waits comes back unchanged. So is getting a player
 * token, which stores nothing. Creating, joining, starting,
 * adding or removing a seat and kicking a watcher are not in this list and are
 * never retried.
 */
const repeatableRequests: ReadonlyArray<{ method: string; path: RegExp }> = [
  { method: "POST", path: /\/lobby\/v1\/player-tokens$/ },
  {
    method: "PUT",
    path: /\/lobby\/v1\/games\/[^/]+\/lobbies\/[^/]+\/member-settings$/,
  },
  { method: "PUT", path: /\/lobby\/v1\/games\/[^/]+\/lobbies\/[^/]+\/team$/ },
  { method: "PUT", path: /\/lobby\/v1\/games\/[^/]+\/lobbies\/[^/]+\/ready$/ },
  {
    method: "POST",
    path: /\/lobby\/v1\/games\/[^/]+\/lobbies\/[^/]+\/heartbeat$/,
  },
  {
    method: "POST",
    path: /\/lobby\/v1\/games\/[^/]+\/lobbies\/[^/]+\/reopen$/,
  },
];

/** Whether a request may be sent again after a 429. */
export function isRepeatable(method: string, url: string): boolean {
  const upper = method.toUpperCase();
  if (upper === "GET") return true;
  const { pathname } = new URL(url, "http://localhost");
  return repeatableRequests.some(
    (entry) => entry.method === upper && entry.path.test(pathname),
  );
}

/** Milliseconds a Retry-After header asks for, or 0 when it is missing or unusable. */
function retryAfterMs(response: Response): number {
  const value = response.headers.get("Retry-After");
  if (value === null) return 0;
  const seconds = Number(value);
  if (Number.isFinite(seconds) && seconds >= 0) return seconds * 1000;
  const date = Date.parse(value);
  return Number.isNaN(date) ? 0 : Math.max(0, date - Date.now());
}

export interface RetryOptions {
  /** Waits for the given milliseconds; tests replace it so nothing really waits. */
  sleep?: ((milliseconds: number) => Promise<void>) | undefined;
  /** Returns a number in [0, 1) for the jitter; tests replace it. */
  random?: (() => number) | undefined;
}

const realSleep = (milliseconds: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, milliseconds));

/**
 * Wraps fetch so a request answered with 429 is sent again, up to
 * retryDelaysMs.length times, when isRepeatable allows it. The last response
 * is returned when the retries run out.
 */
export function retryThrottled(
  inner: typeof fetch,
  { sleep = realSleep, random = Math.random }: RetryOptions = {},
): typeof fetch {
  return (async (input: Request | string | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    if (!isRepeatable(request.method, request.url)) return inner(request);
    for (let attempt = 0; ; attempt++) {
      // A request body can be read once, so each attempt sends a copy.
      const response = await inner(request.clone());
      if (response.status !== 429 || attempt >= retryDelaysMs.length) {
        return response;
      }
      const base = retryDelaysMs[attempt] ?? maxRetryWaitMs;
      const wait = Math.min(
        maxRetryWaitMs,
        Math.max(
          base * (1 + jitterFraction * random()),
          retryAfterMs(response),
        ),
      );
      await sleep(wait);
    }
  }) as typeof fetch;
}
