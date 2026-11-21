import { serveCommand } from "../../bots/localhost";
import { copyText, h } from "../dom";

/** Where the template is published: the repository's `templates/python-bot` folder. */
export const templateRepo = "https://github.com/INTODAN/island-battles-go";

/**
 * The systems the tutorial has commands for: a POSIX shell (macOS, Linux, or
 * Git Bash and WSL on Windows), or Windows' PowerShell.
 */
export type TutorialSystem = "unix" | "windows";

export const tutorialSystems: readonly TutorialSystem[] = ["unix", "windows"];

export const systemLabels: Record<TutorialSystem, string> = {
  unix: "macOS or Linux",
  windows: "Windows (PowerShell)",
};

/**
 * Installs the template. The template has no uv.lock, so this takes the newest
 * island-battles-python, the only SDK that reads the turns the live game sends,
 * and writes the player's own lock.
 */
export const installCommand = "uv sync";

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
 * How `system` runs one of the template's scripts: `scripts/NAME.sh`, or its
 * batch twin `scripts\NAME.cmd` on Windows, which runs in PowerShell and cmd
 * without changing the execution policy.
 */
export const scriptCommand = (system: TutorialSystem, name: string): string =>
  system === "windows" ? `scripts\\${name}.cmd` : `scripts/${name}.sh`;

/**
 * The Python bot template's quick start on `system`. The commands are those of
 * `templates/python-bot` (its README, `scripts/` and `pyproject.toml`); they
 * download only that folder, because the template needs nothing outside it.
 */
export function tutorialSteps(system: TutorialSystem): TutorialStep[] {
  const copy =
    system === "windows"
      ? "Copy-Item -Recurse island-battles-go\\templates\\python-bot my-bot"
      : "cp -r island-battles-go/templates/python-bot my-bot";
  return [
    {
      title: "1. Get the bot template",
      intro:
        "You need git and uv (https://docs.astral.sh/uv/). This downloads only the Python template into a folder called my-bot, and installs it with the newest SDK, the one the game speaks.",
      commands: [
        `git clone --depth 1 --filter=blob:none --sparse ${templateRepo}`,
        "git -C island-battles-go sparse-checkout set templates/python-bot",
        copy,
        "cd my-bot",
        // scripts/check-template.sh runs this command before every release.
        installCommand,
      ],
      after: `Your bot is bot.py. Run ${scriptCommand(system, "check")} to test it once. Change the strategy in play().`,
    },
    {
      title: "2. Run it on your computer",
      intro: "Serve your bot on http://127.0.0.1:41414 and leave it running:",
      // The same command on every system, and the one the bot picker shows.
      commands: [serveCommand],
      after:
        "Create a lobby, add a default bot seat for the other team, choose My localhost bot as your bot, get ready and start. This tab hands each turn to your bot. Saving bot.py reloads it.",
    },
    {
      title: "3. Build a zip to upload",
      intro:
        "This packs your bot and its dependencies into dist/island-battles-bot.zip:",
      commands: [scriptCommand(system, "package")],
      after:
        "In the bot picker, choose Upload bot (.zip), or Upload from the command line and run the command it shows. The server then plays your bot, even with every tab closed.",
    },
  ];
}

/**
 * The AI tutorial's prompt: one message for a coding agent (Claude Code,
 * Codex, Cursor and the like) that does the manual tutorial's setup itself. It
 * is built from the manual steps' commands, so the two tutorials stay the same
 * setup, and it names both systems' scripts, because the agent can tell which
 * system it runs on.
 */
export function agentPrompt(): string {
  const [get] = tutorialSteps("unix");
  const [clone, sparse] = get?.commands ?? [];
  const both = (name: string) =>
    `\`${scriptCommand("unix", name)}\` (\`${scriptCommand("windows", name)}\` on Windows)`;
  return [
    "Set up a bot for the game Island Battles for me, from its Python bot template, in a new folder called my-bot here.",
    "",
    "1. Check that git and uv (https://docs.astral.sh/uv/) are installed. If one is missing, tell me how to install it, or install it if I agree.",
    `2. Download only the template: run \`${clone}\`, then \`${sparse}\`, and copy the island-battles-go/templates/python-bot folder to my-bot. Work in my-bot from now on.`,
    `3. Run \`${installCommand}\`. The template has no uv.lock on purpose: this takes the newest island-battles-python SDK, the only one the live game speaks.`,
    "4. Read AGENTS.md and the skills in .agents/skills/ before changing anything. They explain the game and how a bot is written. The bot is bot.py, and its strategy is play().",
    `5. Run ${both("check")} and fix anything that fails.`,
    `6. Serve the bot with \`${serveCommand}\` in my-bot, in the background, and leave it running. It listens on http://127.0.0.1:41414. Saving bot.py reloads it.`,
    "7. Tell me when it is running, and how to stop it.",
    "",
    `Later, when I ask for a zip to upload, run ${both("package")}: it builds dist/island-battles-bot.zip.`,
  ].join("\n");
}

/** The collapsible AI tutorial of the main menu: one prompt to copy. */
export function mountAgentTutorial(): HTMLElement {
  return h(
    "details",
    { class: "card tutorial" },
    h("summary", {}, "Tutorial: let your AI set it up"),
    h(
      "p",
      { class: "hint" },
      "The same setup in one step: copy this prompt into a coding agent, such as Claude Code, Codex or Cursor, opened in the folder where you want your bot.",
    ),
    copyBlock("The setup prompt", agentPrompt(), "tutorial-prompt"),
    h(
      "p",
      {},
      "When it says your bot is running, create a lobby, add a default bot seat for the other team, choose My localhost bot as your bot, get ready and start. Then ask it to improve the strategy, or to build a zip to upload.",
    ),
  );
}

/** Both tutorials, side by side: by hand, and with a coding agent. */
export function mountTutorials(): HTMLElement {
  return h(
    "div",
    { class: "tutorials" },
    mountTutorial(),
    mountAgentTutorial(),
  );
}

/** The system whose commands the tutorial shows first: Windows on Windows. */
export function detectSystem(
  userAgent: string = globalThis.navigator?.userAgent ?? "",
): TutorialSystem {
  return /Windows/i.test(userAgent) ? "windows" : "unix";
}

/** The commands of one step as the text a Copy button puts on the clipboard. */
export const commandText = (step: TutorialStep): string =>
  step.commands.join("\n");

/** A block of text to copy, such as commands or a prompt, with a Copy button. */
function copyBlock(label: string, text: string, className = ""): HTMLElement {
  const pre = h(
    "pre",
    {
      class: `command command-block ${className}`.trim(),
      // A scrollable region needs to be reachable by keyboard.
      tabIndex: 0,
      attrs: { "aria-label": label },
    },
    h("code", {}, text),
  );
  const copy = h(
    "button",
    {
      type: "button",
      class: "secondary small",
      attrs: {
        "aria-label": `Copy ${label.charAt(0).toLowerCase()}${label.slice(1)}`,
      },
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

function stepItems(system: TutorialSystem): HTMLElement[] {
  return tutorialSteps(system).map((step) =>
    h(
      "li",
      {},
      h("h3", {}, step.title),
      h("p", {}, step.intro),
      copyBlock(`Commands for ${step.title}`, commandText(step)),
      h("p", {}, step.after),
    ),
  );
}

/** The collapsible bot tutorial of the main menu. */
export function mountTutorial(
  system: TutorialSystem = detectSystem(),
): HTMLElement {
  const steps = h("ol", { class: "tutorial-steps" }, ...stepItems(system));
  const choices = tutorialSystems.map((choice) => {
    const input = h("input", {
      type: "radio",
      name: "tutorial-system",
      value: choice,
      checked: choice === system,
    });
    input.addEventListener("change", () => {
      if (input.checked) steps.replaceChildren(...stepItems(choice));
    });
    return h("label", {}, input, systemLabels[choice]);
  });
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
      "fieldset",
      { class: "tutorial-system" },
      h("legend", {}, "Commands for"),
      ...choices,
    ),
    steps,
  );
}
