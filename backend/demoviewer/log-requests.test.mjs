// Tests when the replay panel asks for logs and how it retries (#160): the block of
// backend/services/replay.html between the log-requests markers, run here on its own.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const page = readFileSync(new URL("../services/replay.html", import.meta.url), "utf8");
const block = page.slice(page.indexOf("// log-requests:begin"), page.indexOf("// log-requests:end"));
assert.ok(block.includes("function createDebouncer") && !block.includes("{{"), "the block is cut out and has no template actions");
const { createDebouncer, nextLogRetry, logShownMilliseconds, logRetryMilliseconds, logRetryLimit } =
  new Function(`${block}; return { createDebouncer, nextLogRetry, logShownMilliseconds, logRetryMilliseconds, logRetryLimit };`)();

// A fake clock: timers fire when advance passes their time.
function clock() {
  let now = 0;
  let next = 1;
  const timers = new Map();
  return {
    setTimer: (action, delay) => { timers.set(next, { at: now + delay, action }); return next++; },
    clearTimer: (handle) => timers.delete(handle),
    advance(milliseconds) {
      now += milliseconds;
      for (const [handle, timer] of [...timers]) if (timer.at <= now) { timers.delete(handle); timer.action(); }
    },
  };
}

test("generations that pass quickly are not requested, the one that stays is", () => {
  const time = clock();
  const debouncer = createDebouncer(time.setTimer, time.clearTimer, logShownMilliseconds);
  const requested = [];
  for (let generation = 1; generation <= 20; generation++) {
    debouncer.run(() => requested.push(generation));
    time.advance(logShownMilliseconds - 1);
  }
  assert.deepEqual(requested, []);
  time.advance(1);
  assert.deepEqual(requested, [20]);
});

test("a cancelled request is never made", () => {
  const time = clock();
  const debouncer = createDebouncer(time.setTimer, time.clearTimer, logShownMilliseconds);
  const requested = [];
  debouncer.run(() => requested.push(1));
  debouncer.cancel();
  time.advance(10 * logShownMilliseconds);
  assert.deepEqual(requested, []);
});

test("a refused request is retried after one second, five times, then fails", () => {
  const waits = [];
  for (let retriesMade = 0; ; retriesMade++) {
    const wait = nextLogRetry(0, retriesMade);
    if (wait === null) break;
    waits.push(wait);
  }
  assert.deepEqual(waits, Array(logRetryLimit).fill(logRetryMilliseconds));
  assert.equal(logRetryLimit, 5);
  assert.equal(logRetryMilliseconds, 1000);
});

test("only a refusal is retried", () => {
  assert.equal(nextLogRetry(404, 0), null);
  assert.equal(nextLogRetry(500, 0), null);
  assert.equal(nextLogRetry(200, 0), null);
});
