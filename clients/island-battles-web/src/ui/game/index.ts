import type { Realtime } from "../../realtime";

/**
 * Placeholder until the game view (#101) lands: it has the signature that view exports, so the
 * lobby screens can switch to it. The real module replaces this file.
 */
export function mountGameView(
  container: HTMLElement,
  options: { lobbyId: string; realtime: Realtime; onBack: () => void },
): { dispose(): void } {
  const note = document.createElement("div");
  note.className = "game-placeholder";
  note.textContent = "Game starting…";
  const back = document.createElement("button");
  back.type = "button";
  back.textContent = "Back to the lobby screens";
  back.addEventListener("click", options.onBack);
  container.append(note, back);
  return {
    dispose() {
      note.remove();
      back.remove();
    },
  };
}
