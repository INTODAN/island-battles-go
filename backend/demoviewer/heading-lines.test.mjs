// Tests the heading lines' geometry (#89, #168), which is the block of backend/services/replay.html between the
// heading-lines markers. The page is a Go template, so the block is cut out and run here on its own. The 3D client's
// clients/island-battles-web/src/scene/headingLines.test.ts tests the same rules; the case numbers are #168's.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const page = readFileSync(new URL("../services/replay.html", import.meta.url), "utf8");
const block = page.slice(page.indexOf("// heading-lines:begin"), page.indexOf("// heading-lines:end"));
assert.ok(block.includes("function planHeadings") && !block.includes("{{"), "the block is cut out and has no template actions");
const { planHeadings, headingStyle, tintOf, heldGroupIds, spreadAngles, clusterAngles, laneSpacing } = new Function(
  `${block}; return { planHeadings, headingStyle, tintOf, heldGroupIds, spreadAngles, clusterAngles, laneSpacing };`)();

const castle = (id, x, y) => ({ id, x, y, radius: 50 });
const group = (id, team, x, y, origin, destination) => ({ id, team, x, y, radius: 25, origin, destination });
const degrees = (value) => value * Math.PI / 180;
const away = (from, to) => Math.hypot(to[0] - from.x, to[1] - from.y);
const distance = (a, b) => Math.hypot(a[0] - b[0], a[1] - b[1]);
const reach = 50 + headingStyle.castleGap;
const target = castle(0, 400, 300);
// A group distance from the target at an angle (y down), heading there.
const at = (id, team, degreesFrom, distance = 300) => group(id, team, 400 + distance * Math.cos(degrees(degreesFrom)), 300 + distance * Math.sin(degrees(degreesFrom)), 9, 0);
const byId = (lines) => new Map(lines.map((line) => [line.id, line]));
// The arrowhead as a triangle, casing included.
function headTriangle(line) {
  const [ux, uy] = line.direction;
  const half = (headingStyle.headWidth + 2 * headingStyle.casing) / 2;
  return [line.tip, [line.base[0] - uy * half, line.base[1] + ux * half], [line.base[0] + uy * half, line.base[1] - ux * half]];
}
// The least distance between two convex polygons, 0 when they overlap.
function polygonGap(first, second) {
  let gap = 0;
  for (const polygon of [first, second]) {
    polygon.forEach((point, index) => {
      const next = polygon[(index + 1) % polygon.length];
      const length = distance(point, next);
      const axis = [-(next[1] - point[1]) / length, (next[0] - point[0]) / length];
      const project = (shape) => shape.map((corner) => corner[0] * axis[0] + corner[1] * axis[1]);
      const a = project(first);
      const b = project(second);
      gap = Math.max(gap, Math.min(...b) - Math.max(...a), Math.min(...a) - Math.max(...b));
    });
  }
  return gap;
}
const offLine = (point, line) => {
  const length = distance(line.start, line.base);
  const [dx, dy] = [(line.base[0] - line.start[0]) / length, (line.base[1] - line.start[1]) / length];
  return Math.abs((point[0] - line.start[0]) * dy - (point[1] - line.start[1]) * dx);
};

test("a line runs from the group to the castle's circle, with its arrowhead there", () => {
  const [line] = planHeadings([castle(0, 100, 300), castle(1, 700, 300)], [group(1, 1, 300, 300, 0, 1)]);
  assert.deepEqual(line.start, [300, 300]);
  assert.ok(Math.abs(away(castle(1, 700, 300), line.tip) - reach) < 1e-9);
  assert.ok(Math.abs(distance(line.tip, line.base) - headingStyle.headLength) < 1e-9);
  assert.ok(line.head);
  assert.equal(line.held, false);
  assert.deepEqual(line.passes, []);
});

test("a group close to its castle has no line", () => {
  const castles = [castle(0, 100, 300), castle(1, 700, 300)];
  // reach + 25 + headLength + minShaft from the center is the nearest a line is drawn from.
  const nearest = 700 - (reach + 25 + headingStyle.headLength + headingStyle.minShaft);
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

test("lines come in group ID order", () => {
  const castles = [castle(0, 100, 100), castle(1, 700, 100), castle(2, 400, 500)];
  const lines = planHeadings(castles, [group(9, 1, 300, 100, 0, 1), group(2, 2, 400, 300, 0, 2), group(5, 3, 500, 100, 1, 0)]);
  assert.deepEqual(lines.map((line) => line.id), [2, 5, 9]);
});

test("case 1: two teams' heads at one castle are spread so they never touch", () => {
  for (const apart of [0, 3, 8, 26]) {
    const lines = planHeadings([target], [at(1, 1, 150), at(2, 2, 150 + apart)]);
    assert.equal(lines.length, 2);
    assert.ok(lines.every((line) => line.head));
    assert.ok(polygonGap(headTriangle(lines[0]), headTriangle(lines[1])) >= headingStyle.headGap * .99, `apart ${apart}`);
    for (const line of lines) assert.ok(Math.abs(away(target, line.tip) - reach) < 1e-9);
  }
});

test("case 1: heads far enough apart are not moved", () => {
  const lines = planHeadings([target], [at(1, 1, 0), at(2, 2, 90)]);
  assert.ok(distance(lines[0].tip, [400 + reach, 300]) < 1e-9);
  assert.ok(distance(lines[1].tip, [400, 300 + reach]) < 1e-9);
});

test("case 2: a laned line and a plain one of one team share one head", () => {
  const castles = [castle(1, 0, 300), target];
  const lines = byId(planHeadings(castles, [group(1, 1, 150, 300, 1, 0), at(2, 1, 172, 250), group(3, 2, 280, 300, 0, 1)]));
  assert.ok(Math.abs(lines.get(1).start[1] - 300) > laneSpacing / 2 - 1e-9);
  assert.deepEqual([lines.get(1).head, lines.get(2).head].sort(), [false, true]);
  assert.ok(distance(lines.get(1).base, lines.get(2).base) < 1e-9);
});

test("case 3: opposite lanes are far enough apart that lines and heads never touch", () => {
  const castles = [castle(0, 100, 300), castle(1, 700, 300)];
  const [east, west] = planHeadings(castles, [group(1, 1, 250, 300, 0, 1), group(2, 2, 550, 300, 1, 0)]);
  // To the right of travel, with y down: south when heading east, north when heading west.
  assert.equal(east.start[1], 300 + laneSpacing / 2);
  assert.equal(west.start[1], 300 - laneSpacing / 2);
  assert.ok(east.head && west.head);
  assert.ok(Math.abs(away(castles[1], east.tip) - reach) < 1e-9);
  assert.ok(Math.abs(away(castles[0], west.tip) - reach) < 1e-9);
  const lineHalf = (headingStyle.width + 2 * headingStyle.casing) / 2;
  for (const [line, other] of [[east, west], [west, east]]) {
    for (const corner of headTriangle(line)) assert.ok(offLine(corner, other) - lineHalf >= headingStyle.laneGap - 1e-9);
  }
});

test("case 4: a third castle's group on the same corridor gets a lane", () => {
  const castles = [castle(0, 100, 300), castle(1, 700, 300), castle(9, 900, 310)];
  const lines = byId(planHeadings(castles, [group(1, 1, 250, 300, 0, 1), group(2, 2, 550, 303, 9, 0)]));
  assert.ok(lines.get(1).start[1] > 300);
  assert.ok(lines.get(2).start[1] < 303);
  assert.ok(lines.get(1).start[1] - lines.get(2).start[1] > laneSpacing - 4);
});

test("case 4: a group whose own line is hidden does not shift others", () => {
  const castles = [castle(0, 100, 300), castle(1, 700, 300)];
  const lines = planHeadings(castles, [group(1, 1, 250, 300, 0, 1), group(2, 2, 160, 300, 1, 0)]);
  assert.deepEqual(lines.map((line) => line.id), [1]);
  assert.equal(lines[0].start[1], 300);
});

test("case 4: lines that only cross are not laned", () => {
  const castles = [castle(0, 100, 300), castle(1, 700, 300), castle(2, 400, 600), castle(3, 400, 0)];
  const lines = byId(planHeadings(castles, [group(1, 1, 250, 300, 0, 1), group(2, 2, 400, 450, 2, 3)]));
  assert.equal(lines.get(1).start[1], 300);
});

test("case 5: two teams sailing the same way on one path get parallel lanes", () => {
  // This replaces #89's rule that another team's group on the same route keeps its line on it.
  const castles = [castle(0, 100, 300), castle(1, 700, 300)];
  const lines = planHeadings(castles, [group(1, 1, 350, 300, 0, 1), group(2, 2, 200, 300, 0, 1)]);
  assert.deepEqual(lines.map((line) => line.id), [1, 2]);
  assert.equal(lines[0].start[1], 300 - laneSpacing / 2);
  assert.equal(lines[1].start[1], 300 + laneSpacing / 2);
  assert.ok(polygonGap(headTriangle(lines[0]), headTriangle(lines[1])) > 0);
});

test("case 6: lines sharing a head end at its base, not at its tip", () => {
  const lines = byId(planHeadings([target], [at(1, 1, 200), at(2, 1, 210)]));
  assert.deepEqual([lines.get(1).head, lines.get(2).head], [true, false]);
  assert.deepEqual(lines.get(1).tip, lines.get(2).tip);
  assert.deepEqual(lines.get(1).base, lines.get(2).base);
  assert.ok(Math.abs(distance(lines.get(2).base, lines.get(2).tip) - headingStyle.headLength) < 1e-9);
  // The page draws the member's shaft to the base: its end is inside the head's base.
  const render = page.slice(page.indexOf("function renderHeadings"));
  assert.ok(render.includes(": line.base;"));
});

test("case 7: a shared head covers at most 15 degrees in total", () => {
  const angles = [200, 207, 214, 221, 228];
  const lines = planHeadings([target], angles.map((angle, index) => at(index + 1, 1, angle)));
  const heads = new Map();
  for (const line of lines) heads.set(line.base.join(), [...(heads.get(line.base.join()) || []), angles[line.id - 1]]);
  assert.ok(heads.size > 1);
  for (const members of heads.values()) assert.ok(Math.max(...members) - Math.min(...members) <= 15);
  assert.equal(lines.filter((line) => line.head).length, heads.size);
  assert.deepEqual(clusterAngles([0, 7, 14, 21, 28].map(degrees), degrees(15)), [[0, 1, 2], [3, 4]]);
});

test("case 8: angles either side of 180 degrees are compared round the circle", () => {
  const lines = byId(planHeadings([target], [at(1, 1, 174), at(2, 1, 10), at(3, 1, -174)]));
  assert.equal(lines.get(1).head, true);
  assert.equal(lines.get(3).head, false);
  assert.equal(lines.get(2).head, true);
  assert.ok(Math.abs(Math.abs(Math.atan2(lines.get(1).tip[1] - 300, lines.get(1).tip[0] - 400)) - Math.PI) < 1e-6);
  // Two lines either side of the seam share too.
  assert.deepEqual(planHeadings([target], [at(1, 1, 175), at(2, 1, 185)]).map((line) => line.head), [true, false]);
});

test("case 14: the line of a group that is not moving is dashed", () => {
  const still = { id: 1, x: 1, y: 0 };
  const moving = { id: 2, x: 2, y: 2 };
  assert.deepEqual([...heldGroupIds({ soldier_groups: [still, moving] }, { soldier_groups: [still, { ...moving, x: 1.5 }], clashes: [] })], [1]);
  // A survivor that a clash stopped half-way between the two contact positions.
  const clash = { participants: [{ group_id: 3, x: -1.1, y: 0 }, { group_id: 4, x: -.9, y: 0 }] };
  assert.deepEqual([...heldGroupIds(undefined, { soldier_groups: [{ id: 3, x: -1, y: 0 }], clashes: [clash] })], [3]);
  const [line] = planHeadings([castle(0, 100, 300), castle(1, 700, 300)], [group(1, 1, 300, 300, 0, 1)], new Set([1]));
  assert.equal(line.held, true);
  const render = page.slice(page.indexOf("function renderHeadings"));
  assert.ok(render.includes('"stroke-dasharray": headingStyle.heldDash'));
});

test("every line drawn is longer than its head and a piece of shaft, never NaN", () => {
  let seed = 7;
  const random = () => { seed = (seed * 16807) % 2147483647; return seed / 2147483647; };
  for (let round = 0; round < 60; round++) {
    const groups = Array.from({ length: 6 }, (_, index) => at(index + 1, 1 + Math.floor(random() * 2), random() * 360, 60 + random() * 200));
    for (const line of planHeadings([target, castle(6, 900, 900)], groups)) {
      assert.ok([...line.start, ...line.tip, ...line.base].every(Number.isFinite));
      const [dx, dy] = [400 - line.start[0], 300 - line.start[1]];
      const length = Math.hypot(dx, dy);
      assert.ok(((line.base[0] - line.start[0]) * dx + (line.base[1] - line.start[1]) * dy) / length - 25 >= headingStyle.minShaft - 1e-9);
    }
  }
  assert.deepEqual(planHeadings([target], [at(1, 1, 0, 0)]), []);
});

test("spreading angles moves only those too close, by as little as needed", () => {
  const spread = spreadAngles([0, .05, 2, Math.PI - .02, -Math.PI + .02], .2);
  assert.equal(spread[2], 2);
  assert.ok(Math.abs((spread[0] + spread[1]) / 2 - .025) < 1e-9);
  assert.ok(Math.abs(spread[1] - spread[0] - .2) < 1e-9);
  assert.ok(Math.abs(Math.abs(Math.atan2(Math.sin(spread[4] - spread[3]), Math.cos(spread[4] - spread[3]))) - .2) < 1e-9);
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
  const order = ["renderHeadings(frame, replay.frames[turn - 1])", '"data-kind": "castle"', '"data-kind": "soldier-group"', "renderClashes(frame, animate)"].map((text) => render.indexOf(text));
  assert.ok(order.every((index) => index > 0) && order.every((index, position) => position === 0 || index > order[position - 1]));
});
