// Generates src/api/generated/ from the checked-in API contract:
//   lobby/api/openapi.json   -> lobby.ts   (openapi-typescript)
//   backend/api/openapi.json -> game.ts    (openapi-typescript)
//   lobby/api/events.json    -> events.ts  (json-schema-to-typescript)
// `bun run generate --check` regenerates into a temporary directory and fails
// if the result differs from what is committed.
import {
  mkdir,
  mkdtemp,
  readdir,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { compile } from "json-schema-to-typescript";
import openapiTS, { astToString } from "openapi-typescript";

const root = resolve(import.meta.dir, "../../../..");
const generatedDir = resolve(import.meta.dir, "../../src/api/generated");

const header = (source: string) =>
  `// Generated from ${source} by tools/codegen/generate.ts. Do not edit; run \`bun run generate\`.\n\n`;

const openapi = async (source: string) =>
  header(source) +
  astToString(await openapiTS(pathToFileURL(join(root, source))));

async function generate(outDir: string) {
  await mkdir(outDir, { recursive: true });
  const eventsSource = "lobby/api/events.json";
  const schema = JSON.parse(await readFile(join(root, eventsSource), "utf8"));
  const files: Record<string, string> = {
    "lobby.ts": await openapi("lobby/api/openapi.json"),
    "game.ts": await openapi("backend/api/openapi.json"),
    "events.ts":
      header(eventsSource) +
      (await compile(schema, "LobbyChanged", {
        bannerComment: "",
        cwd: join(root, "lobby/api"),
        format: false,
      })),
  };
  for (const [name, content] of Object.entries(files)) {
    await writeFile(join(outDir, name), content);
  }
}

async function listFiles(dir: string): Promise<string[]> {
  try {
    return (await readdir(dir)).sort();
  } catch {
    return [];
  }
}

if (process.argv.includes("--check")) {
  const tmp = await mkdtemp(join(tmpdir(), "island-battles-generated-"));
  try {
    await generate(tmp);
    const want = await listFiles(tmp);
    const have = await listFiles(generatedDir);
    const stale: string[] = [];
    for (const name of new Set([...want, ...have])) {
      const fresh = want.includes(name)
        ? await readFile(join(tmp, name), "utf8")
        : null;
      const committed = have.includes(name)
        ? await readFile(join(generatedDir, name), "utf8")
        : null;
      if (fresh !== committed) stale.push(name);
    }
    if (stale.length > 0) {
      console.error(
        `src/api/generated is out of date (${stale.join(", ")}). Run \`bun run generate\` and commit the result.`,
      );
      process.exit(1);
    }
    console.log("src/api/generated is up to date");
  } finally {
    await rm(tmp, { recursive: true, force: true });
  }
} else {
  await rm(generatedDir, { recursive: true, force: true });
  await generate(generatedDir);
  console.log(`wrote ${generatedDir}`);
}
