import { inflateSync } from "node:zlib";

// A small PNG decoder for tests: 8-bit RGB or RGBA, not interlaced, which is what the castle
// atlas is. The browser decodes it at runtime; tests read the committed file with this.

export interface DecodedPng {
  width: number;
  height: number;
  /** RGBA bytes, rows from the top. */
  pixels: Uint8Array;
}

function paeth(left: number, up: number, upLeft: number): number {
  const estimate = left + up - upLeft;
  const toLeft = Math.abs(estimate - left);
  const toUp = Math.abs(estimate - up);
  const toUpLeft = Math.abs(estimate - upLeft);
  if (toLeft <= toUp && toLeft <= toUpLeft) return left;
  return toUp <= toUpLeft ? up : upLeft;
}

export function decodePng(file: Uint8Array): DecodedPng {
  const view = new DataView(file.buffer, file.byteOffset, file.byteLength);
  let offset = 8;
  let width = 0;
  let height = 0;
  let colorType = 0;
  const data: Uint8Array[] = [];
  while (offset < file.length) {
    const length = view.getUint32(offset);
    const type = new TextDecoder().decode(
      file.subarray(offset + 4, offset + 8),
    );
    const body = file.subarray(offset + 8, offset + 8 + length);
    if (type === "IHDR") {
      width = view.getUint32(offset + 8);
      height = view.getUint32(offset + 12);
      const depth = body[8];
      colorType = body[9] ?? 0;
      if (depth !== 8 || (colorType !== 2 && colorType !== 6) || body[12]) {
        throw new Error("decodePng: only 8-bit RGB or RGBA, not interlaced");
      }
    } else if (type === "IDAT") {
      data.push(body);
    }
    offset += 12 + length;
  }
  const raw = inflateSync(Buffer.concat(data));
  const channels = colorType === 6 ? 4 : 3;
  const stride = width * channels;
  const rows = new Uint8Array(height * stride);
  for (let y = 0; y < height; y++) {
    const filter = raw[y * (stride + 1)];
    for (let x = 0; x < stride; x++) {
      const value = raw[y * (stride + 1) + 1 + x] ?? 0;
      const left = x >= channels ? (rows[y * stride + x - channels] ?? 0) : 0;
      const up = y > 0 ? (rows[(y - 1) * stride + x] ?? 0) : 0;
      const upLeft =
        y > 0 && x >= channels
          ? (rows[(y - 1) * stride + x - channels] ?? 0)
          : 0;
      const predictor = [
        0,
        left,
        up,
        (left + up) >> 1,
        paeth(left, up, upLeft),
      ][filter ?? 0];
      rows[y * stride + x] = (value + (predictor ?? 0)) & 255;
    }
  }
  const pixels = new Uint8Array(width * height * 4);
  for (let index = 0; index < width * height; index++) {
    for (let channel = 0; channel < 3; channel++) {
      pixels[index * 4 + channel] = rows[index * channels + channel] ?? 0;
    }
    pixels[index * 4 + 3] =
      channels === 4 ? (rows[index * channels + 3] ?? 255) : 255;
  }
  return { width, height, pixels };
}
