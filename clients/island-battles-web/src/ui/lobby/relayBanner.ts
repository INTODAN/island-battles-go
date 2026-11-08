import type { BotRelay, RelayStatus } from "../../bots";
import { h } from "../dom";

/**
 * A small notice over the game view for a player with a localhost bot: it says when
 * the bot cannot be reached or failed, and offers a retry when the relay does not retry itself.
 */
export function mountRelayBanner(relay: BotRelay): {
  el: HTMLElement;
  dispose(): void;
} {
  const text = h("span", { class: "relay-text" });
  const retry = h(
    "button",
    { type: "button", class: "secondary small" },
    "Try again",
  );
  retry.addEventListener("click", () => relay.retry());
  const el = h(
    "div",
    { class: "relay-banner", hidden: true, attrs: { role: "status" } },
    text,
    retry,
  );

  function show(status: RelayStatus): void {
    if (status.kind !== "error") {
      el.hidden = true;
      return;
    }
    el.hidden = false;
    text.textContent = status.retrying
      ? `${status.message} Trying again…`
      : status.message;
    retry.hidden = status.retrying;
  }
  show(relay.status.get());
  const unsubscribe = relay.status.subscribe(show);
  return { el, dispose: unsubscribe };
}
