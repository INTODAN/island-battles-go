// Tests the heading lines' geometry (#89), which is the block of backend/services/replay.html between the
// heading-lines markers. The page is a Go template, so the block is cut out and run here on its own.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const page = readFileSync(new URL("../services/replay.html", import.meta.url), "utf8");
const block = page.slice(page.indexOf("// heading-lines:begin"), page.indexOf("// heading-lines:end"));
assert.ok(block.includes("function planHeadings") && !block.includes("{{"), "the block is cut out and has no template actions");
const { planHeadings, headingStyle, tintOf } = new Function(`${block}; return { planHeadings, headingStyle, tintOf };`)();

const castle = (id, x, y) => ({ id, x, y, radius: 50 });
const group = (id, team, x, y, origin, destination) => ({ id, team, x, y, radius: 25, origin, destination });
const degrees = (value) => value * Math.PI / 180;
const away = (from, to) => Math.hypot(to[0] - from.x, to[1] - from.y);

test("a line runs from the group to the castle's circle, with its arrowhead there", () => {
  const [line] = planHeadings([castle(0, 100, 300), castle(1, 700, 300)], [group(1, 1, 300, 300, 0, 1)]);
  assert.deepEqual(line.start, [300, 300]);
  assert.ok(Math.abs(away(castle(1, 700, 300), line.tip) - (50 + headingStyle.castleGap)) < 1e-9);
  assert.ok(line.head);
  assert.deepEqual(line.passes, []);
});

test("a group close to its castle has no line", () => {
  const castles = [castle(0, 100, 300), castle(1, 700, 300)];
  // 50 + 25 + headLength + 3 from the center is the nearest a line is drawn from.
  const nearest = 700 - (50 + headingStyle.castleGap + 25 + headingStyle.headLength + 3);
  assert.equal(planHeadings(castles, [group(1, 1, nearest + 1, 300, 0, 1)]).length, 0);
  assert.equal(planHeadings(castles, [group(1, 1, nearest - 1, 300, 0, 1)]).length, 1);
  assert.equal(planHeadings(castles, [group(1, 1, 700, 300, 0, 1)]).length, 0);
});

test("a group whose destination is not on the board has no line", () => {
  assert.equal(planHeadings([castle(0, 100, 300)], [group(1, 1, 300, 300, 0, 7)]).length, 0);
});

test("groups of one team on one route share the rearmost group's line", () => {
  const castles = [castle(0, 100, 300), castle(1, 700, 300)];
  const lines = planHeadings(castles, [group(3, 1, 500, 300, 0, 1), group(1, 1, 200, 300, 0, 1), group(2, 1, 350, 300, 0, 1)]);
  assert.deepEqual(lines.map((line) => line.id), [1]);
});

test("a different team on the same route keeps its own line", () => {
  const castles = [castle(0, 100, 300), castle(1, 700, 300)];
  const lines = planHeadings(castles, [group(1, 1, 200, 300, 0, 1), group(2, 2, 350, 300, 0, 1)]);
  assert.deepEqual(lines.map((line) => line.id), [1, 2]);
});

test("lines come in group ID order", () => {
  const castles = [castle(0, 100, 100), castle(1, 700, 100), castle(2, 400, 500)];
  const lines = planHeadings(castles, [group(9, 1, 300, 100, 0, 1), group(2, 2, 400, 300, 0, 2), group(5, 3, 500, 100, 1, 0)]);
  assert.deepEqual(lines.map((line) => line.id), [2, 5, 9]);
});

test("opposite directions on one path keep to their own side, each with an arrowhead", () => {
  const castles = [castle(0, 100, 300), castle(1, 700, 300)];
  const lines = planHeadings(castles, [group(1, 1, 250, 300, 0, 1), group(2, 2, 550, 300, 1, 0)]);
  const [east, west] = lines;
  // To the right of travel, with y down: south when heading east, north when heading west.
  assert.equal(east.start[1], 300 + headingStyle.lane);
  assert.equal(west.start[1], 300 - headingStyle.lane);
  assert.ok(east.head && west.head);
  // Each tip is on the castle's circle, on the same side.
  assert.ok(Math.abs(away(castles[1], east.tip) - 50) < 1e-9);
  assert.ok(Math.abs(away(castles[0], west.tip) - 50) < 1e-9);
  assert.equal(east.tip[1], 300 + headingStyle.lane);
});

test("same-team lines within 15 degrees share one arrowhead, and farther ones do not", () => {
  const target = castle(0, 400, 300);
  const at = (id, team, degreesFrom, distance = 300) => group(id, team, 400 + distance * Math.cos(degrees(degreesFrom)), 300 + distance * Math.sin(degrees(degreesFrom)), 9, 0);
  const near = planHeadings([target], [at(1, 1, 200), at(2, 1, 210)]);
  assert.deepEqual(near.map((line) => line.head), [true, false]);
  assert.deepEqual(near[0].tip, near[1].tip);
  const far = planHeadings([target], [at(1, 1, 200), at(2, 1, 220)]);
  assert.deepEqual(far.map((line) => line.head), [true, true]);
  const teams = planHeadings([target], [at(1, 1, 200), at(2, 2, 205)]);
  assert.deepEqual(teams.map((line) => line.head), [true, true]);
});

test("a cluster across the seam shares one arrowhead", () => {
  const target = castle(0, 400, 300);
  const at = (id, degreesFrom) => group(id, 1, 400 + 300 * Math.cos(degrees(degreesFrom)), 300 + 300 * Math.sin(degrees(degreesFrom)), 9, 0);
  const lines = planHeadings([target], [at(1, 175), at(2, 185)]);
  assert.deepEqual(lines.map((line) => line.head), [true, false]);
});

test("a line that runs under another castle says so", () => {
  const castles = [castle(0, 100, 300), castle(1, 700, 300), castle(2, 400, 330), castle(3, 400, 100)];
  const [line] = planHeadings(castles, [group(1, 1, 230, 300, 0, 1)]);
  assert.deepEqual(line.passes, [2]);
});

test("tints are opaque and lighter than the team colour", () => {
  const tint = tintOf("#3464e0");
  assert.match(tint, /^#[0-9a-f]{6}$/);
  assert.notEqual(tint, "#3464e0");
});

test("the page keeps the caption, and draws lines under castles, groups and the clash overlay", () => {
  assert.ok(page.includes("Lines show where troops are heading."));
  const render = page.slice(page.indexOf("function render(animateClashes"));
  const order = ["renderHeadings(frame)", '"data-kind": "castle"', '"data-kind": "soldier-group"', "renderClashes(frame, animate)"].map((text) => render.indexOf(text));
  assert.ok(order.every((index) => index > 0) && order.every((index, position) => position === 0 || index > order[position - 1]));
});
