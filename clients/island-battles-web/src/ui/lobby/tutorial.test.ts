import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import {
  commandText,
  installCommand,
  templateRepo,
  tutorialSteps,
} from "./tutorial";

const template = new URL(
  "../../../../../templates/python-bot/",
  import.meta.url,
);
const read = (path: string) => readFileSync(new URL(path, template), "utf8");

describe("bot tutorial", () => {
  test("has three steps in order: get, run, package", () => {
    expect(tutorialSteps.map((step) => step.title.charAt(0))).toEqual([
      "1",
      "2",
      "3",
    ]);
  });

  test("downloads only the Python template from the public repository", () => {
    const first = tutorialSteps[0];
    if (!first) throw new Error("no steps");
    const text = commandText(first);
    expect(text).toContain(
      `git clone --depth 1 --filter=blob:none --sparse ${templateRepo}`,
    );
    expect(text).toContain("sparse-checkout set templates/python-bot");
    expect(text).toContain(installCommand);
  });

  test("installs the way the template's README and the release's check do", () => {
    expect(read("README.md")).toContain(installCommand);
    const check = readFileSync(
      new URL("../../../../../scripts/check-template.sh", import.meta.url),
      "utf8",
    );
    expect(check).toContain(`install=(${installCommand})`);
  });

  test("every script it runs exists in the template", () => {
    const used = tutorialSteps
      .flatMap((step) => step.commands)
      .filter((command) => command.startsWith("scripts/"));
    expect(used).toEqual(["scripts/serve.sh", "scripts/package.sh"]);
    for (const script of used) {
      expect(read(script).startsWith("#!/usr/bin/env bash")).toBe(true);
    }
    expect(read("scripts/serve.sh")).toContain(
      "island-battles-bot serve bot:play",
    );
    expect(read("scripts/package.sh")).toContain("island-battles-bot package");
  });

  test("the zip it names is the one the template builds", () => {
    expect(read("README.md")).toContain("dist/island-battles-bot.zip");
    expect(tutorialSteps[2]?.intro).toContain("dist/island-battles-bot.zip");
  });

  test("the clone folder matches the copy source", () => {
    const [clone, sparse, copy, cd] = tutorialSteps[0]?.commands ?? [];
    expect(clone).toEndWith("/island-battles-go");
    expect(sparse).toContain("-C island-battles-go");
    expect(copy).toBe("cp -r island-battles-go/templates/python-bot my-bot");
    expect(cd).toBe("cd my-bot");
  });
});
