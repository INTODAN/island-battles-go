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
  /** How long one turn takes at 1x, in milliseconds. */
  turnMs?: number;
  /** The fastest the queue makes turns play, as a multiple of the normal pace. */
  maxCatchUpSpeed?: number;
  /** More turns than this waiting, and the player skips to the newest. */
  maxBehind?: number;
  /** Called when a turn has finished playing, or a generation was settled. */
  onShown?: (generation: Generation) => void;
}

export const DEFAULT_TURN_MS = 1500;
export const DEFAULT_MAX_CATCH_UP_SPEED = 4;
export const DEFAULT_MAX_BEHIND = 3;

interface Turn {
  previous: Generation;
  next: Generation;
}

interface Running {
  plan: TurnPlan;
  /** 0 to 1 of the turn. */
  progress: number;
}

export class TurnPlayer {
  private readonly turnMs: number;
  private readonly maxCatchUpSpeed: number;
  private readonly maxBehind: number;
  private readonly onShown: ((generation: Generation) => void) | undefined;
  private queue: Turn[] = [];
  private running: Running | null = null;
  /** The generation the stage shows when no turn is running, or null when it is unknown. */
  private shown: Generation | null = null;
  private userSpeed = 1;

  constructor(
    private readonly stage: Stage,
    options: PlayerOptions = {},
  ) {
    this.turnMs = options.turnMs ?? DEFAULT_TURN_MS;
    this.maxCatchUpSpeed =
      options.maxCatchUpSpeed ?? DEFAULT_MAX_CATCH_UP_SPEED;
    this.maxBehind = options.maxBehind ?? DEFAULT_MAX_BEHIND;
    this.onShown = options.onShown;
  }

  /**
   * Queues the turn from `previous` to `next`. It plays when the turns before it are done; when
   * more than `maxBehind` turns wait, the player skips to the newest.
   */
  play(previous: Generation, next: Generation): void {
    this.queue.push({ previous, next });
    if (this.queue.length > this.maxBehind) {
      const newest = this.queue[this.queue.length - 1] as Turn;
      this.queue = [newest];
      this.running = null;
      this.shown = null;
    }
  }

  /** Shows a generation at once, drops everything queued and ends any turn half-way. */
  seek(generation: Generation): void {
    this.queue = [];
    this.running = null;
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

  /** Scales the pace; 0 pauses. The queue's catch-up speed multiplies it. */
  setSpeed(speed: number): void {
    this.userSpeed = Math.max(0, speed);
  }

  get speed(): number {
    return this.userSpeed;
  }

  /** Turns waiting behind the one playing. */
  get waiting(): number {
    return this.queue.length;
  }

  get busy(): boolean {
    return this.running !== null || this.queue.length > 0;
  }

  /** How fast turns play now: the user's speed times about 1x with nothing waiting, up to 4x. */
  get effectiveSpeed(): number {
    const catchUp = Math.min(this.maxCatchUpSpeed, 1 + this.queue.length);
    return this.userSpeed * catchUp;
  }

  /** The progress of the turn playing, 0 to 1, or null when none plays. */
  get progress(): number | null {
    return this.running ? this.running.progress : null;
  }

  /** Advances by `deltaMs` of real time. Call it once a frame. */
  tick(deltaMs: number): void {
    let left = Math.max(0, deltaMs);
    for (let guard = 0; guard < 1000; guard++) {
      if (this.effectiveSpeed <= 0) return;
      if (!this.running && !this.start()) return;
      const running = this.running;
      if (!running) return;
      const speed = this.effectiveSpeed;
      if (speed <= 0) return;
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
    }
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
