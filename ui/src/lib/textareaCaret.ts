/**
 * Caret coordinates inside a textarea (mirror-div algorithm).
 * Based on https://github.com/component/textarea-caret-position (MIT).
 */
const MIRROR_PROPS: readonly string[] = [
  "direction",
  "boxSizing",
  "width",
  "height",
  "overflowX",
  "overflowY",
  "borderTopWidth",
  "borderRightWidth",
  "borderBottomWidth",
  "borderLeftWidth",
  "borderStyle",
  "paddingTop",
  "paddingRight",
  "paddingBottom",
  "paddingLeft",
  "fontStyle",
  "fontVariant",
  "fontWeight",
  "fontStretch",
  "fontSize",
  "lineHeight",
  "fontFamily",
  "textAlign",
  "textTransform",
  "textIndent",
  "textDecoration",
  "letterSpacing",
  "wordSpacing",
  "tabSize",
  "MozTabSize",
];

function getCaretCoordinates(element: HTMLTextAreaElement, position: number): { top: number; left: number } {
  const div = document.createElement("div");
  document.body.appendChild(div);

  const style = div.style;
  const computed = window.getComputedStyle(element);

  style.whiteSpace = "pre-wrap";
  style.wordWrap = "break-word";
  style.position = "absolute";
  style.visibility = "hidden";
  style.overflow = "hidden";

  for (const prop of MIRROR_PROPS) {
    const key = prop as keyof CSSStyleDeclaration;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (style as any)[key] = (computed as any)[key];
  }

  if (element.scrollHeight > parseInt(computed.height, 10)) {
    style.overflowY = "scroll";
  } else {
    style.overflow = "hidden";
  }

  div.textContent = element.value.substring(0, position);
  const span = document.createElement("span");
  span.textContent = element.value.substring(position) || ".";
  div.appendChild(span);

  const borderTop = parseInt(computed.borderTopWidth, 10) || 0;
  const borderLeft = parseInt(computed.borderLeftWidth, 10) || 0;
  const top = span.offsetTop + borderTop;
  const left = span.offsetLeft + borderLeft;

  document.body.removeChild(div);

  return { top, left };
}

/** Viewport coordinates for fixed-position UI (e.g. popovers) at the text caret. */
export function getCaretViewportCoords(textarea: HTMLTextAreaElement, position: number): { top: number; left: number } {
  const local = getCaretCoordinates(textarea, position);
  const rect = textarea.getBoundingClientRect();
  return {
    top: rect.top + local.top - textarea.scrollTop,
    left: rect.left + local.left - textarea.scrollLeft,
  };
}
