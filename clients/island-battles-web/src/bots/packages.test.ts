import { describe, expect, test } from "bun:test";
import {
  type BotPackage,
  type BotPackageUpload,
  describePackage,
  maxZipBytes,
  type PackageCalls,
  storageProblem,
  uploadCommands,
  uploadPackage,
} from "./packages";

const checked: BotPackage = {
  package_id: "p1",
  owner_player_id: "alex",
  owner_name: "Alex",
  language: "python",
  entry_or_command: "bot:play",
  size_bytes: 1572864,
  uploaded_at: "2026-11-12T12:00:00Z",
};

const presigned: BotPackageUpload = {
  package_id: "p1",
  upload: {
    url: "https://storage.example/bucket",
    fields: { key: "lobbies/l/p1.zip", policy: "p" },
  },
};

/** A file of the given size, without allocating it. */
const file = (name: string, size: number) => ({ name, size }) as File;

function game(complete: () => Promise<BotPackage> = async () => checked) {
  const calls: string[] = [];
  const api: PackageCalls = {
    async startBotPackageUpload(fileName) {
      calls.push(`start ${fileName}`);
      return presigned;
    },
    async completeBotPackageUpload(packageId) {
      calls.push(`complete ${packageId}`);
      return complete();
    },
  };
  return { calls, api };
}

describe("describePackage", () => {
  test("names the language, the entry or command and the size", () => {
    expect(describePackage(checked)).toBe("python · bot:play · 1.5 MiB");
    expect(
      describePackage({
        ...checked,
        language: "native",
        entry_or_command: "./bot worker",
        size_bytes: 3145728,
      }),
    ).toBe("native · ./bot worker · 3.0 MiB");
  });
});

describe("uploadCommands", () => {
  test("uploads with the token in both SDKs", () => {
    const commands = uploadCommands("https://game.example", "ibut1.a.b");
    expect(commands.python).toBe(
      "uv run island-battles-bot upload --game-url https://game.example --token=ibut1.a.b dist/island-battles-bot.zip",
    );
    expect(commands.go).toBe(
      "go run github.com/INTODAN/island-battles-go/clients/island-battles-go/cmd/island-battles-package upload --game-url https://game.example --token=ibut1.a.b dist/island-battles-bot.zip",
    );
  });
});

describe("uploadPackage", () => {
  test("starts, posts the zip to storage with the presigned fields, and completes", async () => {
    const { calls, api } = game();
    const posted: unknown[] = [];
    const progress: number[] = [];
    const zip = file("island-battles-bot.zip", 1000);
    const result = await uploadPackage({
      file: zip,
      calls: api,
      async post(url, fields, sent, onProgress) {
        posted.push([url, fields, sent]);
        onProgress(0.5);
        onProgress(1);
      },
      onProgress: (fraction) => progress.push(fraction),
    });
    expect(result).toEqual(checked);
    expect(calls).toEqual(["start island-battles-bot.zip", "complete p1"]);
    expect(posted).toEqual([
      [presigned.upload.url, presigned.upload.fields, zip],
    ]);
    expect(progress).toEqual([0, 0.5, 1]);
  });

  test("rejects with the game's reason for a bad package", async () => {
    const { api } = game(async () => {
      throw new Error(
        "the package was refused: the zip has no island-battles-bot.json at its root.",
      );
    });
    await expect(
      uploadPackage({
        file: file("bot.zip", 10),
        calls: api,
        post: async () => undefined,
      }),
    ).rejects.toThrow("has no island-battles-bot.json at its root");
  });

  test("refuses a file that can't be a package before sending anything", async () => {
    const { calls, api } = game();
    await expect(
      uploadPackage({ file: file("bot.tar.gz", 10), calls: api }),
    ).rejects.toThrow("Choose the .zip");
    await expect(
      uploadPackage({ file: file("bot.zip", 0), calls: api }),
    ).rejects.toThrow("is empty");
    await expect(
      uploadPackage({ file: file("bot.zip", maxZipBytes + 1), calls: api }),
    ).rejects.toThrow("the limit is 250.0 MiB");
    expect(calls).toEqual([]);
  });

  test("the limit is the SDKs'", () => {
    expect(maxZipBytes).toBe(262_144_000);
  });
});

describe("storageProblem", () => {
  test("reads S3's code and message", () => {
    expect(
      storageProblem(
        "<Error><Code>EntityTooLarge</Code><Message>Too big</Message></Error>",
        400,
      ),
    ).toBe("EntityTooLarge: Too big");
    expect(storageProblem("", 403)).toBe("HTTP 403");
  });
});
