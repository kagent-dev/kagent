import { createElement } from "react";
import { createPortal } from "react-dom";
import type { ComponentType, ReactElement } from "react";
import { EXTENSION_POINT_RENDER_MODE } from "./extensionPoints";
import type { ExtensionPointId, ExtensionPointProps } from "./extensionPoints";
import { useExtensionSlotComponents } from "./hooks";

/**
 * Props for a slot. Points that declare a context contract require `context`;
 * points that declare none accept no second prop at all, so a slot can never be
 * mounted without the data its component was typed against.
 */
export type ExtensionSlotProps<Id extends ExtensionPointId> =
  Record<never, never> extends ExtensionPointProps<Id>
    ? { id: Id; context?: ExtensionPointProps<Id> }
    : { id: Id; context: ExtensionPointProps<Id> };

function ExtensionSlotImpl<Id extends ExtensionPointId>({
  id,
  context,
}: {
  id: Id;
  context?: ExtensionPointProps<Id>;
}): ReactElement | null {
  const components = useExtensionSlotComponents(id);
  if (components.length === 0) return null;

  // `display: contents` keeps the wrapper out of layout, so a slot inside a
  // flex row does not become an extra flex item. It exists only to give the
  // rendered contributions a stable hook for tests and styling.
  //
  // One wrapper for the point rather than one per contribution: the point is a
  // single place in the layout, and two installed extensions mounting there put
  // two components in that one place — the same way two would sit side by side if
  // the page had written them itself.
  const rendered = (
    <div css={{ display: "contents" }} data-testid={`extension-slot-${id}`}>
      {/* `createElement` rather than JSX: spreading a still-generic props type
          into JSX defeats TypeScript's attribute checking, and the component
          and context are already known to agree by construction.

          Keyed by position, which is stable here because the install order is
          fixed for the life of the document — nothing reorders or filters this
          list at runtime. */}
      {components.map((Component, index) =>
        createElement(Component as ComponentType<object>, {
          key: index,
          ...(context ?? {}),
        }),
      )}
    </div>
  );

  return EXTENSION_POINT_RENDER_MODE[id] === "portal"
    ? createPortal(rendered, document.body)
    : rendered;
}

/**
 * Renders every component mounted at `id`, in install order, or nothing when no
 * installed extension mounts one. Whether they are rendered in place or portalled
 * out is the point's own business — see `EXTENSION_POINT_RENDER_MODE`.
 *
 * The cast narrows the permissive implementation signature to the conditional
 * public one; it is the single place the framework trades internal convenience
 * for caller-side strictness.
 */
export const ExtensionSlot = ExtensionSlotImpl as <Id extends ExtensionPointId>(
  props: ExtensionSlotProps<Id>,
) => ReactElement | null;
