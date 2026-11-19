// The line of feedback on a hand player's orders (#345): `HandSurface.showOrderFeedback`. No DOM:
// it renders through a callback, on an injected timer, so `bun test` runs it on a fake clock.
//
// The newest sentence shows for `ORDER_FEEDBACK_MS`, each call restarting the time. From the
// second order refused within that time, a count follows the sentence, "(3 orders refused)", which
// also makes a screen reader announce a sentence that repeats. One order is counted once: the drag
// or the menu that gave it and hand play's `onSettled` may both report it, with its id.

/** How long one piece of feedback shows, in milliseconds. */
export const ORDER_FEEDBACK_MS = 5000;

export interface OrderNoticeDeps {
  /** Shows `text` on the line, or hides the line for null. */
  render(text: string | null): void;
  /** Runs `run` after `ms` and returns the function that cancels it. */
  schedule(run: () => void, ms: number): () => void;
}

export interface OrderNotices {
  /** Shows a sentence about one order; `orderId` is its `PlayOrder.id` when it has one. */
  show(sentence: string, orderId?: number): void;
  /** Hides the line and cancels the timer. */
  dispose(): void;
}

/** The line's text for the newest sentence and the orders refused within the time. */
export function orderNoticeText(sentence: string, refused: number): string {
  return refused > 1 ? `${sentence} (${refused} orders refused)` : sentence;
}

export function createOrderNotices(deps: OrderNoticeDeps): OrderNotices {
  let cancel: (() => void) | undefined;
  /** The orders reported within the time, by id. */
  let ids = new Set<number>();
  let refused = 0;
  let newest: string | null = null;

  function clear(): void {
    cancel?.();
    cancel = undefined;
    ids = new Set();
    refused = 0;
    newest = null;
  }

  return {
    show(sentence, orderId) {
      if (orderId !== undefined) {
        // Reported already (by the control that gave it, or by hand play): shown once.
        if (ids.has(orderId)) return;
        ids.add(orderId);
        refused += 1;
      } else if (sentence !== newest || refused === 0) {
        refused += 1;
      }
      newest = sentence;
      cancel?.();
      cancel = deps.schedule(() => {
        clear();
        deps.render(null);
      }, ORDER_FEEDBACK_MS);
      deps.render(orderNoticeText(sentence, refused));
    },
    dispose() {
      clear();
      deps.render(null);
    },
  };
}
