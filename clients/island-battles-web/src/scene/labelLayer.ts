import { Matrix, Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { TransformNode } from "@babylonjs/core/Meshes/transformNode";
import type { Scene } from "@babylonjs/core/scene";
import { AdvancedDynamicTexture } from "@babylonjs/gui/2D/advancedDynamicTexture";
import { Control } from "@babylonjs/gui/2D/controls/control";
import { Rectangle } from "@babylonjs/gui/2D/controls/rectangle";
import { TextBlock } from "@babylonjs/gui/2D/controls/textBlock";
import { fadeAlpha, type LabelRequest, placeLabels } from "./labels";

export interface LabelSpec {
  /** Lines of text; each line is drawn as given. */
  lines: string[];
  anchor: TransformNode;
  /** The pill's border and text accent. */
  accent: string;
  /** The pill's fill. */
  fill: string;
  textColor: string;
  fontSize: number;
  bold: boolean;
  /** When two labels overlap, the one with the lower priority is hidden. */
  priority: number;
}

interface Label {
  spec: LabelSpec;
  rectangle: Rectangle;
  text: TextBlock;
  width: number;
  height: number;
  /** Whether the last placement showed it; the control fades towards this. */
  shown: boolean;
  /** Its current opacity, 0 to 1. */
  alpha: number;
}

const FONT_FAMILY = "system-ui, -apple-system, Segoe UI, sans-serif";

let measuringContext: CanvasRenderingContext2D | null | undefined;
function measureWidth(text: string, fontSize: number, bold: boolean): number {
  if (measuringContext === undefined) {
    measuringContext = document.createElement("canvas").getContext("2d");
  }
  if (!measuringContext) return Math.ceil(text.length * fontSize * 0.6);
  measuringContext.font = `${bold ? 700 : 400} ${fontSize}px ${FONT_FAMILY}`;
  return Math.ceil(measuringContext.measureText(text).width);
}

/**
 * Babylon GUI labels linked to nodes in the scene. They are drawn in screen pixels, so they keep
 * their size at every zoom; each frame the lower priority one of two overlapping labels fades out. A label never moves
 * away from its anchor.
 */
export class LabelLayer {
  private readonly texture: AdvancedDynamicTexture;
  private readonly labels = new Map<string, Label>();

  constructor(private readonly scene: Scene) {
    this.texture = AdvancedDynamicTexture.CreateFullscreenUI(
      "labels",
      true,
      scene,
    );
    // Labels never take the pointer, so hovering and clicking reach the meshes below.
    this.texture.isForeground = true;
    // Labels are drawn after the board's post-processing (#102), so tone mapping, bloom and FXAA
    // never soften their text or shift their team colours.
    if (this.texture.layer) this.texture.layer.applyPostProcess = false;
    scene.onBeforeRenderObservable.add(() => this.place());
  }

  /** Adds or updates the label, keeping its GUI controls. */
  set(id: string, spec: LabelSpec): void {
    const lineHeight = Math.ceil(spec.fontSize * 1.25);
    const width =
      Math.max(
        ...spec.lines.map((line) =>
          measureWidth(line, spec.fontSize, spec.bold),
        ),
      ) + 14;
    const height = lineHeight * spec.lines.length + 6;
    let label = this.labels.get(id);
    if (!label) {
      const rectangle = new Rectangle(id);
      rectangle.horizontalAlignment = Control.HORIZONTAL_ALIGNMENT_LEFT;
      rectangle.verticalAlignment = Control.VERTICAL_ALIGNMENT_TOP;
      rectangle.thickness = 2;
      rectangle.cornerRadius = 7;
      rectangle.isHitTestVisible = false;
      rectangle.isPointerBlocker = false;
      const text = new TextBlock(`${id}-text`);
      text.fontFamily = FONT_FAMILY;
      text.isHitTestVisible = false;
      rectangle.addControl(text);
      this.texture.addControl(rectangle);
      rectangle.alpha = 0;
      label = { spec, rectangle, text, width, height, shown: false, alpha: 0 };
      this.labels.set(id, label);
    }
    label.spec = spec;
    label.width = width;
    label.height = height;
    label.rectangle.widthInPixels = width;
    label.rectangle.heightInPixels = height;
    label.rectangle.color = spec.accent;
    label.rectangle.background = spec.fill;
    label.text.text = spec.lines.join("\n");
    label.text.color = spec.textColor;
    label.text.fontSize = spec.fontSize;
    label.text.fontWeight = spec.bold ? "700" : "400";
    label.text.lineSpacing = "0px";
    label.rectangle.linkWithMesh(spec.anchor as never);
  }

  remove(id: string): void {
    const label = this.labels.get(id);
    if (!label) return;
    this.texture.removeControl(label.rectangle);
    label.rectangle.dispose();
    this.labels.delete(id);
  }

  ids(): string[] {
    return [...this.labels.keys()];
  }

  /** The labels' sizes and screen positions, for tests in a browser. */
  describe(): {
    id: string;
    text: string;
    visible: boolean;
    alpha: number;
    x: number;
    y: number;
    width: number;
    height: number;
  }[] {
    return [...this.labels.entries()].map(([id, label]) => ({
      id,
      text: label.spec.lines.join("\n"),
      visible: label.rectangle.isVisible,
      alpha: label.alpha,
      x: label.rectangle.centerX,
      y: label.rectangle.centerY,
      width: label.width,
      height: label.height,
    }));
  }

  dispose(): void {
    this.texture.dispose();
    this.labels.clear();
  }

  private place(): void {
    const camera = this.scene.activeCamera;
    if (!camera || this.labels.size === 0) return;
    const engine = this.scene.getEngine();
    const width = engine.getRenderWidth();
    const height = engine.getRenderHeight();
    const transform = this.scene.getTransformMatrix();
    const viewport = camera.viewport.toGlobal(width, height);
    const elapsed = engine.getDeltaTime();
    const requests: LabelRequest[] = [];
    for (const [id, label] of this.labels) {
      const projected = Vector3.Project(
        label.spec.anchor.getAbsolutePosition(),
        Matrix.IdentityReadOnly,
        transform,
        viewport,
      );
      if (projected.z < 0 || projected.z > 1) continue;
      requests.push({
        id,
        x: projected.x,
        y: projected.y,
        width: label.width,
        height: label.height,
        priority: label.spec.priority,
        wasHidden: !label.shown,
      });
    }
    const shown = new Set(
      placeLabels(requests)
        .filter((placement) => placement.visible)
        .map((placement) => placement.id),
    );
    for (const [id, label] of this.labels) {
      label.shown = shown.has(id);
      const alpha = fadeAlpha(label.alpha, label.shown, elapsed);
      if (alpha !== label.alpha) {
        label.alpha = alpha;
        label.rectangle.alpha = alpha;
      }
      label.rectangle.isVisible = alpha > 0;
    }
  }
}
