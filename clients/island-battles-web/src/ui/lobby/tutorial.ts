import { copyText, h } from "../dom";

/** Where the template is published: the repository's `templates/python-bot` folder. */
export const templateRepo = "https://github.com/INTODAN/island-battles-go";

export interface TutorialStep {
  title: string;
  /** What the step does, before its commands. */
  intro: string;
  /** The shell commands to run, one per line, in order. */
  commands: string[];
  /** What to do next in the game, after the commands. */
  after: string;
}

/**
 * The Python bot template's quick start. The commands are those of
 * `templates/python-bot` (its README, `scripts/` and `pyproject.toml`); they
 * download only that folder, because the template needs nothing outside it.
 */
export const tutorialSteps: TutorialStep[] = [
  {
    title: "1. Get the bot template",
    intro:
      "You need git and uv (https://docs.astral.sh/uv/). This downloads only the Python template into a folder called my-bot, and installs it.",
    commands: [
      `git clone --depth 1 --filter=blob:none --sparse ${templateRepo}`,
      "git -C island-battles-go sparse-checkout set templates/python-bot",
      "cp -r island-battles-go/templates/python-bot my-bot",
      "cd my-bot",
      "uv sync",
    ],
    after:
      "Your bot is bot.py. Run scripts/check.sh to test it once. Change the strategy in play().",
  },
  {
    title: "2. Run it on your computer",
    intro: "Serve your bot on http://127.0.0.1:41414 and leave it running:",
    commands: ["scripts/serve.sh"],
    after:
      "Create a lobby, add a bot seat for the other team, choose My localhost bot as your bot, get ready and start. This tab hands each turn to your bot. Saving bot.py reloads it.",
  },
  {
    title: "3. Build a zip to upload",
    intro:
      "This packs your bot and its dependencies into dist/island-battles-bot.zip:",
    commands: ["scripts/package.sh"],
    after:
      "In the bot picker, choose Upload bot (.zip), or Upload from the command line and run the command it shows. The server then plays your bot, even with every tab closed.",
  },
];

/** The commands of one step as the text a Copy button puts on the clipboard. */
export const commandText = (step: TutorialStep): string =>
  step.commands.join("\n");

/** A command block with a Copy button. */
function commandBlock(step: TutorialStep): HTMLElement {
  const text = commandText(step);
  const pre = h(
    "pre",
    {
      class: "command command-block",
      // A scrollable region needs to be reachable by keyboard.
      tabIndex: 0,
      attrs: { "aria-label": `Commands for ${step.title}` },
    },
    h("code", {}, text),
  );
  const copy = h(
    "button",
    {
      type: "button",
      class: "secondary small",
      attrs: { "aria-label": `Copy commands for ${step.title}` },
    },
    "Copy",
  );
  const status = h("span", { class: "hint", attrs: { role: "status" } });
  copy.addEventListener("click", async () => {
    status.textContent = (await copyText(text))
      ? "Copied."
      : "Copy failed. Select the text instead.";
    setTimeout(() => {
      status.textContent = "";
    }, 2000);
  });
  return h(
    "div",
    { class: "tutorial-commands" },
    pre,
    h("div", { class: "tutorial-copy" }, copy, status),
  );
}

/** The collapsible bot tutorial of the main menu. */
export function mountTutorial(): HTMLElement {
  return h(
    "details",
    { class: "card tutorial" },
    h("summary", {}, "Tutorial: build your own bot"),
    h(
      "p",
      { class: "hint" },
      "A short guide to the Python bot template: get it, play it from your computer, and package it for upload.",
    ),
    h(
      "ol",
      { class: "tutorial-steps" },
      ...tutorialSteps.map((step) =>
        h(
          "li",
          {},
          h("h3", {}, step.title),
          h("p", {}, step.intro),
          commandBlock(step),
          h("p", {}, step.after),
        ),
      ),
    ),
  );
}
