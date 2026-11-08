// Dev entry points for the game view, until the lobby screens (#98) wire it into the app flow:
//
// - `/?game=<lobby_id>` shows that lobby's game live, for the guest session saved in
//   `sessionStorage` (sign in through the lobby flow, or seed `island-battles-web.session`).
// - `/?demo` asks for a demo file and shows it; `/?demo=<fixture.json>` shows a file from
//   test/fixtures.

import { createRealtime } from "../../realtime";
import { mountDemoView, mountGameView } from ".";

function host(): HTMLElement {
  document.getElementById("scene")?.setAttribute("hidden", "");
  const element = document.createElement("div");
  element.id = "game-dev";
  element.style.cssText = "position:fixed;inset:0";
  document.body.append(element);
  return element;
}

export function startGamePage(lobbyId: string): void {
  const container = host();
  const realtime = createRealtime();
  if (!realtime.session.get()) {
    container.textContent =
      "No guest session saved in this tab. Sign in through the lobby first.";
    return;
  }
  const mounted = mountGameView(container, {
    lobbyId,
    realtime,
    onBack: () => {
      mounted.dispose();
      container.textContent =
        "Back to lobby: the lobby screens will handle this.";
    },
  });
}

export async function startDemoPage(fixture: string): Promise<void> {
  const container = host();
  const open = (file: File) => {
    mountDemoView(container, { file, onBack: choose });
  };
  function choose() {
    const label = document.createElement("label");
    label.style.cssText =
      "color:#fff;font:16px system-ui;padding:16px;display:block";
    label.append("Demo file: ");
    const input = document.createElement("input");
    input.type = "file";
    input.accept = ".json,application/json";
    label.append(input);
    container.replaceChildren(label);
    input.addEventListener("change", () => {
      const file = input.files?.[0];
      if (file) open(file);
    });
  }
  if (fixture) {
    const response = await fetch(`/fixtures/${encodeURIComponent(fixture)}`);
    open(new File([await response.blob()], fixture));
  } else {
    choose();
  }
}
