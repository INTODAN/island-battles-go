// Tests the replay's step timer (#273): the block of backend/services/replay.html
// between the playback-timer markers, run here on its own with fake timers.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const page = readFileSync(new URL("../services/replay.html", import.meta.url), "utf8");
const block = page.slice(page.indexOf("// playback-timer:begin"), page.indexOf("// playback-timer:end"));
assert.ok(block.includes("function createPlaybackTimer") && !block.includes("{{"), "the block is cut out and has no template actions");
const { createPlaybackTimer } = new Function(`${block}; return { createPlaybackTimer };`)();

test("the first step after restart() comes after one full interval", (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let ticks = 0;
  const timer = createPlaybackTimer(() => ticks++, () => 500);
  timer.restart();
  t.mock.timers.tick(499); // the timer has run, paused, for almost a step
  assert.equal(ticks, 0);
  timer.restart(); // Play is pressed
  t.mock.timers.tick(499);
  assert.equal(ticks, 0, "the leftover of the old interval must not fire early");
  t.mock.timers.tick(1);
  assert.equal(ticks, 1);
  t.mock.timers.tick(500);
  assert.equal(ticks, 2);
});

test("the interval is read again after every step", (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  let ticks = 0;
  let interval = 500;
  const timer = createPlaybackTimer(() => ticks++, () => interval);
  timer.restart();
  t.mock.timers.tick(500);
  assert.equal(ticks, 1);
  interval = 250;
  t.mock.timers.tick(500); // the pending step was scheduled at the old interval
  t.mock.timers.tick(250);
  assert.equal(ticks, 3);
});
