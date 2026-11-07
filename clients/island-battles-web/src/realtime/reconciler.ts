export interface ReconcilerOptions {
  /** Runs one reconciling read. A rejection is passed to `retryable`. */
  run(): Promise<void>;
  /** Whether a failed read is worth retrying; false stops quietly. */
  retryable(error: unknown): boolean;
  /** Requests that arrive within this window share one read. */
  settleMs: number;
  retryBaseMs: number;
  retryMaxMs: number;
}

/**
 * Runs a reconciling read when asked: requests close together share one read,
 * a request during a read runs one more read after it, and a failed read is
 * retried with exponential backoff. It never reads on its own otherwise.
 */
export class Reconciler {
  private settleTimer: ReturnType<typeof setTimeout> | undefined;
  private retryTimer: ReturnType<typeof setTimeout> | undefined;
  private running = false;
  private again = false;
  private failures = 0;
  private cancelled = false;

  constructor(private readonly options: ReconcilerOptions) {}

  request(): void {
    if (this.cancelled) return;
    if (this.running) {
      this.again = true;
      return;
    }
    // A new request replaces a pending retry: it reads right away anyway.
    clearTimeout(this.retryTimer);
    this.retryTimer = undefined;
    if (this.settleTimer !== undefined) return;
    this.settleTimer = setTimeout(() => {
      this.settleTimer = undefined;
      void this.start();
    }, this.options.settleMs);
  }

  cancel(): void {
    this.cancelled = true;
    clearTimeout(this.settleTimer);
    clearTimeout(this.retryTimer);
    this.settleTimer = undefined;
    this.retryTimer = undefined;
  }

  private async start(): Promise<void> {
    if (this.cancelled) return;
    this.running = true;
    this.again = false;
    let failed = false;
    try {
      await this.options.run();
      this.failures = 0;
    } catch (error) {
      if (this.options.retryable(error)) {
        failed = true;
        this.failures++;
      } else {
        this.failures = 0;
        this.again = false;
      }
    } finally {
      this.running = false;
    }
    if (this.cancelled) return;
    if (failed) {
      this.again = false;
      const delay = Math.min(
        this.options.retryBaseMs * 2 ** (this.failures - 1),
        this.options.retryMaxMs,
      );
      this.retryTimer = setTimeout(() => {
        this.retryTimer = undefined;
        void this.start();
      }, delay);
    } else if (this.again) {
      this.request();
    }
  }
}
