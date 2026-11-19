import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { components } from "../../api/generated/game";
import { drawnPosition } from "../shore";
import type { TurnPlan, TurnSample } from "./plan";
import {
  DEFAULT_MAX_BEHIND,
  DEFAULT_MAX_FRAME_MS,
  type LiveTiming,
  MAX_LIVE_TURN_MS,
  MIN_LIVE_TURN_MS,
  PLAYING,
  type Stage,
  TurnPlayer,
  WATCHING,
} from "./player";

type Demo = components["schemas"]["Demo"];
type Generation = components["schemas"]["Generation"];

function load(name: string): Demo {
  return JSON.parse(
    readFileSync(
      new URL(`../../../test/fixtures/${name}.json`, import.meta.url),
      "utf8",
    ),
  ) as Demo;
}

const FRAME_MS = 1000 / 60;

/** Where the scene draws a group of `generation`: at its island's shore near a castle. */
function drawnIn(
  generation: Generation,
  group: Generation["soldier_groups"][number],
) {
  const castle = (id: number) => generation.castles.find((c) => c.id === id);
  return drawnPosition(
    group,
    castle(group.origin_castle_id),
    castle(group.destination_castle_id),
  );
}

/** A stage that remembers what it was told, and the poses it was shown. */
class FakeStage implements Stage {
  calls: string[] = [];
  settled: Generation | null = null;
  /** True between begin and end: a turn is on stage. */
  midTurn = false;
  lastSample: TurnSample | null = null;
  /** Every group's position at each apply, for jump checks. */
  jumps: number[] = [];
  private lastPoses = new Map<number, { x: number; y: number }>();

  settle(generation: Generation): void {
    this.calls.push(`settle:${generation.id}`);
    this.settled = generation;
    this.midTurn = false;
    this.lastSample = null;
    this.lastPoses.clear();
    for (const group of generation.soldier_groups) {
      this.lastPoses.set(group.id, drawnIn(generation, group));
    }
  }
  begin(plan: TurnPlan): void {
    this.calls.push(`begin:${plan.next.id}`);
    this.midTurn = true;
  }
  apply(sample: TurnSample): void {
    this.lastSample = sample;
    for (const [id, pose] of sample.groups) {
      const before = this.lastPoses.get(id);
      if (before && pose.visible) {
        this.jumps.push(Math.hypot(pose.x - before.x, pose.y - before.y));
      }
      if (pose.visible) this.lastPoses.set(id, { x: pose.x, y: pose.y });
    }
  }
  end(plan: TurnPlan): void {
    this.calls.push(`end:${plan.next.id}`);
    this.midTurn = false;
    this.settled = plan.next;
    this.lastPoses.clear();
    for (const group of plan.next.soldier_groups) {
      this.lastPoses.set(group.id, drawnIn(plan.next, group));
    }
  }
}

/** Runs `ms` of fake time in frames; returns the time at which the player went idle, if it did. */
function run(player: TurnPlayer, ms: number, onFrame?: (time: number) => void) {
  for (let time = 0; time < ms; time += FRAME_MS) {
    player.tick(FRAME_MS);
    onFrame?.(time);
  }
}

const busy = load("busy-turn");

describe("TurnPlayer", () => {
  test("plays a whole fixture with no jumps between frames or turns", () => {
    for (const name of ["busy-turn", "six-teams", "generated-map"]) {
      const demo = load(name);
      const stage = new FakeStage();
      const player = new TurnPlayer(stage);
      const frames = demo.frames;
      player.seek(frames[0] as Generation);
      for (let index = 1; index < frames.length; index++) {
        player.play(
          frames[index - 1] as Generation,
          frames[index] as Generation,
        );
        run(player, 1500 + 100);
      }
      expect(player.busy).toBe(false);
      expect(stage.settled).toBe(frames.at(-1) as Generation);
      // Settled once, up front: the turns joined up on their own.
      expect(
        stage.calls.filter((call) => call.startsWith("settle")),
      ).toHaveLength(1);
      expect(stage.calls.filter((call) => call.startsWith("end"))).toHaveLength(
        frames.length - 1,
      );
      // At 60 frames a second a group moves at most 2 units a turn over 90 frames, plus the
      // short slide a stopped survivor makes; nothing may leap.
      expect(Math.max(...stage.jumps)).toBeLessThan(0.1);
    }
  });

  test("a turn takes the configured time at normal speed", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage, { turnMs: 1000 });
    const [first, second] = busy.frames as [Generation, Generation];
    player.seek(first);
    player.play(first, second);
    run(player, 900);
    expect(stage.midTurn).toBe(true);
    run(player, 200);
    expect(stage.midTurn).toBe(false);
    expect(stage.settled).toBe(second);
  });

  test("setSpeed scales the pace and zero pauses", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage, { turnMs: 1000 });
    const [first, second] = busy.frames as [Generation, Generation];
    player.seek(first);
    player.setSpeed(0);
    player.play(first, second);
    run(player, 5000);
    expect(stage.midTurn).toBe(false);
    expect(player.busy).toBe(true);
    player.setSpeed(2);
    run(player, 520);
    expect(stage.settled).toBe(second);
  });

  test("a queue raises the speed gradually, never in steps, up to 4x (#176)", () => {
    const player = new TurnPlayer(new FakeStage());
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    expect(player.effectiveSpeed).toBe(1);
    for (let index = 1; index <= 8; index++) {
      player.play(frames[index - 1] as Generation, frames[index] as Generation);
    }
    // Queuing changes nothing at once.
    expect(player.effectiveSpeed).toBe(1);
    const speeds: number[] = [];
    run(player, 1500, () => speeds.push(player.effectiveSpeed));
    // It rises over a few hundred milliseconds: by less than a fifth a frame, ...
    for (let index = 1; index < speeds.length; index++) {
      expect(
        Math.abs((speeds[index] as number) - (speeds[index - 1] as number)),
      ).toBeLessThan(0.2);
    }
    // (about 100 ms in)
    expect(speeds[5] as number).toBeLessThan(2.5);
    // ... and reaches the catch-up the backlog asks for, at most 4x.
    expect(Math.max(...speeds)).toBeGreaterThan(3);
    expect(Math.max(...speeds)).toBeLessThanOrEqual(4);
  });

  test("turns that arrive together catch up within a few seconds", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage);
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    // Three bots' worth of turns land at once.
    for (let index = 1; index <= 3; index++) {
      player.play(frames[index - 1] as Generation, frames[index] as Generation);
    }
    let finished = Number.POSITIVE_INFINITY;
    run(player, 6000, (time) => {
      if (!player.busy && finished === Number.POSITIVE_INFINITY)
        finished = time;
    });
    expect(stage.settled).toBe(frames[3] as Generation);
    expect(finished).toBeLessThan(3000);
    // They were all played, none skipped.
    expect(stage.calls.filter((call) => call.startsWith("end"))).toHaveLength(
      3,
    );
  });

  test("a viewer never trails a fast live game by more than a few seconds", () => {
    const stage = new FakeStage();
    let now = 0;
    const player = new TurnPlayer(stage, { now: () => now });
    player.setLive(true);
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    // One turn arrives every 250 ms, six times faster than they play at 1x.
    const arrivedAt = new Map<number, number>();
    const shownAt = new Map<number, number>();
    let next = 1;
    let time = 0;
    for (; time < 12_000; time += FRAME_MS) {
      now = time;
      if (next < frames.length && time >= next * 250) {
        arrivedAt.set((frames[next] as Generation).id, time);
        player.play(frames[next - 1] as Generation, frames[next] as Generation);
        next++;
      }
      const before = stage.settled;
      player.tick(FRAME_MS);
      if (stage.settled !== before && stage.settled) {
        shownAt.set(stage.settled.id, time);
      }
    }
    expect(stage.settled).toBe(frames.at(-1) as Generation);
    for (const [id, shown] of shownAt) {
      const arrived = arrivedAt.get(id);
      if (arrived !== undefined) expect(shown - arrived).toBeLessThan(3000);
    }
  });

  test("beyond a large backlog, it skips to the newest", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage, { maxBehind: 3 });
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    for (let index = 1; index <= 4; index++) {
      player.play(frames[index - 1] as Generation, frames[index] as Generation);
    }
    expect(player.waiting).toBe(1);
    run(player, 2000);
    expect(stage.settled).toBe(frames[4] as Generation);
    // Only the newest turn played; the rest were skipped.
    expect(stage.calls.filter((call) => call.startsWith("end:"))).toEqual([
      `end:${(frames[4] as Generation).id}`,
    ]);
    expect(stage.calls).toContain(`settle:${(frames[3] as Generation).id}`);
  });

  test("by default the backlog it skips at is ten turns", () => {
    const player = new TurnPlayer(new FakeStage());
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    for (let index = 1; index <= DEFAULT_MAX_BEHIND; index++) {
      const previous = frames[(index - 1) % frames.length] as Generation;
      player.play(previous, frames[index % frames.length] as Generation);
    }
    expect(player.waiting).toBe(DEFAULT_MAX_BEHIND);
    player.play(frames[0] as Generation, frames[1] as Generation);
    expect(player.waiting).toBe(1);
  });

  test("seek ends a turn in progress at once and leaves nothing half-way", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage);
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    player.play(frames[0] as Generation, frames[1] as Generation);
    player.play(frames[1] as Generation, frames[2] as Generation);
    run(player, 700);
    expect(stage.midTurn).toBe(true);
    expect(player.progress).not.toBeNull();

    player.seek(frames[5] as Generation);
    expect(stage.midTurn).toBe(false);
    expect(stage.settled).toBe(frames[5] as Generation);
    expect(stage.calls.at(-1)).toBe(`settle:${(frames[5] as Generation).id}`);
    expect(player.busy).toBe(false);
    expect(player.progress).toBeNull();
    // Nothing else happens afterwards: the queue was dropped.
    const calls = stage.calls.length;
    run(player, 3000);
    expect(stage.calls).toHaveLength(calls);
  });

  test("a turn that does not follow what is shown settles on its start first", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage);
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    player.play(frames[4] as Generation, frames[5] as Generation);
    run(player, 100);
    expect(stage.calls).toContain(`settle:${(frames[4] as Generation).id}`);
    expect(
      stage.calls.indexOf(`settle:${(frames[4] as Generation).id}`),
    ).toBeLessThan(
      stage.calls.indexOf(`begin:${(frames[5] as Generation).id}`),
    );
  });

  test("reports each generation as it is shown", () => {
    const shown: number[] = [];
    const player = new TurnPlayer(new FakeStage(), {
      onShown: (generation) => shown.push(generation.id),
    });
    const frames = busy.frames as Generation[];
    player.seek(frames[0] as Generation);
    player.play(frames[0] as Generation, frames[1] as Generation);
    run(player, 2000);
    expect(shown).toEqual([
      (frames[0] as Generation).id,
      (frames[1] as Generation).id,
    ]);
  });
});

/** Many generations in a row, from the busy fixture's, numbered 0, 1, 2, ... */
const long: Generation[] = Array.from({ length: 400 }, (_, index) => ({
  ...(busy.frames[index % busy.frames.length] as Generation),
  id: index,
}));

/** A stage that records the turn position shown: the generation's number plus the progress. */
class PositionStage implements Stage {
  position = 0;
  private current = 0;
  settle(generation: Generation): void {
    this.position = generation.id;
  }
  begin(plan: TurnPlan): void {
    this.current = plan.previous.id;
  }
  apply(sample: TurnSample): void {
    this.position = this.current + sample.t;
  }
  end(plan: TurnPlan): void {
    this.position = plan.next.id;
  }
}

/**
 * Follows a live game for `ms` whose turns arrive `gap(n)` after the one before, in frames of
 * `frameMs`. Records, over the second half, how fast the turns play (turns a second, which a
 * cruising boat's speed is proportional to) and the turn length the player chose.
 */
function follow(
  gap: (index: number) => number,
  ms: number,
  frameMs = FRAME_MS,
) {
  const stage = new PositionStage();
  let now = 0;
  const player = new TurnPlayer(stage, { now: () => now });
  player.setLive(true);
  player.seek(long[0] as Generation);
  let next = 0;
  let arrival = 500;
  const rates: number[] = [];
  const durations: number[] = [];
  let last = stage.position;
  while (now < ms) {
    now += frameMs;
    while (now >= arrival && next < long.length - 1) {
      player.play(long[next] as Generation, long[next + 1] as Generation);
      next++;
      arrival += gap(next);
    }
    player.tick(frameMs);
    if (now > ms / 2) {
      rates.push(((stage.position - last) * 1000) / frameMs);
      durations.push(player.turnDuration);
    }
    last = stage.position;
  }
  return { rates, durations };
}

/** The largest change in speed from one frame to the next, as a fraction of the mean. */
function largestChange(rates: readonly number[]): number {
  const mean = rates.reduce((sum, rate) => sum + rate, 0) / rates.length;
  let largest = 0;
  for (let index = 1; index < rates.length; index++) {
    const change = Math.abs(
      (rates[index] as number) - (rates[index - 1] as number),
    );
    largest = Math.max(largest, change / mean);
  }
  return largest;
}

describe("following live (#176)", () => {
  test("steady turns play over the time between them, at one steady speed", () => {
    const { rates, durations } = follow(() => 1000, 30_000);
    for (const duration of durations) {
      expect(Math.abs(duration - 1000)).toBeLessThan(10);
    }
    // Never stopped, and the same speed every frame, across every turn's end.
    expect(Math.min(...rates)).toBeGreaterThan(0.9);
    expect(largestChange(rates)).toBeLessThan(0.01);
  });

  test("slow turns play over at most 3 s, then wait", () => {
    const { rates, durations } = follow(() => 5000, 60_000);
    for (const duration of durations) {
      expect(duration).toBeCloseTo(MAX_LIVE_TURN_MS, 0);
    }
    const moving = rates.filter((rate) => rate > 1e-6);
    for (const rate of moving) expect(rate).toBeCloseTo(1000 / 3000, 2);
    // Three seconds of every five move.
    expect(moving.length / rates.length).toBeCloseTo(0.6, 1);
  });

  test("fast turns play over no less than 250 ms", () => {
    const { durations } = follow(() => 100, 10_000);
    for (const duration of durations) {
      expect(duration).toBeGreaterThanOrEqual(MIN_LIVE_TURN_MS);
      expect(duration).toBeLessThan(MIN_LIVE_TURN_MS + 5);
    }
  });

  test("turns that arrive in bursts play at a steady speed between the bursts", () => {
    // Three turns every three seconds: at once, or 80 ms apart as a live server sends them.
    for (const apart of [0, 80]) {
      const { rates, durations } = follow(
        (index) => (index % 3 === 0 ? 3000 - 2 * apart : apart),
        40_000,
      );
      for (const duration of durations) {
        expect(Math.abs(duration - 1000)).toBeLessThan(50);
      }
      expect(Math.min(...rates)).toBeGreaterThan(0.8);
      expect(largestChange(rates)).toBeLessThan(0.02);
    }
  });

  test("turns that arrive unevenly never stop the boats", () => {
    let seed = 1;
    const jitter = () => {
      seed = (seed * 1664525 + 1013904223) % 2 ** 32;
      return seed / 2 ** 32;
    };
    const { rates } = follow(() => 600 + 800 * jitter(), 60_000);
    expect(Math.min(...rates)).toBeGreaterThan(0);
    // The speed eases: no frame changes it by more than a few per cent.
    expect(largestChange(rates)).toBeLessThan(0.05);
  });

  // Gaps ±40 % around 1 s and 0.5 s, as a game of bots that answer quickly or slowly (#361's
  // review): the buffer grows with the spread of the gaps, so the boats almost never wait.
  for (const [low, span] of [
    [600, 800],
    [300, 400],
  ] as const) {
    // 24 games of 50 turns each.
    test(`turns ${low} to ${low + span} ms apart stop the boats rarely and briefly, over 24 games`, () => {
      let stops = 0;
      let longest = 0;
      let turns = 0;
      for (let game = 1; game <= 24; game++) {
        let state = game;
        const jitter = () => {
          state = (state * 1664525 + 1013904223) % 2 ** 32;
          return state / 2 ** 32;
        };
        const run = live(() => low + span * jitter(), 55 * (low + span / 2));
        const after = stopsFrom(run, 5 * (low + span / 2));
        stops += after.length;
        longest = Math.max(longest, ...after);
        turns += lagsFrom(run, 5 * (low + span / 2)).length;
      }
      expect((stops / turns) * 100).toBeLessThan(0.2);
      expect(longest).toBeLessThanOrEqual(6);
    }, 30_000);
  }
});

describe("time steps (#176)", () => {
  test("one long frame advances by at most 100 ms", () => {
    const stage = new FakeStage();
    const player = new TurnPlayer(stage, { turnMs: 1000 });
    const [first, second] = busy.frames as [Generation, Generation];
    player.seek(first);
    player.play(first, second);
    // A tab comes back after five seconds in the background.
    player.tick(5000);
    expect(player.progress).toBeCloseTo(DEFAULT_MAX_FRAME_MS / 1000, 6);
    expect(stage.midTurn).toBe(true);
  });

  test("no time is lost across a turn's end", () => {
    const stage = new PositionStage();
    let next = 0;
    const player = new TurnPlayer(stage, {
      turnMs: 1000,
      // As the game view and the dev page do: the next turn is queued as one ends.
      onIdle: () => {
        player.play(long[next] as Generation, long[next + 1] as Generation);
        next++;
      },
    });
    player.seek(long[0] as Generation);
    player.play(long[0] as Generation, long[1] as Generation);
    next = 1;
    // Frames of 17 ms do not divide a turn: every turn ends part-way through a frame.
    const frame = 17;
    for (let count = 1; count <= 600; count++) {
      player.tick(frame);
      expect(stage.position).toBeCloseTo((count * frame) / 1000, 9);
    }
  });
});

/**
 * The positions `WATCHING` showed when its timing last changed on purpose: recorded when it began
 * to give back a lag instead of keeping it (#361).
 */
const watchingTrace = JSON.parse(
  readFileSync(new URL("./watching-trace.json", import.meta.url), "utf8"),
) as { steady: number[]; bursts: number[]; uneven: number[] };

interface LiveRun {
  /** The timing, or none for the player's default. */
  timing?: LiveTiming;
  frameMs?: number;
  /** No frame runs between these times, in ms, while turns keep arriving: a hidden tab. */
  hidden?: readonly [number, number];
  /** Switches the timing at this time, in ms. */
  switchTo?: { at: number; timing: LiveTiming };
  /**
   * The game's turn pace, given to the player (`setLivePace`) before following and again at
   * every arrival, as the game view does at every change of the view.
   */
  paceMs?: number;
  /** Gives the pace only once, before following. */
  paceOnce?: boolean;
  /** Jumps to the newest turn at this time, in ms, as Return to live does after a scrub back. */
  jumpAt?: number;
  /**
   * Stops following at `from` and follows again at `to`, in ms, playing the turns that arrived
   * meanwhile, as a watcher's Follow live does after a scrub back.
   */
  replay?: readonly [number, number];
}

/**
 * Follows a live game for `ms` whose turns arrive `gap(n)` after the one before, the first at
 * 500 ms. Records the position every 10th frame; the lag as each turn arrives (the backlog: how
 * far the board is behind the generation that was open, at that turn's end); after the first
 * turn has played, each stop: the frames in a row in which the board did not move; and the
 * board's speed in each frame, in turns a second.
 */
function live(gap: (index: number) => number, ms: number, run: LiveRun = {}) {
  const frameMs = run.frameMs ?? FRAME_MS;
  const stage = new PositionStage();
  let now = 0;
  const player = new TurnPlayer(stage, {
    now: () => now,
    ...(run.timing ? { liveTiming: run.timing } : {}),
  });
  if (run.paceMs !== undefined) player.setLivePace(run.paceMs);
  player.setLive(true);
  player.seek(long[0] as Generation);
  let next = 0;
  let arrival = 500;
  let frame = 0;
  let switched = false;
  const trace: number[] = [];
  const lags: { at: number; index: number; lag: number }[] = [];
  let jumped = false;
  let held: number | null = null;
  let replayed = false;
  const stops: { at: number; frames: number }[] = [];
  let still = 0;
  let last = stage.position;
  const speeds: number[] = [];
  const rates: { at: number; rate: number }[] = [];
  while (now < ms) {
    now += frameMs;
    if (run.replay && held === null && now >= run.replay[0]) {
      // Scrubbed back: following stops, and the turns that arrive aren't played.
      player.setLive(false);
      player.seek(long[next] as Generation);
      held = next;
    }
    if (run.replay && held !== null && !replayed && now >= run.replay[1]) {
      // Follow live again: the turns in between are queued, then following starts.
      for (let index = held; index < next; index++) {
        player.play(long[index] as Generation, long[index + 1] as Generation);
      }
      player.setLive(true);
      replayed = true;
    }
    while (now >= arrival && next < long.length - 1) {
      if (held !== null && !replayed) {
        next++;
        arrival += gap(next);
        continue;
      }
      lags.push({
        at: now,
        index: next + 1,
        lag: player.waiting + (player.busy ? 1 - (player.progress ?? 0) : 0),
      });
      player.play(long[next] as Generation, long[next + 1] as Generation);
      if (run.paceMs !== undefined && !run.paceOnce) {
        player.setLivePace(run.paceMs);
      }
      next++;
      arrival += gap(next);
    }
    if (run.jumpAt !== undefined && !jumped && now >= run.jumpAt) {
      // Scrubbed back, then Return to live: following stops, and the newest turn is shown at once.
      player.setLive(false);
      player.seek(long[next] as Generation);
      player.setLive(true);
      jumped = true;
    }
    if (run.switchTo && !switched && now >= run.switchTo.at) {
      player.setLiveTiming(run.switchTo.timing);
      switched = true;
    }
    if (run.hidden && now >= run.hidden[0] && now < run.hidden[1]) continue;
    player.tick(frameMs);
    frame++;
    if (frame % 10 === 0) trace.push(stage.position);
    speeds.push(player.catchUpSpeed);
    rates.push({ at: now, rate: ((stage.position - last) * 1000) / frameMs });
    if (stage.position >= 1) {
      if (stage.position === last) still++;
      else if (still > 0) {
        stops.push({ at: now, frames: still });
        still = 0;
      }
    }
    last = stage.position;
  }
  return { trace, lags, stops, speeds, rates, player };
}

/** The lengths of the stops that ended from `from` ms on, in frames. */
const stopsFrom = (run: ReturnType<typeof live>, from: number) =>
  run.stops.filter((stop) => stop.at >= from).map((stop) => stop.frames);

/** The lags as the turns from `index` on arrive. */
const lagsFromTurn = (run: ReturnType<typeof live>, index: number) =>
  run.lags.filter((entry) => entry.index >= index).map((entry) => entry.lag);

/** The lags from `from` ms on. */
const lagsFrom = (run: ReturnType<typeof live>, from: number) =>
  run.lags.filter((entry) => entry.at >= from).map((entry) => entry.lag);

let seed = 1;
const uneven = () => {
  seed = (seed * 1664525 + 1013904223) % 2 ** 32;
  return 600 + 800 * (seed / 2 ** 32);
};

describe("live timing (#345)", () => {
  test("by default and with WATCHING it plays exactly as the recorded trace", () => {
    const patterns: [keyof typeof watchingTrace, (index: number) => number][] =
      [
        ["steady", () => 1000],
        ["bursts", (index) => (index % 3 === 0 ? 3000 : 0)],
        ["uneven", () => uneven()],
      ];
    for (const timing of [undefined, WATCHING]) {
      for (const [name, gap] of patterns) {
        seed = 1;
        const { trace } = live(gap, 30_000, timing ? { timing } : {});
        const recorded = watchingTrace[name];
        expect(trace).toHaveLength(recorded.length);
        for (const [index, position] of trace.entries()) {
          expect(position).toBeCloseTo(recorded[index] as number, 9);
        }
      }
    }
  });

  test("a hand player's board reaches the open generation as its turn ends", () => {
    for (const pace of [500, 1000, 2000]) {
      const run = live(() => pace, 60_000, { timing: PLAYING });
      for (const lag of lagsFrom(run, 20_000)) expect(lag).toBeLessThan(0.05);
      // A turn may end a frame before the next is queued: never more than a frame's stop.
      const stops = stopsFrom(run, 20_000);
      expect(Math.max(0, ...stops)).toBeLessThanOrEqual(1);
      // At most 3 per 100 turns (at least one allowed).
      expect(stops.length).toBeLessThanOrEqual(
        Math.max(1, (40_000 / pace) * 0.03),
      );
    }
  });

  test("at a 0.5 s pace a watcher keeps the buffer in hand, and a hand player none", () => {
    const watching = lagsFrom(
      live(() => 500, 60_000),
      20_000,
    );
    const playing = lagsFrom(
      live(() => 500, 60_000, { timing: PLAYING }),
      20_000,
    );
    for (const lag of watching) {
      expect(Math.abs(lag - WATCHING.bufferTurns)).toBeLessThan(0.1);
    }
    expect(Math.max(...playing)).toBeLessThan(0.05);
  });

  test("a late turn stops the board, and the lag doesn't stay", () => {
    // Turn 20 comes 300 ms late; the others keep the server's pace.
    const run = live(
      (index) => (index === 20 ? 1300 : index === 21 ? 700 : 1000),
      40_000,
      { timing: PLAYING },
    );
    // The board waited about 300 ms for it.
    expect(Math.max(...stopsFrom(run, 20_000))).toBeGreaterThan(250 / FRAME_MS);
    // Three turns later it is back with the open generation.
    for (const lag of lagsFrom(run, 24_600)) expect(lag).toBeLessThan(0.1);
  });

  test("two turns that arrive together are caught up within a few turns, without a jump", () => {
    const run = live(
      (index) => (index === 20 ? 2000 : index === 21 ? 0 : 1000),
      40_000,
      { timing: PLAYING },
    );
    for (const lag of lagsFrom(run, 25_600)) expect(lag).toBeLessThan(0.1);
    expect(Math.max(...run.speeds)).toBeLessThanOrEqual(1.5);
    for (let index = 1; index < run.speeds.length; index++) {
      const change = Math.abs(
        (run.speeds[index] as number) - (run.speeds[index - 1] as number),
      );
      expect(change).toBeLessThan(0.05);
    }
  });

  test("with jitter on the arrivals a hand player's lag stays small", () => {
    seed = 7;
    const run = live(
      () => 1020 + 80 * ((uneven() - 600) / 800 - 0.5),
      120_000,
      {
        timing: PLAYING,
      },
    );
    const lags = lagsFrom(run, 20_000).sort((a, b) => a - b);
    expect(lags[Math.floor(lags.length * 0.95)] as number).toBeLessThan(0.1);
  });

  test("switching from PLAYING to WATCHING eases to a watcher's lag, without a step", () => {
    const run = live(() => 1000, 60_000, {
      timing: PLAYING,
      switchTo: { at: 30_000, timing: WATCHING },
    });
    for (let index = 1; index < run.speeds.length; index++) {
      const change = Math.abs(
        (run.speeds[index] as number) - (run.speeds[index - 1] as number),
      );
      expect(change).toBeLessThan(0.05);
    }
    for (const lag of lagsFrom(run, 50_000)) {
      expect(Math.abs(lag - WATCHING.bufferTurns)).toBeLessThan(0.1);
    }
  });

  test("after a hidden tab, switching to WATCHING keeps no more than the buffer behind", () => {
    // Hidden for 6 s while playing by hand; the team is out 20 s later.
    const run = live(() => 1000, 80_000, {
      timing: PLAYING,
      hidden: [20_000, 26_000],
      switchTo: { at: 40_000, timing: WATCHING },
    });
    expect(run.player.liveTiming).toBe(WATCHING);
    for (const lag of lagsFrom(run, 60_000)) {
      expect(Math.abs(lag - WATCHING.bufferTurns)).toBeLessThan(0.1);
    }
  });

  test("switching from WATCHING to PLAYING catches up without a step", () => {
    const run = live(() => 1000, 60_000, {
      switchTo: { at: 30_000, timing: PLAYING },
    });
    for (let index = 1; index < run.speeds.length; index++) {
      const change = Math.abs(
        (run.speeds[index] as number) - (run.speeds[index - 1] as number),
      );
      expect(change).toBeLessThan(0.05);
    }
    for (const lag of lagsFrom(run, 40_000)) expect(lag).toBeLessThan(0.05);
  });
});

describe("a watcher's lag (#361)", () => {
  const buffer = WATCHING.bufferTurns;

  /** The fastest the board moved from `from` ms on, as a multiple of the pace. */
  const fastest = (run: ReturnType<typeof live>, from: number, pace: number) =>
    Math.max(
      ...run.rates
        .filter((entry) => entry.at >= from)
        .map((entry) => (entry.rate * pace) / 1000),
    );

  /** The largest change of the catch-up speed from one frame to the next. */
  const largestStep = (run: ReturnType<typeof live>) => {
    let largest = 0;
    for (let index = 1; index < run.speeds.length; index++) {
      largest = Math.max(
        largest,
        Math.abs(
          (run.speeds[index] as number) - (run.speeds[index - 1] as number),
        ),
      );
    }
    return largest;
  };

  /** The mean of the lags between `from` and `to` ms: the steady lag before something happened. */
  const steadyLag = (
    run: ReturnType<typeof live>,
    from: number,
    to: number,
  ) => {
    const lags = run.lags
      .filter((entry) => entry.at >= from && entry.at < to)
      .map((entry) => entry.lag);
    return lags.reduce((sum, lag) => sum + lag, 0) / lags.length;
  };

  for (const pace of [500, 1000, 2000]) {
    test(`at a ${pace} ms pace the lag is the buffer`, () => {
      const run = live(() => pace, 20_000 + 100 * pace);
      for (const lag of lagsFrom(run, 20_000)) {
        expect(Math.abs(lag - buffer)).toBeLessThan(0.1);
      }
    });

    test(`at a ${pace} ms pace, with ±40 ms of jitter, the lag is about the buffer`, () => {
      let state = 7;
      const jitter = () => {
        state = (state * 1664525 + 1013904223) % 2 ** 32;
        return state / 2 ** 32;
      };
      const run = live(() => pace + 80 * (jitter() - 0.5), 20_000 + 200 * pace);
      const lags = lagsFrom(run, 20_000);
      const mean = lags.reduce((sum, lag) => sum + lag, 0) / lags.length;
      expect(Math.abs(mean - buffer)).toBeLessThan(0.1);
      // Each turn is as early or late as its arrival, by up to 40 ms: the lag doesn't drift.
      const off = lags
        .map((lag) => Math.abs(lag - buffer))
        .sort((a, b) => a - b);
      expect(off[Math.floor(off.length * 0.95)] as number).toBeLessThan(
        0.1 + 40 / pace,
      );
    });
  }

  test("the first turns, which play longer, leave no lag", () => {
    // The turn's length starts at 1.5 s and eases down to the time between arrivals.
    const run = live(() => 500, 30_000);
    for (const lag of lagsFromTurn(run, 12)) {
      expect(Math.abs(lag - buffer)).toBeLessThan(0.1);
    }
  });

  for (const [pace, hiddenMs] of [
    [500, 4000],
    [1000, 6000],
    [2000, 10_000],
  ] as const) {
    // At twice the pace the board gains a second a second: the lag goes in about as long again
    // as the tab was hidden, and then the last of it eases out.
    test(`after a ${hiddenMs / 1000} s hidden tab at a ${pace} ms pace, the lag is given back within ${hiddenMs / 1000 + 2.5} s, never faster than 2x`, () => {
      const start = 20_000 + 0.3 * pace;
      const end = start + hiddenMs;
      const run = live(() => pace, end + 30 * pace, {
        hidden: [start, end],
      });
      const steady = steadyLag(run, 10_000, start);
      expect(Math.abs(steady - buffer)).toBeLessThan(0.1);
      // The tab comes back with every turn that arrived meanwhile waiting.
      const after = run.lags.filter((entry) => entry.at >= end);
      expect(after[0]?.lag as number).toBeGreaterThan(hiddenMs / pace - 1);
      for (const lag of lagsFrom(run, end + hiddenMs + 2500)) {
        expect(Math.abs(lag - steady)).toBeLessThan(0.1);
      }
      // No turn plays faster than twice the pace, and the speed changes gradually: no jump.
      expect(Math.max(...run.speeds)).toBeLessThanOrEqual(2);
      expect(fastest(run, end, pace)).toBeLessThan(2.05);
      expect(largestStep(run)).toBeLessThan(0.06);
    });
  }

  // A game that waits for its bots (no pace, 1 s gaps), and real-time games whose pace the view
  // gives the player.
  for (const [pace, given] of [
    [1000, false],
    [500, true],
    [1000, true],
    [2000, true],
  ] as const) {
    for (const turns of [5, 7, 9]) {
      test(`${turns} turns that arrive together after a ${turns}-turn stall at ${pace} ms${given ? " with the pace" : ""} play at no more than 2x the pace`, () => {
        // A reconnect: turn 20 comes `turns` turns late, and the next ones with it.
        const run = live(
          (index) =>
            index === 20
              ? turns * pace
              : index > 20 && index < 20 + turns
                ? 0
                : pace,
          (60 + turns) * pace,
          given ? { paceMs: pace } : {},
        );
        const from = 500 + (19 + turns) * pace;
        // The turns that arrived together don't make the catch-up faster than twice the pace.
        expect(fastest(run, from, pace)).toBeLessThan(2.1);
        expect(Math.max(...run.speeds)).toBeLessThanOrEqual(2);
        // At 1 s, the board doesn't overshoot into its buffer, and gives the lag back.
        if (pace === 1000) {
          for (const lag of lagsFrom(run, from + 100)) {
            expect(lag).toBeGreaterThan(buffer - 0.1);
          }
          for (const lag of lagsFromTurn(run, 20 + turns + 22)) {
            expect(lag).toBeLessThan(buffer + 0.15);
          }
        }
      });
    }
  }

  test("a hand player who reaches live with a turn queued doesn't replay at 4x once out", () => {
    // Played to the newest turn (one queued as following starts), then out while hidden 9.5 s.
    const stage = new PositionStage();
    let now = 0;
    const player = new TurnPlayer(stage, {
      now: () => now,
      liveTiming: PLAYING,
    });
    player.setLivePace(1000);
    player.seek(long[0] as Generation);
    player.play(long[0] as Generation, long[1] as Generation);
    player.setLive(true);
    let next = 1;
    const speeds: number[] = [];
    for (; now < 60_000; now += FRAME_MS) {
      if (now >= 500 + next * 1000) {
        player.play(long[next] as Generation, long[next + 1] as Generation);
        next++;
      }
      if (now >= 24_000 && player.liveTiming !== WATCHING) {
        player.setLiveTiming(WATCHING);
      }
      if (now >= 20_000 && now < 29_500) continue;
      player.tick(FRAME_MS);
      if (now >= 29_500) speeds.push(player.catchUpSpeed);
    }
    expect(Math.max(...speeds)).toBeGreaterThan(1.9);
    expect(Math.max(...speeds)).toBeLessThanOrEqual(2);
  });

  test("a turn 3 s late in a real-time game counts as half a turn more than usual", () => {
    // Turn 21 comes 3 s late at a 1 s pace; the view gives a watcher the game's pace.
    const paced = live((index) => (index === 20 ? 4000 : 1000), 60_000, {
      paceMs: 1000,
    });
    expect(Math.max(...lagsFromTurn(paced, 21))).toBeLessThan(0.85);
    for (const lag of lagsFromTurn(paced, 36)) {
      expect(Math.abs(lag - buffer)).toBeLessThan(0.1);
    }
    expect(Math.max(...paced.speeds)).toBeLessThanOrEqual(2);
    // Without a pace (a game that waits for its bots), the gap stretches the turns after it for
    // a while, and the spread of the gaps keeps a little more in hand; that too is given back.
    const unpaced = live((index) => (index === 20 ? 4000 : 1000), 70_000);
    const lags = lagsFromTurn(unpaced, 21);
    expect(Math.max(...lags)).toBeGreaterThan(
      Math.max(...lagsFromTurn(paced, 21)),
    );
    expect(Math.max(...lags)).toBeLessThan(2);
    for (const lag of lagsFromTurn(unpaced, 46)) {
      expect(Math.abs(lag - buffer)).toBeLessThan(0.1);
    }
    expect(Math.max(...unpaced.speeds)).toBeLessThanOrEqual(2);
  });

  test("a hand player who is out while the tab is hidden gets back to the buffer", () => {
    // Hidden for 9.5 turns while playing by hand; the team is out half-way through.
    const run = live(() => 1000, 60_000, {
      timing: PLAYING,
      paceMs: 1000,
      hidden: [20_000, 29_500],
      switchTo: { at: 24_000, timing: WATCHING },
    });
    const after = run.lags.filter((entry) => entry.at >= 29_500);
    expect(after[0]?.lag as number).toBeGreaterThan(9);
    for (const lag of lagsFrom(run, 29_500 + 9500 + 2500)) {
      expect(Math.abs(lag - buffer)).toBeLessThan(0.1);
    }
    expect(fastest(run, 29_500, 1000)).toBeLessThan(2.05);
  });

  for (const pace of [500, 1000]) {
    test(`at a ${pace} ms pace, Follow live ticked again replays the missed turns at up to 4x, then 2x`, () => {
      // Scrubbed back for 8 turns, then Follow live.
      const back = 8 * pace;
      const run = live(() => pace, 20_000 + back + 40 * pace, {
        replay: [20_000, 20_000 + back],
      });
      const start = 20_000 + back;
      const after = run.lags.filter((entry) => entry.at >= start);
      expect(after[0]?.lag as number).toBeGreaterThan(7);
      // Faster than a lag the board built itself, never in a jump: the replay doesn't fall further
      // behind the game, nor reach the ten turns that skip to the newest.
      expect(Math.max(...run.speeds)).toBeGreaterThan(2);
      expect(Math.max(...run.speeds)).toBeLessThanOrEqual(4);
      expect(fastest(run, start, pace)).toBeLessThan(6.1);
      expect(after[1]?.lag as number).toBeLessThan(after[0]?.lag as number);
      for (const lag of lagsFrom(run, start + 12 * pace)) {
        expect(Math.abs(lag - buffer)).toBeLessThan(0.1);
      }
      // Once the board is back, a lag it builds (a hidden tab) is given back at 2x again.
      const hidden = live(() => pace, start + 40 * pace, {
        replay: [20_000, start],
        hidden: [start + 20 * pace, start + 26 * pace],
      });
      expect(
        Math.max(
          ...hidden.speeds.slice(Math.ceil((start + 20 * pace) / FRAME_MS)),
        ),
      ).toBeLessThanOrEqual(2);
    });
  }
});

describe("a hand player's timing with the game's pace (#345)", () => {
  /** After warming up: the lags, and the stops per 100 turns and the longest, in frames. */
  const settled = (run: ReturnType<typeof live>) => {
    const lags = lagsFrom(run, 20_000);
    const stops = stopsFrom(run, 20_000);
    return {
      lags,
      maxLag: Math.max(...lags),
      stopsPer100: (stops.length / lags.length) * 100,
      longestStop: Math.max(0, ...stops),
    };
  };

  for (const pace of [500, 1000]) {
    const playing = { timing: PLAYING, paceMs: pace };

    // A server rarely keeps its pace exactly: the turn follows the time between arrivals.
    for (const ratio of [1, 1.04, 1.3, 0.7, 2]) {
      test(`at a ${pace} ms pace, turns ${ratio}x the pace apart keep up without stopping each turn`, () => {
        const run = settled(live(() => ratio * pace, 300 * pace, playing));
        expect(run.maxLag).toBeLessThan(0.1);
        expect(run.stopsPer100).toBeLessThanOrEqual(6);
        expect(run.longestStop).toBeLessThanOrEqual(2);
      });
    }

    test(`at a ${pace} ms pace, with ±40 ms of jitter, the stops are short`, () => {
      let state = 7;
      const jitter = () => {
        state = (state * 1664525 + 1013904223) % 2 ** 32;
        return state / 2 ** 32;
      };
      const run = settled(
        live(() => 1.02 * pace + 80 * (jitter() - 0.5), 200 * pace, playing),
      );
      const sorted = [...run.lags].sort((a, b) => a - b);
      expect(sorted[Math.floor(sorted.length * 0.95)] as number).toBeLessThan(
        0.15,
      );
      expect(run.stopsPer100).toBeLessThan(25);
      // A stop is a turn arriving late, as long as it is late: no more than a few frames.
      expect(run.longestStop).toBeLessThanOrEqual(4);
    });

    for (const late of [2000, 4000]) {
      test(`at a ${pace} ms pace, a turn ${late} ms late leaves a small lag that goes`, () => {
        // Turn 20 comes late; the server keeps its pace from there.
        const run = live(
          (index) => (index === 20 ? pace + late : pace),
          60 * pace + late,
          playing,
        );
        const lags = run.lags.filter((entry) => entry.index >= 20);
        expect(Math.max(...lags.map((entry) => entry.lag))).toBeLessThan(0.25);
        const later = lags.filter((entry) => entry.index >= 30);
        expect(later.length).toBeGreaterThan(10);
        for (const { lag } of later) expect(lag).toBeLessThan(0.1);
      });
    }

    test(`at a ${pace} ms pace, the first turns and the turns after a jump are on time`, () => {
      const run = live(() => pace, 60 * pace, {
        ...playing,
        jumpAt: 30 * pace + 250,
      });
      // From the second turn of the game on, including the turns right after the jump.
      for (const lag of lagsFromTurn(run, 2)) expect(lag).toBeLessThan(0.1);
    });

    test(`at a ${pace} ms pace, five turns that arrive together are caught up within three turns`, () => {
      const run = live(
        (index) =>
          index === 20 ? 5 * pace : index > 20 && index <= 24 ? 0 : pace,
        60 * pace,
        playing,
      );
      for (const lag of lagsFromTurn(run, 28)) expect(lag).toBeLessThan(0.1);
    });
  }

  test("without the pace, a late turn stretches the turns after it (why the pace is passed)", () => {
    const run = live((index) => (index === 20 ? 3000 : 1000), 50_000, {
      timing: PLAYING,
    });
    expect(Math.max(...lagsFromTurn(run, 23))).toBeGreaterThan(0.3);
  });

  test("the pace given at every arrival or once gives the same board", () => {
    for (const ratio of [1.04, 1.3, 0.7]) {
      const every = live(() => ratio * 1000, 120_000, {
        timing: PLAYING,
        paceMs: 1000,
      });
      const once = live(() => ratio * 1000, 120_000, {
        timing: PLAYING,
        paceMs: 1000,
        paceOnce: true,
      });
      expect(every.trace).toEqual(once.trace);
    }
  });

  test("with steady turns, the pace changes nothing for a watcher", () => {
    seed = 1;
    const { trace } = live(() => 1000, 30_000, { paceMs: 1000 });
    for (const [index, position] of trace.entries()) {
      expect(position).toBeCloseTo(watchingTrace.steady[index] as number, 9);
    }
  });
});
