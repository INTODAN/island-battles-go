/**
 * Uploading bot packages to a lobby: the zip that the SDKs' `package` command
 * builds. The game hands out a presigned POST, the browser posts the zip
 * straight to storage with it, and the game then checks the zip. Choosing a
 * package as a bot comes later; here it is only uploaded, listed and deleted.
 */

import type { components } from "../api/generated/game";

export type BotPackage = components["schemas"]["BotPackage"];
export type BotPackageUpload = components["schemas"]["BotPackageUpload"];

/** The zip's limit, in binary units as the SDKs' `package` commands check it. */
export const maxZipBytes = 250 * 1024 * 1024;

/** A size in MiB, as the SDKs print it. */
export function mebibytes(size: number): string {
  return `${(size / (1024 * 1024)).toFixed(1)} MiB`;
}

/** A few words about a package, such as "python · bot:play · 1.2 MiB". */
export function describePackage(item: BotPackage): string {
  return `${item.language} · ${item.entry_or_command} · ${mebibytes(item.size_bytes)}`;
}

/** The commands that upload a package with an upload token, in each SDK. */
export function uploadCommands(
  gameUrl: string,
  token: string,
): { python: string; go: string } {
  const rest = `upload --game-url ${gameUrl} --token ${token} dist/island-battles-bot.zip`;
  return {
    python: `uv run island-battles-bot ${rest}`,
    go: `go run github.com/INTODAN/island-battles-go/clients/island-battles-go/cmd/island-battles-package ${rest}`,
  };
}

/** S3's own code and message, from its XML, or the status. */
export function storageProblem(text: string, status: number): string {
  const found = [
    ...text.matchAll(/<(Code|Message)>([^<]*)<\/(?:Code|Message)>/g),
  ]
    .slice(0, 2)
    .map((match) => match[2]);
  return found.length > 0 ? found.join(": ") : `HTTP ${status}`;
}

/** Posts a file with a presigned POST and reports the progress, 0 to 1. */
export type StoragePost = (
  url: string,
  fields: Record<string, string>,
  file: File,
  onProgress: (fraction: number) => void,
) => Promise<void>;

/**
 * Posts the presigned form, fields first and the file last, with
 * XMLHttpRequest, which reports the upload's progress as `fetch` can't.
 */
export const postToStorage: StoragePost = (url, fields, file, onProgress) =>
  new Promise((resolve, reject) => {
    const form = new FormData();
    for (const [name, value] of Object.entries(fields))
      form.append(name, value);
    form.append("file", file);
    const request = new XMLHttpRequest();
    request.open("POST", url);
    request.upload.addEventListener("progress", (event) => {
      if (event.lengthComputable) onProgress(event.loaded / event.total);
    });
    request.addEventListener("load", () => {
      if (request.status >= 200 && request.status < 300) resolve();
      else
        reject(
          new Error(
            `Storage refused the zip: ${storageProblem(request.responseText, request.status)}`,
          ),
        );
    });
    request.addEventListener("error", () =>
      reject(
        new Error(
          "Could not send the zip to storage. Check your connection and try again.",
        ),
      ),
    );
    request.send(form);
  });

/** The calls an upload makes to the game. */
export interface PackageCalls {
  startBotPackageUpload(fileName: string): Promise<BotPackageUpload>;
  completeBotPackageUpload(packageId: string): Promise<BotPackage>;
}

/**
 * Uploads a zip to the lobby and resolves to the checked package. It rejects
 * before sending anything for a file that can't be a package, and otherwise
 * with what the game or storage said.
 */
export async function uploadPackage(options: {
  file: File;
  calls: PackageCalls;
  post?: StoragePost | undefined;
  onProgress?: (fraction: number) => void;
}): Promise<BotPackage> {
  const { file, calls } = options;
  const onProgress = options.onProgress ?? (() => undefined);
  if (!/\.zip$/i.test(file.name)) {
    throw new Error(
      "Choose the .zip that the SDK's package command writes, such as dist/island-battles-bot.zip.",
    );
  }
  if (file.size === 0) throw new Error(`${file.name} is empty.`);
  if (file.size > maxZipBytes) {
    throw new Error(
      `${file.name} is ${mebibytes(file.size)}; the limit is ${mebibytes(maxZipBytes)}.`,
    );
  }
  const started = await calls.startBotPackageUpload(file.name);
  onProgress(0);
  await (options.post ?? postToStorage)(
    started.upload.url,
    started.upload.fields,
    file,
    onProgress,
  );
  return calls.completeBotPackageUpload(started.package_id);
}
