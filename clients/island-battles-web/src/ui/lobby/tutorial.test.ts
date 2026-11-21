import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { serveCommand } from "../../bots/localhost";
import {
  agentPrompt,
  commandText,
  detectSystem,
  installCommand,
  scriptCommand,
  templateRepo,
  tutorialSteps,
  tutorialSystems,
} from "./tutorial";

const template = new URL(
  "../../../../../templates/python-bot/",
  import.meta.url,
);
const read = (path: string) => readFileSync(new URL(path, template), "utf8");

/**
 * The `uv` commands a script runs, up to their first word that only one shell
 * spells this way: a variable, a quote, a redirection or a line continuation.
 */
const uvCommands = (script: string): string[] =>
  script
    .split("\n")
    .map((line) => line.trim().replace(/^exec /, ""))
    .filter((line) => line.startsWith("uv "))
    .map((line) => {
      const words = line.split(/\s+/);
      const shellSpecific = words.findIndex((word) => /[$%"\\>|]/.test(word));
      return words
        .slice(0, shellSpecific < 0 ? undefined : shellSpecific)
        .join(" ");
    });

describe("bot tutorial", () => {
  for (const system of tutorialSystems) {
    describe(system, () => {
      const steps = tutorialSteps(system);

      test("has three steps in order: get, run, package", () => {
        expect(steps.map((step) => step.title.charAt(0))).toEqual([
          "1",
          "2",
          "3",
        ]);
      });

      test("downloads only the Python template from the public repository", () => {
        const first = steps[0];
        if (!first) throw new Error("no steps");
        const text = commandText(first);
        expect(text).toContain(
          `git clone --depth 1 --filter=blob:none --sparse ${templateRepo}`,
        );
        expect(text).toContain("sparse-checkout set templates/python-bot");
        expect(text).toContain(installCommand);
      });

      test("every script it runs or names exists in the template", () => {
        const text = steps
          .flatMap((step) => [...step.commands, step.after])
          .join("\n");
        const named = [...text.matchAll(/scripts[/\\][\w.]+/g)].map(
          (match) => match[0],
        );
        expect(named).toEqual(
          ["check", "package"].map((name) => scriptCommand(system, name)),
        );
        for (const script of named) {
          read(script.replace("\\", "/"));
        }
      });

      test("the zip it names is the one the template builds", () => {
        expect(read("README.md")).toContain("dist/island-battles-bot.zip");
        expect(steps[2]?.intro).toContain("dist/island-battles-bot.zip");
      });

      test("the clone folder matches the copy source", () => {
        const [clone, sparse, copy, cd] = steps[0]?.commands ?? [];
        expect(clone).toEndWith("/island-battles-go");
        expect(sparse).toContain("-C island-battles-go");
        expect(copy).toBe(
          system === "windows"
            ? "Copy-Item -Recurse island-battles-go\\templates\\python-bot my-bot"
            : "cp -r island-battles-go/templates/python-bot my-bot",
        );
        expect(cd).toBe("cd my-bot");
      });
    });
  }

  test("installs the way the template's README and the release's check do", () => {
    expect(read("README.md")).toContain(installCommand);
    const check = readFileSync(
      new URL("../../../../../scripts/check-template.sh", import.meta.url),
      "utf8",
    );
    expect(check).toContain(`install=(${installCommand})`);
  });

  test("serves the bot one way: the bot picker's command, on every system", () => {
    expect(serveCommand).toBe("uv run island-battles-bot serve bot:play");
    for (const system of tutorialSystems) {
      expect(tutorialSteps(system)[1]?.commands).toEqual([serveCommand]);
    }
    expect(read("README.md")).toContain(serveCommand);
    const scripts = readdirSync(new URL("scripts/", template));
    expect(scripts.filter((file) => file.startsWith("serve."))).toEqual([]);
  });

  test("packages the bot with the SDK's command", () => {
    expect(read("scripts/package.sh")).toContain("island-battles-bot package");
  });

  test("shows Windows' commands on Windows", () => {
    expect(
      detectSystem(
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
      ),
    ).toBe("windows");
    expect(
      detectSystem("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"),
    ).toBe("unix");
    expect(detectSystem("Mozilla/5.0 (X11; Linux x86_64)")).toBe("unix");
  });
});

describe("AI tutorial", () => {
  const prompt = agentPrompt();

  test("does the manual tutorial's setup: the same download, install and serve", () => {
    const [get, serve] = tutorialSteps("unix");
    const [clone, sparse] = get?.commands ?? [];
    for (const command of [clone, sparse, installCommand, serveCommand]) {
      expect(prompt).toContain(`\`${command}\``);
    }
    expect(serve?.commands).toEqual([serveCommand]);
    expect(prompt).toContain("templates/python-bot folder to my-bot");
  });

  test("names each script on both systems, and every one exists", () => {
    const named = [...prompt.matchAll(/scripts[/\\][\w.]+/g)].map(
      (match) => match[0],
    );
    expect(named).toEqual(
      ["check", "package"].flatMap((name) =>
        tutorialSystems.map((system) => scriptCommand(system, name)),
      ),
    );
    for (const script of named) read(script.replace("\\", "/"));
  });

  test("points the agent at the template's guide and skills", () => {
    expect(prompt).toContain("AGENTS.md");
    expect(read("AGENTS.md")).toContain(".agents/skills");
    expect(readdirSync(new URL(".agents/skills/", template)).length).toBe(4);
    expect(prompt).toContain("dist/island-battles-bot.zip");
  });
});

describe("the template's Windows scripts", () => {
  const scripts = readdirSync(new URL("scripts/", template));
  const names = scripts
    .filter((file) => file.endsWith(".sh"))
    .map((file) => file.slice(0, -3));

  test("every bash script has a batch twin, and no batch script is alone", () => {
    expect(names.length).toBeGreaterThan(0);
    expect(scripts.filter((file) => file.endsWith(".cmd")).sort()).toEqual(
      names.map((name) => `${name}.cmd`).sort(),
    );
  });

  for (const name of names) {
    test(`${name}.cmd runs what ${name}.sh runs, from the project's folder`, () => {
      const bash = read(`scripts/${name}.sh`);
      const batch = read(`scripts/${name}.cmd`);
      expect(bash.startsWith("#!/usr/bin/env bash")).toBe(true);
      expect(batch.startsWith("@echo off")).toBe(true);
      expect(batch).toContain('cd /d "%~dp0.."');
      expect(uvCommands(batch)).toEqual(uvCommands(bash));
    });
  }

  test("checkouts keep each script's line endings", () => {
    const attributes = read(".gitattributes");
    expect(attributes).toContain("*.cmd text eol=crlf");
    expect(attributes).toContain("*.sh text eol=lf");
  });
});
