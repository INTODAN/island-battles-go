import { ShaderMaterial } from "@babylonjs/core/Materials/shaderMaterial";
import { RawTexture } from "@babylonjs/core/Materials/Textures/rawTexture";
import { Texture } from "@babylonjs/core/Materials/Textures/texture";
import { Color3 } from "@babylonjs/core/Maths/math.color";
import { Vector3, Vector4 } from "@babylonjs/core/Maths/math.vector";
import { CreateGround } from "@babylonjs/core/Meshes/Builders/groundBuilder";
import type { Mesh } from "@babylonjs/core/Meshes/mesh";
import type { Scene } from "@babylonjs/core/scene";
import { DEEP_SEA_COLOR, FOAM_COLOR, SHALLOWS_COLOR } from "./art";
import type { DistanceField } from "./seaDistance";

// The sea (issue #102): a toon water shader. Deep water offshore, a light band of shallows by
// the distance to the nearest island (a distance field from seaDistance.ts), a foam line at the
// shore that breathes, gentle swells that move the band edges and a few ripple rings, and a
// faint sky reflection toward the horizon from the environment texture.

/** How far from the waterline the shallows reach, in world units. */
export const SHALLOWS_WIDTH = 0.26;
/** How far from the waterline the foam line reaches. */
export const FOAM_WIDTH = 0.06;

const vertexSource = `
precision highp float;
attribute vec3 position;
uniform mat4 world;
uniform mat4 viewProjection;
varying vec3 vWorld;
void main(void) {
  vec4 worldPosition = world * vec4(position, 1.0);
  vWorld = worldPosition.xyz;
  gl_Position = viewProjection * worldPosition;
}
`;

const fragmentSource = `
precision highp float;
varying vec3 vWorld;
uniform sampler2D distanceMap;
uniform sampler2D sky;
uniform vec4 mapRect;
uniform float maxDistance;
uniform float time;
uniform float skyReady;
uniform vec3 deepColor;
uniform vec3 shallowColor;
uniform vec3 foamColor;
uniform vec3 eye;
uniform float shallowWidth;
uniform float foamWidth;
uniform float linearOutput;
uniform float detail;

void main(void) {
  vec2 p = vWorld.xz;
  vec2 uv = (p - mapRect.xy) * mapRect.zw;
  float d = texture2D(distanceMap, uv).r * maxDistance;
  // Gentle swells move the band edges back and forth.
  float swell = sin(p.x * 2.1 + time * 0.8) * 0.5 + sin(p.y * 1.7 - time * 0.6 + p.x * 0.6) * 0.5;
  float dw = d + swell * 0.025;
  float shallow = 1.0 - smoothstep(shallowWidth - 0.012, shallowWidth + 0.012, dw);
  vec3 color = mix(deepColor, shallowColor, shallow);
  // A toon outline where the shallows meet the deep.
  float edge = 1.0 - smoothstep(0.006, 0.016, abs(dw - shallowWidth));
  color = mix(color, mix(deepColor, shallowColor, 0.45), edge);
  if (detail > 0.5) {
  // A thin ring drifting out from the shallows, fading offshore.
  float drift = fract((d - time * 0.06) / 0.32);
  float ring = 1.0 - smoothstep(0.0, 0.035, abs(drift - 0.5));
  float offshore = smoothstep(shallowWidth + 0.03, shallowWidth + 0.08, dw) * (1.0 - smoothstep(0.3, 0.7, d));
  color = mix(color, mix(deepColor, shallowColor, 0.18), ring * offshore * 0.6);
  // Short, sparse wave dashes on the open sea, drifting slowly.
  vec2 q = vec2(p.x * 0.9 + p.y * 0.35 + time * 0.05, p.y * 2.4);
  vec2 cell = floor(q);
  float hash = fract(sin(dot(cell, vec2(12.9898, 78.233))) * 43758.5453);
  vec2 local = fract(q) - 0.5;
  float dash = step(0.82, hash) * (1.0 - smoothstep(0.2, 0.3, abs(local.x))) * (1.0 - smoothstep(0.03, 0.06, abs(local.y + 0.08 * sin(local.x * 6.0 + time))));
  color = mix(color, mix(deepColor, shallowColor, 0.16), dash * (1.0 - shallow) * smoothstep(0.6, 1.0, d));
  }
  // The foam line at the shore breathes.
  float width = foamWidth * (0.75 + 0.25 * sin(time * 1.3 + p.x * 1.9 + p.y * 1.1));
  float foam = 1.0 - smoothstep(width - 0.008, width + 0.008, dw);
  // A thin line of deeper water just past the foam sets it off from the pale shallows.
  float gap = 1.0 - smoothstep(0.004, 0.012, abs(dw - width - 0.02));
  color = mix(color, mix(shallowColor, deepColor, 0.3), gap * 0.7);
  color = mix(color, foamColor, foam);
  // A faint sky reflection, only toward the horizon.
  vec3 view = normalize(eye - vWorld);
  float fresnel = pow(1.0 - clamp(view.y, 0.0, 1.0), 5.0) * 0.25 * skyReady * detail * (1.0 - shallow);
  if (fresnel > 0.002) {
    vec3 r = reflect(-view, vec3(0.0, 1.0, 0.0));
    vec2 skyUv = vec2(atan(r.z, r.x) / 6.2831853 + 0.5, acos(clamp(r.y, -1.0, 1.0)) / 3.14159265);
    color = mix(color, texture2D(sky, skyUv).rgb, fresnel);
  }
  // Under the post-processing's image processing, output linear colour as Babylon's own
  // materials do; it converts back to gamma.
  if (linearOutput > 0.5) color = pow(color, vec3(2.2));
  gl_FragColor = vec4(color, 1.0);
}
`;

export interface Sea {
  mesh: Mesh;
  material: ShaderMaterial;
  /** Draws shallows and foam around these islands. */
  setField(field: DistanceField): void;
  /** The environment texture, once it has loaded. */
  setSky(texture: Texture): void;
  /** Off draws only the bands and foam: no ripples, wave dashes or sky (the low setting). */
  setDetail(on: boolean): void;
}

function fieldTexture(scene: Scene, field: DistanceField): RawTexture {
  const pixels = new Uint8Array(field.columns * field.rows * 4);
  for (let index = 0; index < field.values.length; index++) {
    const value = Math.round((field.values[index] ?? 1) * 255);
    pixels[index * 4] = value;
    pixels[index * 4 + 1] = value;
    pixels[index * 4 + 2] = value;
    pixels[index * 4 + 3] = 255;
  }
  const texture = RawTexture.CreateRGBATexture(
    pixels,
    field.columns,
    field.rows,
    scene,
    false,
    false,
    Texture.BILINEAR_SAMPLINGMODE,
  );
  texture.name = "sea-distance";
  texture.wrapU = Texture.CLAMP_ADDRESSMODE;
  texture.wrapV = Texture.CLAMP_ADDRESSMODE;
  return texture;
}

/** An open-sea texture: one far-away texel, until the board's islands are known. */
function openSea(scene: Scene): RawTexture {
  const texture = RawTexture.CreateRGBATexture(
    new Uint8Array([255, 255, 255, 255]),
    1,
    1,
    scene,
    false,
    false,
    Texture.NEAREST_SAMPLINGMODE,
  );
  texture.name = "sea-open";
  return texture;
}

export function createSea(scene: Scene, name = "sea"): Sea {
  const mesh = CreateGround(name, { width: 400, height: 400 }, scene);
  mesh.isPickable = false;
  const material = new ShaderMaterial(
    `${name}-water`,
    scene,
    { vertexSource, fragmentSource },
    {
      attributes: ["position"],
      uniforms: [
        "world",
        "viewProjection",
        "mapRect",
        "maxDistance",
        "time",
        "skyReady",
        "deepColor",
        "shallowColor",
        "foamColor",
        "eye",
        "shallowWidth",
        "foamWidth",
        "linearOutput",
        "detail",
      ],
      samplers: ["distanceMap", "sky"],
    },
  );
  let distance = openSea(scene);
  const noSky = openSea(scene);
  material.setTexture("distanceMap", distance);
  material.setTexture("sky", noSky);
  material.setVector4("mapRect", new Vector4(0, 0, 1, 1));
  material.setFloat("maxDistance", 1);
  material.setFloat("skyReady", 0);
  material.setColor3("deepColor", Color3.FromHexString(DEEP_SEA_COLOR));
  material.setColor3("shallowColor", Color3.FromHexString(SHALLOWS_COLOR));
  material.setColor3("foamColor", Color3.FromHexString(FOAM_COLOR));
  material.setFloat("shallowWidth", SHALLOWS_WIDTH);
  material.setFloat("foamWidth", FOAM_WIDTH);
  material.setVector3("eye", Vector3.Zero());
  material.setFloat("time", 0);
  material.setFloat("detail", 1);
  mesh.material = material;
  let seconds = 0;
  const observer = scene.onBeforeRenderObservable.add(() => {
    seconds += scene.getEngine().getDeltaTime() / 1000;
    material.setFloat("time", seconds % 1000);
    material.setFloat(
      "linearOutput",
      scene.imageProcessingConfiguration.applyByPostProcess ? 1 : 0,
    );
    const camera = scene.activeCamera;
    if (camera) material.setVector3("eye", camera.globalPosition);
  });
  mesh.onDisposeObservable.add(() => {
    scene.onBeforeRenderObservable.remove(observer);
    distance.dispose();
    noSky.dispose();
  });
  return {
    mesh,
    material,
    setField(field) {
      const texture = fieldTexture(scene, field);
      material.setTexture("distanceMap", texture);
      distance.dispose();
      distance = texture;
      material.setVector4(
        "mapRect",
        new Vector4(field.minX, field.minZ, 1 / field.width, 1 / field.depth),
      );
      material.setFloat("maxDistance", field.maxDistance);
    },
    setDetail(on) {
      material.setFloat("detail", on ? 1 : 0);
    },
    setSky(texture) {
      material.setTexture("sky", texture);
      material.setFloat("skyReady", 1);
    },
  };
}
