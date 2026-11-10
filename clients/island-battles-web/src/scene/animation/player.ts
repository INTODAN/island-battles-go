// Plays turns one after another and catches up when they pile up. No Babylon imports: the
// player talks to a `Stage`, and tests drive it with a fake stage and a fake clock.

import type { components } from "../../api/generated/game";
import { planTurn, sampleTurn, type TurnPlan, type TurnSample } from "./plan";

type Generation = components["schemas"]["Generation"];

/** What the player draws onto. */
export interface Stage {
  /** Shows a generation as it is, with nothing half-way and nothing animating. */
  settle(generation: Generation): void;
  /** A turn is about to play; the stage is showing `plan.previous`. */
  begin(plan: TurnPlan): void;
  /** Shows the turn at `sample.t`. */
  apply(sample: TurnSample, plan: TurnPlan): void;
  /** The turn is over; the stage must show `plan.next`. */
  end(plan: TurnPlan): void;
}

export interface PlayerOptions {
  /** How long one turn takes at 1x when not following live, in milliseconds. */
  turnMs?: number;
  /** The fastest the catch-up makes turns play, as a multiple of the pace. */
  maxCatchUpSpeed?: number;
  /** More turns than this waiting, and the player skips to the newest. */
  maxBehind?: number;
  /** The most time one frame may advance the player by, in milliseconds. */
  maxFrameMs?: number;
  /** The clock that times the arrival of live turns, in milliseconds. */
  now?: () => number;
  /** Called when a turn has finished playing, or a generation was settled. */
  onShown?: (generation: Generation) => void;
  /**
   * Called when a turn has finished and nothing waits. A turn queued from here plays on in the
   * same frame, with the frame's time that is left, so it follows without a pause.
   */
  onIdle?: () => void;
}

export const DEFAULT_TURN_MS = 1500;
export const DEFAULT_MAX_CATCH_UP_SPEED = 4;
export const DEFAULT_MAX_BEHIND = 10;
/** A frame advances the player by at most this, so a tab coming back to front does not jump. */
export const DEFAULT_MAX_FRAME_MS = 100;
/** Following live, a turn plays over the time between arriving turns, within these limits. */
export const MIN_LIVE_TURN_MS = 250;
export const MAX_LIVE_TURN_MS = 3000;
/** How much of a turn is still to play, following live, when the next one should arrive. */
export const LIVE_BUFFER_TURNS = 0.5;
/** How quickly the catch-up speed moves to its target: the time constant, in milliseconds. */
export const CATCH_UP_EASE_MS = 300;
/** How quickly a live turn's length moves to the time between arrivals. */
export const LIVE_PACE_EASE_MS = 600;
/** The weight of the newest gap between arrivals in their running average. */
export const ARRIVAL_WEIGHT = 0.25;
/**
 * Turns that arrive closer together than this, or than BURST_SHARE of the time between turns,
 * arrive together, in a burst: a game whose bots are fast and whose turns wait for something
 * else (another seat, the network) delivers them in bursts.
 */
export const BURST_MS = 30;
export const BURST_SHARE = 0.25;
/** The most turns one burst counts; more close together are a fast game, not a burst. */
export const MAX_BURST = 4;
/** Catch-up speed per turn of backlog beyond the buffer. */
const CATCH_UP_GAIN = 0.5;
/** Following live, the slowest the catch-up plays to let a buffer build up. */
const MIN_CATCH_UP_SPEED = 0.8;

interface Turn {
  previous: Generation;
  next: Generation;
}

interface Running {
  plan: TurnPlan;
  /** 0 to 1 of the turn. */
  progress: number;
}

/** Moves `value` towards `target` as an exponential ease with time constant `timeMs`. */
function ease(
  value: number,
  target: number,
  deltaMs: number,
  timeMs: number,
): number {
  return value + (target - value) * (1 - Math.exp(-deltaMs / timeMs));
}

function clamp(value: number, low: number, high: number): number {
  return Math.min(high, Math.max(low, value));
}

/**
 * Plays turns at one of two paces. Stepping through a saved or finished game, a turn takes
 * `turnMs` times the user's speed. Following a live game (`setLive(true)`), a turn takes about
 * the time between arriving turns, and the speed changes gradually to keep about half a turn in
 * hand, so boats sail on at a steady speed without waiting at the end of each turn.
 */
export class TurnPlayer {
  private readonly turnMs: number;
  private readonly maxCatchUpSpeed: number;
  private readonly maxBehind: number;
  private readonly maxFrameMs: number;
  private readonly now: () => number;
  private readonly onShown: ((generation: Generation) => void) | undefined;
  private readonly onIdle: (() => void) | undefined;
  private queue: Turn[] = [];
  private running: Running | null = null;
  /** The generation the stage shows when no turn is running, or null when it is unknown. */
  private shown: Generation | null = null;
  private userSpeed = 1;
  private live = false;
  /** The catch-up multiple now; it eases towards the target. */
  private catchUp = 1;
  /** How many turns arrived in the burst that started at `burstStart`. */
  private burst = 1;
  /** When the first turn of the last burst arrived, by `now`. */
  private burstStart: number | null = null;
  /** When the last live turn arrived, by `now`. */
  private lastArrival: number | null = null;
  /** The running average of the time between live turns. */
  private arrivalGap: number | null = null;
  /** How long a live turn takes now; it eases towards the time between arrivals. */
  private liveTurnMs: number;
  /**
   * Following live, the turns that would still be waiting had every turn played at the pace
   * since it arrived: one more at each arrival, one fewer per turn's length of time.
   */
  private paced = 0;

  constructor(
    private readonly stage: Stage,
    options: PlayerOptions = {},
  ) {
    this.turnMs = options.turnMs ?? DEFAULT_TURN_MS;
    this.maxCatchUpSpeed =
      options.maxCatchUpSpeed ?? DEFAULT_MAX_CATCH_UP_SPEED;
    this.maxBehind = options.maxBehind ?? DEFAULT_MAX_BEHIND;
    this.maxFrameMs = options.maxFrameMs ?? DEFAULT_MAX_FRAME_MS;
    this.now = options.now ?? (() => performance.now());
    this.onShown = options.onShown;
    this.onIdle = options.onIdle;
    this.liveTurnMs = clamp(this.turnMs, MIN_LIVE_TURN_MS, MAX_LIVE_TURN_MS);
  }

  /**
   * Queues the turn from `previous` to `next`. It plays when the turns before it are done; when
   * more than `maxBehind` turns wait, the player skips to the newest. Following live, the time
   * it arrived sets the pace.
   */
  play(previous: Generation, next: Generation): void {
    if (this.live) this.arrived();
    this.queue.push({ previous, next });
    if (this.queue.length > this.maxBehind) {
      const newest = this.queue[this.queue.length - 1] as Turn;
      this.queue = [newest];
      this.running = null;
      this.shown = null;
      this.paced = Math.min(this.paced, 1);
    }
  }

  /** Shows a generation at once, drops everything queued and ends any turn half-way. */
  seek(generation: Generation): void {
    this.queue = [];
    this.running = null;
    this.catchUp = 1;
    this.stage.settle(generation);
    this.shown = generation;
    this.onShown?.(generation);
  }

  /**
   * Shows a turn frozen at `progress`, for stills. Everything queued is dropped; `seek` or
   * `play` carry on from there.
   */
  scrub(previous: Generation, next: Generation, progress: number): void {
    this.queue = [];
    this.stage.settle(previous);
    const plan = planTurn(previous, next);
    this.stage.begin(plan);
    this.stage.apply(sampleTurn(plan, progress), plan);
    this.running = null;
    this.shown = null;
  }

  /** Scales the pace when not following live; 0 pauses either way. */
  setSpeed(speed: number): void {
    this.userSpeed = Math.max(0, speed);
  }

  get speed(): number {
    return this.userSpeed;
  }

  /**
   * Following a live game: the pace adapts to how fast turns arrive. Off, a turn takes
   * `turnMs` at the user's speed.
   */
  setLive(live: boolean): void {
    if (live === this.live) return;
    this.live = live;
    this.lastArrival = null;
    this.burstStart = null;
    this.burst = 1;
    this.arrivalGap = null;
    this.paced = 0;
    this.liveTurnMs = clamp(this.turnMs, MIN_LIVE_TURN_MS, MAX_LIVE_TURN_MS);
  }

  get following(): boolean {
    return this.live;
  }

  /** Turns waiting behind the one playing. */
  get waiting(): number {
    return this.queue.length;
  }

  get busy(): boolean {
    return this.running !== null || this.queue.length > 0;
  }

  /** How long a turn takes at the pace now, before any catch-up, in milliseconds. */
  get turnDuration(): number {
    return this.live ? this.liveTurnMs : this.turnMs;
  }

  /** The catch-up multiple now: 1 at the pace, more while catching up. */
  get catchUpSpeed(): number {
    return this.catchUp;
  }

  /** How fast turns play now, as a multiple of `turnMs` at 1x. */
  get effectiveSpeed(): number {
    if (this.userSpeed <= 0) return 0;
    if (this.live) return (this.catchUp * this.turnMs) / this.liveTurnMs;
    return this.userSpeed * this.catchUp;
  }

  /** The progress of the turn playing, 0 to 1, or null when none plays. */
  get progress(): number | null {
    return this.running ? this.running.progress : null;
  }

  /**
   * Advances by `deltaMs` of real time, at most `maxFrameMs`. Call it once a frame. The time
   * left over when a turn ends carries into the next one.
   */
  tick(deltaMs: number): void {
    let left = clamp(deltaMs, 0, this.maxFrameMs);
    this.pace(left);
    for (let guard = 0; guard < 1000; guard++) {
      const speed = this.effectiveSpeed;
      if (speed <= 0) return;
      if (!this.running && !this.start()) return;
      const running = this.running;
      if (!running) return;
      const need = ((1 - running.progress) * this.turnMs) / speed;
      if (left < need) {
        running.progress += (left * speed) / this.turnMs;
        this.stage.apply(
          sampleTurn(running.plan, running.progress),
          running.plan,
        );
        return;
      }
      left -= need;
      this.finish(running);
      if (this.queue.length === 0) this.onIdle?.();
    }
  }

  /**
   * Records a live turn's arrival, for the time between turns. Turns that arrive in a burst
   * share the time from the burst's first turn to the next burst's.
   */
  private arrived(): void {
    const now = this.now();
    this.paced += 1;
    const close = Math.max(
      BURST_MS,
      BURST_SHARE * (this.arrivalGap ?? this.turnMs),
    );
    if (
      this.lastArrival !== null &&
      now - this.lastArrival < close &&
      this.burst < MAX_BURST
    ) {
      this.lastArrival = now;
      this.burst += 1;
      return;
    }
    if (this.burstStart !== null) {
      // A long wait (a paused server, a tab in the background) counts as a slow turn, no more.
      const gap = Math.min(
        (now - this.burstStart) / this.burst,
        2 * MAX_LIVE_TURN_MS,
      );
      const weight = 1 - (1 - ARRIVAL_WEIGHT) ** this.burst;
      this.arrivalGap =
        this.arrivalGap === null
          ? gap
          : this.arrivalGap + weight * (gap - this.arrivalGap);
    }
    this.burstStart = now;
    this.lastArrival = now;
    this.burst = 1;
  }

  /** The catch-up speed to aim for now. */
  private catchUpTarget(): number {
    const backlog =
      this.queue.length + (this.running ? 1 - this.running.progress : 0);
    if (!this.live) {
      // Stepping through a game nothing waits behind the turn that plays (or is about to);
      // a queue that builds up plays faster, as much as one turn more a turn waiting.
      const behind = this.running
        ? this.queue.length
        : Math.max(0, this.queue.length - 1);
      return Math.min(this.maxCatchUpSpeed, 1 + behind);
    }
    // Following live, a turn should play over about the time until the next one arrives, with
    // LIVE_BUFFER_TURNS in hand: a burst of turns plays at the pace over the time it covers, and
    // only a backlog beyond that is caught up.
    const expected = this.paced + LIVE_BUFFER_TURNS;
    const slowest = Math.max(
      MIN_CATCH_UP_SPEED,
      this.liveTurnMs / MAX_LIVE_TURN_MS,
    );
    return clamp(
      1 + CATCH_UP_GAIN * (backlog - expected),
      slowest,
      this.maxCatchUpSpeed,
    );
  }

  /** Eases the turn length and the catch-up speed by `deltaMs`. */
  private pace(deltaMs: number): void {
    if (this.live) {
      const target =
        this.arrivalGap === null
          ? this.turnMs
          : clamp(this.arrivalGap, MIN_LIVE_TURN_MS, MAX_LIVE_TURN_MS);
      this.liveTurnMs = clamp(
        ease(this.liveTurnMs, target, deltaMs, LIVE_PACE_EASE_MS),
        MIN_LIVE_TURN_MS,
        MAX_LIVE_TURN_MS,
      );
      this.paced = Math.max(0, this.paced - deltaMs / this.liveTurnMs);
    }
    this.catchUp = ease(
      this.catchUp,
      this.catchUpTarget(),
      deltaMs,
      CATCH_UP_EASE_MS,
    );
  }

  private start(): boolean {
    const turn = this.queue.shift();
    if (!turn) return false;
    if (this.shown?.id !== turn.previous.id) {
      this.stage.settle(turn.previous);
      this.shown = turn.previous;
    }
    const plan = planTurn(turn.previous, turn.next);
    this.stage.begin(plan);
    this.stage.apply(sampleTurn(plan, 0), plan);
    this.running = { plan, progress: 0 };
    return true;
  }

  private finish(running: Running): void {
    this.stage.apply(sampleTurn(running.plan, 1), running.plan);
    this.stage.end(running.plan);
    this.running = null;
    this.shown = running.plan.next;
    this.onShown?.(running.plan.next);
  }
}
