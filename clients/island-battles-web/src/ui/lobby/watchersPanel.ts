import { ApiError } from "../../api/errors";
import type { Realtime, Snapshot } from "../../realtime";
import { h } from "../dom";
import type { LobbyActions, Watcher } from "./actions";
import { describeProblem } from "./problems";

export interface WatchersPanelOptions {
  realtime: Realtime;
  actions: Pick<LobbyActions, "listWatchers" | "kickWatcher">;
  /** Over the game view: a small collapsible box instead of a card in the room. */
  overlay?: boolean;
}

/**
 * The people who watch the lobby with its join code, outside its seats. Every member sees how
 * many there are and who; the host can kick one, who stops receiving turns at once and can't
 * watch again with the code. The list is read again whenever the lobby changes, since a
 * watcher arriving or leaving is a lobby change.
 */
export function mountWatchersPanel(options: WatchersPanelOptions): {
  el: HTMLElement;
  dispose(): void;
} {
  const { realtime, actions } = options;
  let disposed = false;
  let watchers: Watcher[] = [];
  let reading = false;
  let dirty = false;
  let lastVersion = -1;
  const kicking = new Set<string>();

  const count = h("span", { class: "watchers-count" });
  const list = h("ul", { class: "watchers-list" });
  const empty = h(
    "p",
    { class: "hint" },
    "Nobody is watching. Anyone with the join code can watch without taking a seat.",
  );
  const error = h("p", { class: "field-error", attrs: { role: "alert" } });
  const heading = options.overlay
    ? h("summary", {}, "Watchers ", count)
    : h("header", {}, h("h3", {}, "Watchers"), count);
  const el = options.overlay
    ? h(
        "details",
        { class: "watchers-overlay", attrs: { "aria-label": "Watchers" } },
        heading,
        empty,
        list,
        error,
      )
    : h(
        "section",
        { class: "card watchers", attrs: { "aria-label": "Watchers" } },
        heading,
        empty,
        list,
        error,
      );

  const isHost = (snapshot: Snapshot) =>
    snapshot.lobby.host_player_id === realtime.session.get()?.playerId;

  function render(): void {
    const snapshot = realtime.lobby.get();
    if (disposed || !snapshot) return;
    count.textContent = String(snapshot.watcher_count);
    empty.hidden = snapshot.watcher_count > 0;
    const host = isHost(snapshot);
    list.replaceChildren(
      ...watchers.map((watcher) =>
        h(
          "li",
          { attrs: { "data-player-id": watcher.player_id } },
          h("span", { class: "member-name" }, watcher.display_name),
          host
            ? h(
                "button",
                {
                  type: "button",
                  class: "secondary small",
                  disabled: kicking.has(watcher.player_id),
                  attrs: { "aria-label": `Kick ${watcher.display_name}` },
                  onclick: () => void kick(watcher),
                },
                "Kick",
              )
            : null,
        ),
      ),
    );
  }

  async function refresh(): Promise<void> {
    if (reading) {
      dirty = true;
      return;
    }
    reading = true;
    try {
      watchers = await actions.listWatchers();
      error.textContent = "";
    } catch (cause) {
      error.textContent = describeProblem(cause, "watch").message;
    } finally {
      reading = false;
    }
    if (disposed) return;
    render();
    if (dirty) {
      dirty = false;
      void refresh();
    }
  }

  async function kick(watcher: Watcher): Promise<void> {
    kicking.add(watcher.player_id);
    render();
    try {
      await actions.kickWatcher(watcher.player_id);
      watchers = watchers.filter(
        (item) => item.player_id !== watcher.player_id,
      );
    } catch (cause) {
      // A watcher who left meanwhile is gone either way.
      if (!(cause instanceof ApiError && cause.status === 404)) {
        error.textContent = describeProblem(cause, "watch").message;
      }
    } finally {
      kicking.delete(watcher.player_id);
    }
    if (!disposed) {
      render();
      void refresh();
    }
  }

  function onLobby(snapshot: Snapshot | null): void {
    if (!snapshot || disposed) return;
    if (snapshot.lobby.version !== lastVersion) {
      lastVersion = snapshot.lobby.version;
      if (snapshot.watcher_count > 0 || watchers.length > 0) void refresh();
    }
    render();
  }

  const unsubscribe = realtime.lobby.subscribe(onLobby);
  onLobby(realtime.lobby.get());
  return {
    el,
    dispose() {
      disposed = true;
      unsubscribe();
    },
  };
}
