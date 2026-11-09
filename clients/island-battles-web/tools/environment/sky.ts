// Writes assets/environment/sky.png, the board's environment texture (issue #102): a small
// equirectangular sky (u is the azimuth, v runs from the zenith at the top to the nadir at the
// bottom) with a warm glow around the sun of src/scene/look.ts. It is made here rather than
// downloaded, so it needs no licence beyond the repository's.
//
//   bun run tools/environment/sky.ts
import { writeFileSync } from "node:fs";
import { deflateSync } from "node:zlib";
import { SUN_DIRECTION } from "../../src/scene/lookConstants";

const WIDTH = 256;
const HEIGHT = 128;

type Rgb = [number, number, number];
const ZENITH: Rgb = [92, 150, 204];
const UPPER: Rgb = [150, 196, 228];
const HORIZON: Rgb = [232, 236, 230];
const BELOW: Rgb = [30, 62, 76];
const SUN: Rgb = [255, 236, 196];

const mix = (a: Rgb, b: Rgb, t: number): Rgb => [
  a[0] + (b[0] - a[0]) * t,
  a[1] + (b[1] - a[1]) * t,
  a[2] + (b[2] - a[2]) * t,
];
const smooth = (t: number) => {
  const s = Math.min(1, Math.max(0, t));
  return s * s * (3 - 2 * s);
};

// The sun's direction (from the ground toward the sun): the light shines the other way.
const sunLength = Math.hypot(...SUN_DIRECTION);
const sun = SUN_DIRECTION.map((value) => -value / sunLength) as [
  number,
  number,
  number,
];

const raw = Buffer.alloc((WIDTH * 3 + 1) * HEIGHT);
for (let y = 0; y < HEIGHT; y++) {
  raw[y * (WIDTH * 3 + 1)] = 0;
  const theta = ((y + 0.5) / HEIGHT) * Math.PI;
  for (let x = 0; x < WIDTH; x++) {
    const phi = ((x + 0.5) / WIDTH - 0.5) * Math.PI * 2;
    const direction = [
      Math.sin(theta) * Math.cos(phi),
      Math.cos(theta),
      Math.sin(theta) * Math.sin(phi),
    ];
    const up = direction[1] ?? 0;
    let colour: Rgb;
    if (up >= 0) {
      colour = mix(HORIZON, UPPER, smooth(up / 0.35));
      colour = mix(colour, ZENITH, smooth((up - 0.35) / 0.65));
    } else {
      colour = mix(HORIZON, BELOW, smooth(-up / 0.12));
    }
    const facing =
      (direction[0] ?? 0) * sun[0] + up * sun[1] + (direction[2] ?? 0) * sun[2];
    const glow =
      Math.max(0, facing) ** 24 * 0.85 + Math.max(0, facing) ** 4 * 0.12;
    colour = mix(colour, SUN, Math.min(1, glow));
    const offset = y * (WIDTH * 3 + 1) + 1 + x * 3;
    raw[offset] = Math.round(colour[0]);
    raw[offset + 1] = Math.round(colour[1]);
    raw[offset + 2] = Math.round(colour[2]);
  }
}

const crcTable = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});
function crc32(data: Buffer): number {
  let crc = 0xffffffff;
  for (const byte of data)
    crc = (crcTable[(crc ^ byte) & 255] ?? 0) ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}
function chunk(type: string, data: Buffer): Buffer {
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([length, body, crc]);
}
const header = Buffer.alloc(13);
header.writeUInt32BE(WIDTH, 0);
header.writeUInt32BE(HEIGHT, 4);
header[8] = 8; // bit depth
header[9] = 2; // RGB
const png = Buffer.concat([
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  chunk("IHDR", header),
  chunk("IDAT", deflateSync(raw, { level: 9 })),
  chunk("IEND", Buffer.alloc(0)),
]);
const out = new URL("../../assets/environment/sky.png", import.meta.url);
writeFileSync(out, png);
console.log(`${out.pathname}: ${png.length} bytes`);
