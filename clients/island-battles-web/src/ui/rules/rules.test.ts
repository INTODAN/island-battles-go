/**
 * Checks the Game rules page against the backend's rules, so a rule changed
 * there fails here until the page says it too. It reads the Go sources and the
 * player OpenAPI document; the sentences are checked by people, as the
 * game-rules skill (`.agents/skills/game-rules/SKILL.md`) asks.
 */
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import {
  clashRadius,
  commandOrder,
  commandRules,
  defaultTurnSeconds,
  maxGenerations,
  turnSteps,
  upgradeOrder,
  upgradeRules,
} from "./rules";

const repository = new URL("../../../../../", import.meta.url);
const read = (path: string) => readFileSync(new URL(path, repository), "utf8");

const skill =
  "The game's rules changed: update clients/island-battles-web/src/ui/rules/rules.ts as .agents/skills/game-rules/SKILL.md says.";

/** The value of `const name = value` in a Go file. */
function goConstant(path: string, name: string): number {
  const found = read(path).match(
    new RegExp(`\\b${name}\\s*(?:\\w+\\s*)?=\\s*([0-9.]+)`),
  );
  if (!found?.[1]) throw new Error(`${path} has no constant ${name}`);
  return Number(found[1]);
}

/** `upgradeLevels` of backend/models/upgrades.go, by upgrade type. */
function backendUpgradeLevels(): Record<
  string,
  { value: number; cost: number }[]
> {
  const source = read("backend/models/upgrades.go");
  const names = new Map<string, string>();
  for (const [, constant, type] of source.matchAll(
    /(\w+)\s+UpgradeType\s*=\s*"(\w+)"/g,
  )) {
    if (constant && type) names.set(constant, type);
  }
  const block = source.match(
    /var upgradeLevels = map\[UpgradeType\]\[\]upgradeLevel\{([\s\S]*?)\n\}/,
  )?.[1];
  if (!block)
    throw new Error("backend/models/upgrades.go has no upgradeLevels");
  const levels: Record<string, { value: number; cost: number }[]> = {};
  for (const [, constant, list] of block.matchAll(
    /(\w+):\s*\{(.*)\},?\s*$/gm,
  )) {
    const type = constant && names.get(constant);
    if (!type || !list) throw new Error(`unknown upgrade constant ${constant}`);
    levels[type] = [...list.matchAll(/\{([^{}]*)\}/g)].map(([, fields]) => ({
      value: Number(fields?.match(/Value:\s*([0-9.]+)/)?.[1] ?? 0),
      cost: Number(fields?.match(/SoldierCost:\s*([0-9]+)/)?.[1] ?? 0),
    }));
  }
  return levels;
}

describe("game rules page", () => {
  test("describes every upgrade with the backend's levels and costs", () => {
    const backend = backendUpgradeLevels();
    const page = Object.fromEntries(
      Object.entries(upgradeRules).map(([type, rule]) => [type, rule.levels]),
    );
    expect(page, skill).toEqual(backend);
    expect([...upgradeOrder].sort() as string[], skill).toEqual(
      Object.keys(backend).sort(),
    );
  });

  test("lists every upgrade type of the player API", () => {
    const openapi = JSON.parse(read("backend/api/openapi.json"));
    const types: string[] = openapi.components.schemas.UpgradeType.enum;
    expect(Object.keys(upgradeRules).sort(), skill).toEqual([...types].sort());
  });

  test("lists every player command of the player API", () => {
    const openapi = JSON.parse(read("backend/api/openapi.json"));
    const commands = Object.keys(
      openapi.components.schemas.PublicGenerationCommand.discriminator.mapping,
    );
    expect(Object.keys(commandRules).sort(), skill).toEqual(
      [...commands].sort(),
    );
    expect([...commandOrder].sort() as string[], skill).toEqual(
      [...commands].sort(),
    );
  });

  test("runs the end of a turn in the backend's order", () => {
    const source = read("backend/simulation/commands.go");
    const body = source.match(/func EndOfTurnCommands\(\)[\s\S]*?\n\}/)?.[0];
    if (!body)
      throw new Error(
        "backend/simulation/commands.go has no EndOfTurnCommands",
      );
    const commandTypes = read("backend/models/command.go");
    const steps = [...body.matchAll(/models\.(\w+)/g)]
      .map(
        ([, constant]) =>
          commandTypes.match(
            new RegExp(`\\b${constant}\\s+CommandType\\s*=\\s*"(\\w+)"`),
          )?.[1],
      )
      .filter((step) => step !== undefined);
    expect(
      turnSteps.map((step) => step.step),
      skill,
    ).toEqual(["commands", ...steps]);
  });

  test("uses the backend's numbers", () => {
    expect(maxGenerations, skill).toBe(
      goConstant("backend/services/generation_input.go", "MaxGenerations"),
    );
    expect(defaultTurnSeconds, skill).toBe(
      goConstant(
        "backend/models/lobby_settings.go",
        "DefaultTurnTimeoutSeconds",
      ),
    );
    expect(clashRadius, skill).toBe(
      goConstant("backend/models/soldier_group.go", "DefaultClashRadius"),
    );
  });
});
