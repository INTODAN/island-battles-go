import { h } from "../dom";
import {
  commandOrder,
  commandRules,
  type RulesSection,
  rulesSections,
  upgradeOrder,
  upgradeRules,
} from "./rules";
import "./rules.css";

function section(part: RulesSection): HTMLElement {
  const list = part.items
    ? h(
        part.ordered ? "ol" : "ul",
        {},
        ...part.items.map((item) => h("li", {}, item)),
      )
    : null;
  return h(
    "section",
    { attrs: { "aria-labelledby": `rules-${part.id}` } },
    h("h3", { id: `rules-${part.id}` }, part.title),
    ...part.paragraphs.map((paragraph) => h("p", {}, paragraph)),
    list,
  );
}

function commands(): HTMLElement {
  return h(
    "dl",
    { class: "rules-commands" },
    ...commandOrder.flatMap((type) => [
      h("dt", {}, commandRules[type].name),
      h("dd", {}, commandRules[type].description),
    ]),
  );
}

const formatValue = (value: number): string =>
  Number.isInteger(value) ? String(value) : value.toFixed(1);

function upgradeTable(): HTMLElement {
  const levels = Math.max(
    ...upgradeOrder.map((type) => upgradeRules[type].levels.length),
  );
  const levelHeadings = Array.from({ length: levels }, (_, index) =>
    h("th", { scope: "col" }, `Level ${index + 1}`),
  );
  const rows = upgradeOrder.map((type) => {
    const rule = upgradeRules[type];
    const cells = Array.from({ length: levels }, (_, index) => {
      const level = rule.levels[index];
      if (!level) return h("td", { class: "rules-none" }, "—");
      return h(
        "td",
        {},
        h("strong", {}, formatValue(level.value)),
        h(
          "span",
          { class: "rules-cost" },
          level.cost === 0 ? "free" : `${level.cost} soldiers`,
        ),
      );
    });
    return h(
      "tr",
      {},
      h(
        "th",
        { scope: "row" },
        h("span", { class: "rules-upgrade-name" }, rule.name),
        h("span", { class: "rules-unit" }, rule.unit),
      ),
      ...cells,
    );
  });
  const effects = h(
    "dl",
    { class: "rules-commands" },
    ...upgradeOrder.flatMap((type) => [
      h("dt", {}, upgradeRules[type].name),
      h("dd", {}, upgradeRules[type].effect),
    ]),
  );
  return h(
    "div",
    {},
    effects,
    h(
      "div",
      {
        class: "rules-table-scroll",
        // A scrollable region needs to be reachable by keyboard.
        tabIndex: 0,
        attrs: { role: "region", "aria-label": "Upgrade levels and costs" },
      },
      h(
        "table",
        { class: "rules-table" },
        h(
          "caption",
          {},
          "Each level's effect, and the soldiers it costs from the level below",
        ),
        h(
          "thead",
          {},
          h("tr", {}, h("th", { scope: "col" }, "Upgrade"), ...levelHeadings),
        ),
        h("tbody", {}, ...rows),
      ),
    ),
  );
}

/** The rules as one element: every section, with the commands and the upgrade table in place. */
export function renderRules(): HTMLElement {
  const parts = rulesSections.flatMap((part) => {
    const shown: HTMLElement[] = [section(part)];
    if (part.id === "turns") {
      shown.push(
        h(
          "section",
          { attrs: { "aria-labelledby": "rules-commands" } },
          h("h3", { id: "rules-commands" }, "Orders"),
          h("p", {}, "Each turn, every castle your team owns can:"),
          commands(),
        ),
      );
    }
    if (part.id === "upgrades") shown.push(upgradeTable());
    return shown;
  });
  return h("div", { class: "rules-body" }, ...parts);
}

/**
 * Opens the Game rules page as a modal dialog over whatever is on screen, and
 * removes it once closed. Escape, the Close button and a click outside close it.
 */
export function openRules(): HTMLDialogElement {
  const close = h(
    "button",
    { type: "button", class: "secondary small" },
    "Close",
  );
  const dialog = h(
    "dialog",
    { class: "rules-dialog", attrs: { "aria-labelledby": "rules-title" } },
    h(
      "header",
      { class: "rules-header" },
      h("h2", { id: "rules-title" }, "Game rules"),
      close,
    ),
    renderRules(),
  );
  close.addEventListener("click", () => dialog.close());
  dialog.addEventListener("click", (event) => {
    // A click on the backdrop lands on the dialog itself.
    if (event.target === dialog) dialog.close();
  });
  dialog.addEventListener("close", () => dialog.remove());
  document.body.append(dialog);
  dialog.showModal();
  close.focus();
  return dialog;
}

/** A button that opens the Game rules page. */
export function rulesButton(className = "secondary small"): HTMLButtonElement {
  const button = h(
    "button",
    { type: "button", class: className },
    "Game rules",
  );
  button.addEventListener("click", () => openRules());
  return button;
}
