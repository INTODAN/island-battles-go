import { checkBotHealth, serveCommand } from "../../bots";
import { copyText, h } from "../dom";
import type { BotChoice, DefaultBot, LobbyActions, LobbyBots } from "./actions";
import { choiceFromValue, choiceValue } from "./roomModel";

export interface BotPicker {
  el: HTMLElement;
  /** Shows the saved choice, from the server, and whether choosing is still possible. */
  update(chosen: BotChoice | undefined, locked: boolean): void;
  dispose(): void;
}

/** Used when `GET /bots/defaults` cannot be read. */
const fallbackBots: DefaultBot[] = [1, 2, 3].map((level) => ({
  level,
  name: `Level ${level}`,
  description: "",
}));

export function mountBotPicker(options: {
  actions: LobbyActions;
  /** Gets the list the server answered with after a choice was saved. */
  onSaved(bots: LobbyBots): void;
  /** Gets the sentence for a refused or failed save; empty clears it. */
  onError(message: string): void;
  describeError(cause: unknown): string;
  healthIntervalMs?: number;
}): BotPicker {
  const { actions } = options;
  let chosen: BotChoice | undefined;
  let locked = false;
  let saving = false;
  let disposed = false;

  const status = h(
    "span",
    { class: "pill", attrs: { "data-status": "checking" } },
    "Checking…",
  );
  const command = h("code", { class: "command" }, serveCommand);
  const copy = h(
    "button",
    { type: "button", class: "secondary small" },
    "Copy command",
  );
  copy.addEventListener("click", async () => {
    copy.textContent = (await copyText(serveCommand))
      ? "Copied"
      : "Copy failed";
    setTimeout(() => {
      copy.textContent = "Copy command";
    }, 1500);
  });
  const local = h("input", {
    type: "radio",
    name: "bot",
    value: "localhost",
    id: "bot-localhost",
  });
  const localLabel = h(
    "label",
    { class: "bot-option", htmlFor: "bot-localhost" },
    local,
    h(
      "span",
      { class: "bot-text" },
      h("strong", {}, "My localhost bot"),
      h(
        "span",
        { class: "hint" },
        "Your own bot, running on this computer. This page relays each turn to it.",
      ),
    ),
    status,
  );
  const localHelp = h(
    "div",
    { class: "bot-help" },
    h("span", { class: "hint" }, "Run this in your bot's folder:"),
    command,
    copy,
  );
  const defaults = h("div", { class: "bot-defaults" });
  const el = h(
    "section",
    { class: "card bot-picker" },
    h("h2", {}, "Your bot"),
    localLabel,
    localHelp,
    h(
      "p",
      { class: "hint" },
      "Or let the server play for you with a default bot:",
    ),
    defaults,
  );

  function radios(): HTMLInputElement[] {
    return Array.from(el.querySelectorAll<HTMLInputElement>("input[name=bot]"));
  }

  function sync(): void {
    const value = choiceValue(chosen);
    for (const radio of radios()) {
      radio.checked = radio.value === value;
      radio.disabled = locked || saving;
    }
    el.classList.toggle("saving", saving);
  }

  async function choose(value: string): Promise<void> {
    const choice = choiceFromValue(value);
    if (!choice || saving || locked || value === choiceValue(chosen)) {
      sync();
      return;
    }
    saving = true;
    options.onError("");
    sync();
    try {
      options.onSaved(await actions.chooseBot(choice));
    } catch (cause) {
      options.onError(options.describeError(cause));
    } finally {
      saving = false;
      if (!disposed) sync();
    }
  }

  el.addEventListener("change", (event) => {
    const target = event.target;
    if (target instanceof HTMLInputElement && target.name === "bot") {
      void choose(target.value);
    }
  });

  function showDefaults(bots: DefaultBot[]): void {
    defaults.replaceChildren(
      ...bots.map((bot) => {
        const id = `bot-level-${bot.level}`;
        return h(
          "label",
          { class: "bot-option", htmlFor: id },
          h("input", {
            type: "radio",
            name: "bot",
            value: `level-${bot.level}`,
            id,
          }),
          h(
            "span",
            { class: "bot-text" },
            h(
              "strong",
              {},
              `Level ${bot.level}${bot.name && bot.name !== `Level ${bot.level}` ? ` · ${bot.name}` : ""}`,
            ),
            bot.description
              ? h("span", { class: "hint" }, bot.description)
              : null,
          ),
        );
      }),
    );
    sync();
  }
  showDefaults(fallbackBots);
  void actions
    .defaultBots()
    .then((bots) => {
      if (!disposed && bots.length > 0) showDefaults(bots);
    })
    .catch(() => undefined);

  async function checkHealth(): Promise<void> {
    if (document.hidden) return;
    const up = await checkBotHealth();
    if (disposed) return;
    status.dataset.status = up ? "up" : "down";
    status.textContent = up ? "Connected" : "Not running";
  }
  void checkHealth();
  const timer = setInterval(
    () => void checkHealth(),
    options.healthIntervalMs ?? 3000,
  );

  return {
    el,
    update(next, isLocked) {
      chosen = next;
      locked = isLocked;
      sync();
    },
    dispose() {
      disposed = true;
      clearInterval(timer);
    },
  };
}
