type Child = Node | string | null | undefined | false;

type Props<K extends keyof HTMLElementTagNameMap> = Partial<
  Omit<HTMLElementTagNameMap[K], "style" | "children" | "classList">
> & {
  class?: string;
  attrs?: Record<string, string>;
};

/** Creates an element with properties, attributes and children; falsy children are skipped. */
export function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  props: Props<K> = {},
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const element = document.createElement(tag);
  const { class: className, attrs, ...rest } = props;
  if (className) element.className = className;
  Object.assign(element, rest);
  for (const [name, value] of Object.entries(attrs ?? {})) {
    element.setAttribute(name, value);
  }
  element.append(
    ...children.filter(
      (child): child is Node | string =>
        child !== null && child !== undefined && child !== false,
    ),
  );
  return element;
}

export function clear(element: Element): void {
  element.replaceChildren();
}

/** Copies text to the clipboard; false when the browser refuses. */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
