import type { BotRelay, BotRelays, RelayStatus } from "../../bots";
import { h } from "../dom";

/** One relay that the banner reports on: the player's own bot's, or a seat's, with its name. */
interface Reported {
  relay: BotRelay;
  /** Names the seat in its line, such as "Localhost bot (port 41415)"; empty for your own bot. */
  name: string;
}

/**
 * A small notice over the game view for a player with a localhost bot, and for the host who
 * relays localhost bot seats: it says when a bot cannot be reached or failed, one line per
 * bot, and offers a retry when the relay does not retry itself.
 */
export function mountRelayBanner(relays: BotRelays): {
  el: HTMLElement;
  dispose(): void;
} {
  const el = h("div", {
    class: "relay-banner",
    hidden: true,
    attrs: { role: "status" },
  });
  let stops: (() => void)[] = [];

  function reported(): Reported[] {
    return [
      { relay: relays.own, name: "" },
      ...relays.seats
        .get()
        .map((seat) => ({ relay: seat.relay, name: seat.bot.display_name })),
    ];
  }

  function line(item: Reported, status: RelayStatus): HTMLElement | null {
    if (status.kind !== "error") return null;
    const message = status.retrying
      ? `${status.message} Trying again…`
      : status.message;
    const retry = h(
      "button",
      { type: "button", class: "secondary small", hidden: status.retrying },
      "Try again",
    );
    retry.addEventListener("click", () => item.relay.retry());
    return h(
      "div",
      { class: "relay-row" },
      h(
        "span",
        { class: "relay-text" },
        item.name ? `${item.name}: ${message}` : message,
      ),
      retry,
    );
  }

  function show(): void {
    const rows = reported()
      .map((item) => line(item, item.relay.status.get()))
      .filter((row): row is HTMLElement => row !== null);
    el.replaceChildren(...rows);
    el.hidden = rows.length === 0;
  }

  /** Follows every relay's status, again whenever the seats change. */
  function follow(): void {
    for (const stop of stops) stop();
    stops = reported().map((item) => item.relay.status.subscribe(show));
    show();
  }
  follow();
  const stopSeats = relays.seats.subscribe(follow);
  return {
    el,
    dispose() {
      stopSeats();
      for (const stop of stops) stop();
      stops = [];
    },
  };
}
