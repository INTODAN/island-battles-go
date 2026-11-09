import { VertexData } from "@babylonjs/core/Meshes/mesh.vertexData";
import { type AtlasSwatch, swatchUv } from "./atlasUv";

// A small builder for flat-shaded, low-poly meshes on the castle atlas (issue #102). Every
// triangle gets vertices of its own with the face's normal, so hard edges stay hard, and a UV
// on an atlas swatch per corner, so a face can run along a swatch's gradient.

export type Point3 = readonly [number, number, number];
/** A multiplier of a face's atlas colour: one for all channels, or red, green and blue. */
export type Shade = number | readonly [number, number, number];

/** A corner's place on the atlas: a swatch and how far along its gradient (0 top row, 1 bottom). */
export interface SwatchPoint {
  swatch: AtlasSwatch;
  along: number;
}

export class FlatGeometry {
  readonly positions: number[] = [];
  readonly normals: number[] = [];
  readonly uvs: number[] = [];
  readonly colors: number[] = [];
  readonly indices: number[] = [];

  /**
   * Adds a triangle facing `toward` (its winding is fixed so the normal points that way, the
   * side Babylon draws). `shade` multiplies the atlas colour of the face through vertex colours,
   * as one factor or per channel; 1 keeps the swatch's own colour.
   */
  triangle(
    a: Point3,
    b: Point3,
    c: Point3,
    colours: readonly [SwatchPoint, SwatchPoint, SwatchPoint],
    toward: Point3,
    shade: Shade = 1,
  ): void {
    let normal = faceNormal(a, b, c);
    let corners: [Point3, Point3, Point3] = [a, b, c];
    let atlas = colours;
    if (dot(normal, toward) < 0) {
      corners = [a, c, b];
      atlas = [colours[0], colours[2], colours[1]];
      normal = [-normal[0], -normal[1], -normal[2]];
    }
    const start = this.positions.length / 3;
    for (const [index, corner] of corners.entries()) {
      const point = atlas[index] as SwatchPoint;
      this.positions.push(corner[0], corner[1], corner[2]);
      this.normals.push(normal[0], normal[1], normal[2]);
      const [u, v] = swatchUv(point.swatch, point.along);
      this.uvs.push(u, v);
      if (typeof shade === "number") this.colors.push(shade, shade, shade, 1);
      else this.colors.push(shade[0], shade[1], shade[2], 1);
    }
    this.indices.push(start, start + 1, start + 2);
  }

  /** A quad a-b-c-d (in order around its edge) as two triangles facing `toward`. */
  quad(
    a: Point3,
    b: Point3,
    c: Point3,
    d: Point3,
    colours: readonly [SwatchPoint, SwatchPoint, SwatchPoint, SwatchPoint],
    toward: Point3,
    shade: Shade = 1,
  ): void {
    this.triangle(a, b, c, [colours[0], colours[1], colours[2]], toward, shade);
    this.triangle(a, c, d, [colours[0], colours[2], colours[3]], toward, shade);
  }

  get triangleCount(): number {
    return this.indices.length / 3;
  }

  toVertexData(): VertexData {
    const data = new VertexData();
    data.positions = this.positions;
    data.normals = this.normals;
    data.uvs = this.uvs;
    data.colors = this.colors;
    data.indices = this.indices;
    return data;
  }
}

function dot(left: Point3, right: Point3): number {
  return left[0] * right[0] + left[1] * right[1] + left[2] * right[2];
}

/**
 * The normal Babylon computes for a triangle (`VertexData.ComputeNormals`: (a - b) × (c - b)),
 * which is the side it draws with back faces culled.
 */
export function faceNormal(a: Point3, b: Point3, c: Point3): Point3 {
  const u = [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
  const v = [c[0] - b[0], c[1] - b[1], c[2] - b[2]];
  const x = (u[1] ?? 0) * (v[2] ?? 0) - (u[2] ?? 0) * (v[1] ?? 0);
  const y = (u[2] ?? 0) * (v[0] ?? 0) - (u[0] ?? 0) * (v[2] ?? 0);
  const z = (u[0] ?? 0) * (v[1] ?? 0) - (u[1] ?? 0) * (v[0] ?? 0);
  const length = Math.hypot(x, y, z) || 1;
  return [x / length, y / length, z / length];
}

/** A flat colour on a swatch, for faces that do not run along the gradient. */
export function solid(
  swatch: AtlasSwatch,
  along: number,
): [SwatchPoint, SwatchPoint, SwatchPoint, SwatchPoint] {
  const point = { swatch, along };
  return [point, point, point, point];
}
