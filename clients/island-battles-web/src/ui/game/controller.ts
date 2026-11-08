// What the game view shows and does: the history, the turn shown, play/pause and Follow live.
// No DOM and no Babylon: it drives a `PlayerPort` (the scene's turn player in the page, a fake
// in tests).

import { type GameOutcome, type Generation, mergeGenerations } from "./history";

/** The part of the scene's turn player the controller uses. */
export interface PlayerPort {
  /** Queues the animated turn from `previous` to `next`. */
  play(previous: Generation, next: Generation): void;
  /** Shows a generation at once, dropping anything queued. */
  seek(generation: Generation): void;
  /** True while a turn plays or waits. */
  readonly busy: boolean;
}

export interface ViewState {
  frames: readonly Generation[];
  /** Index of the turn shown (the newest one queued while turns play), or -1 before a load. */
  index: number;
  /** Shows each new turn as it arrives. Off when the user scrubs back. */
  follow: boolean;
  playing: boolean;
  outcome: GameOutcome | null;
  /** Whether new turns can arrive: false for a demo file. */
  live: boolean;
}

export interface ControllerOptions {
  live: boolean;
}

export class GameController {
  private frames: readonly Generation[] = [];
  private shownId: number | null = null;
  private follow = false;
  private playing = false;
  private outcome: GameOutcome | null = null;
  private readonly live: boolean;
  private readonly listeners = new Set<(state: ViewState) => void>();

  constructor(
    private readonly player: PlayerPort,
    options: ControllerOptions,
  ) {
    this.live = options.live;
  }

  get state(): ViewState {
    return {
      frames: this.frames,
      index: this.indexOfShown(),
      follow: this.follow,
      playing: this.playing,
      outcome: this.outcome,
      live: this.live,
    };
  }

  subscribe(listener: (state: ViewState) => void): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  /** Follow live makes sense only while the game runs and this view receives its turns. */
  private get followable(): boolean {
    return this.live && this.outcome === null;
  }

  /**
   * Loads the history for the first time. A live game opens on its newest turn and follows; a
   * saved game opens on its first turn, paused.
   */
  load(frames: readonly Generation[], outcome: GameOutcome | null): void {
    this.frames = mergeGenerations([], frames);
    this.outcome = outcome;
    const first = this.live ? this.frames.at(-1) : this.frames[0];
    this.shownId = first?.id ?? null;
    this.follow = this.followable;
    this.playing = false;
    if (first) this.player.seek(first);
    this.emit();
  }

  /**
   * Merges turns that arrived live or came from a re-read of the history, and the outcome when
   * the game ended. Turns already held are ignored. While following, the new turns play on.
   */
  merge(frames: readonly Generation[], outcome?: GameOutcome | null): void {
    const merged = mergeGenerations(this.frames, frames);
    let changed = merged !== this.frames;
    this.frames = merged;
    // The last turn plays before the end stops following.
    if (this.follow) this.playToNewest();
    if (outcome && this.outcome === null) {
      this.outcome = outcome;
      this.follow = false;
      changed = true;
    }
    if (changed) this.emit();
  }

  /** Jumps to a turn with no animation. Going to the newest turn of a running game follows live. */
  scrubTo(index: number): void {
    const target = this.frames[this.clamp(index)];
    if (!target) return;
    this.playing = false;
    this.player.seek(target);
    this.shownId = target.id;
    this.follow = this.followable && this.atNewest();
    this.emit();
  }

  /** One turn back is a jump; one turn forward plays that turn. */
  step(delta: -1 | 1): void {
    const index = this.indexOfShown();
    if (delta < 0) {
      this.scrubTo(index - 1);
      return;
    }
    this.playing = false;
    if (this.player.busy) {
      // Finish what is queued first by jumping to where it ends, then step on.
      const queued = this.frames[index];
      if (queued) this.player.seek(queued);
    }
    this.advance();
    this.emit();
  }

  /** Play from the turn shown; at the end of a finished game, play again from the first turn. */
  togglePlay(): void {
    if (this.playing || this.follow) {
      this.pause();
      return;
    }
    if (this.atNewest()) {
      if (this.followable) {
        this.follow = true;
        this.emit();
        return;
      }
      const first = this.frames[0];
      if (!first) return;
      this.player.seek(first);
      this.shownId = first.id;
    }
    this.playing = true;
    this.emit();
  }

  /** Follow live on or off. On jumps to the newest turn, playing the turns in between. */
  setFollow(on: boolean): void {
    if (on) {
      if (!this.followable) return;
      this.playing = false;
      this.follow = true;
      this.playToNewest();
    } else {
      this.follow = false;
    }
    this.emit();
  }

  /** Call once a frame: while playing, queues the next turn when the last one has finished. */
  tick(): void {
    if (!this.playing || this.player.busy) return;
    if (!this.advance()) {
      this.playing = false;
      this.emit();
      return;
    }
    if (this.atNewest()) {
      this.playing = false;
      if (this.followable) this.follow = true;
    }
    this.emit();
  }

  private pause(): void {
    const wasFollowing = this.follow;
    this.playing = false;
    this.follow = false;
    if (wasFollowing && this.player.busy) {
      // Stop on the turn shown instead of finishing a queue of turns.
      const shown = this.frames[this.indexOfShown()];
      if (shown) this.player.seek(shown);
    }
    this.emit();
  }

  /** Queues every turn after the one shown, in order. The player catches up when many wait. */
  private playToNewest(): void {
    let index = this.indexOfShown();
    while (index < this.frames.length - 1) {
      this.queue(index);
      index++;
    }
  }

  /** Plays the turn after the shown one. False when there is none. */
  private advance(): boolean {
    const index = this.indexOfShown();
    if (index >= this.frames.length - 1) return false;
    this.queue(index);
    if (this.followable && this.atNewest()) this.follow = true;
    return true;
  }

  private queue(fromIndex: number): void {
    const previous = this.frames[fromIndex];
    const next = this.frames[fromIndex + 1];
    if (!previous || !next) return;
    this.player.play(previous, next);
    this.shownId = next.id;
  }

  private atNewest(): boolean {
    return this.indexOfShown() === this.frames.length - 1;
  }

  private indexOfShown(): number {
    if (this.shownId === null) return -1;
    return this.frames.findIndex((frame) => frame.id === this.shownId);
  }

  private clamp(index: number): number {
    return Math.min(this.frames.length - 1, Math.max(0, index));
  }

  private emit(): void {
    const state = this.state;
    for (const listener of [...this.listeners]) listener(state);
  }
}
