// Tests the replay's playback speed (#174): the block of backend/services/replay.html
// between the playback-speed markers, run here on its own.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const page = readFileSync(new URL("../services/replay.html", import.meta.url), "utf8");
const block = page.slice(page.indexOf("// playback-speed:begin"), page.indexOf("// playback-speed:end"));
assert.ok(block.includes("function playbackInterval") && !block.includes("{{"), "the block is cut out and has no template actions");
const { playbackInterval, playbackSpeeds } = new Function(`${block}; return { playbackInterval, playbackSpeeds };`)();

test("1x is 500 ms per generation, and the other speeds scale it", () => {
  assert.equal(playbackInterval(1), 500);
  assert.equal(playbackInterval(0.5), 1000);
  assert.equal(playbackInterval(2), 250);
  assert.equal(playbackInterval(4), 125);
});

test("a speed the select does not offer plays at 1x", () => {
  for (const speed of [0, -1, 3, NaN, Infinity, undefined]) assert.equal(playbackInterval(speed), 500, String(speed));
});

test("the select offers the same speeds", () => {
  const options = [...page.matchAll(/<select id="speed"[^>]*>(.*?)<\/select>/g)][0][1];
  const values = [...options.matchAll(/value="([^"]+)"/g)].map((match) => Number(match[1]));
  assert.deepEqual(values, playbackSpeeds);
  assert.match(options, /value="1" selected/);
});
