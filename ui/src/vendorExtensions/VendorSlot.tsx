import { createElement } from "react";
import { createPortal } from "react-dom";
import type { ComponentType, ReactElement } from "react";
import { EXTENSION_POINT_RENDER_MODE } from "./extensionPoints";
import type { ExtensionPointId, ExtensionPointProps } from "./extensionPoints";
import { useVendorSlotComponent } from "./hooks";

/**
 * Props for a slot. Points that declare a context contract require `context`;
 * points that declare none accept no second prop at all, so a slot can never be
 * mounted without the data its component was typed against.
 */
export type VendorSlotProps<Id extends ExtensionPointId> =
  Record<never, never> extends ExtensionPointProps<Id>
    ? { id: Id; context?: ExtensionPointProps<Id> }
    : { id: Id; context: ExtensionPointProps<Id> };

function VendorSlotImpl<Id extends ExtensionPointId>({
  id,
  context,
}: {
  id: Id;
  context?: ExtensionPointProps<Id>;
}): ReactElement | null {
  const Component = useVendorSlotComponent(id);
  if (!Component) return null;

  // `display: contents` keeps the wrapper out of layout, so a slot inside a
  // flex row does not become an extra flex item. It exists only to give the
  // rendered contribution a stable hook for tests and styling.
  const rendered = (
    <div css={{ display: "contents" }} data-testid={`vendor-slot-${id}`}>
      {/* `createElement` rather than JSX: spreading a still-generic props type
          into JSX defeats TypeScript's attribute checking, and the component
          and context are already known to agree by construction. */}
      {createElement(Component as ComponentType<object>, context ?? {})}
    </div>
  );

  return EXTENSION_POINT_RENDER_MODE[id] === "portal"
    ? createPortal(rendered, document.body)
    : rendered;
}

/**
 * Renders the vendor component mounted at `id`, or nothing when the config
 * mounts none. Whether the component is rendered in place or portalled out is
 * the point's own business — see `EXTENSION_POINT_RENDER_MODE`.
 *
 * The cast narrows the permissive implementation signature to the conditional
 * public one; it is the single place the framework trades internal convenience
 * for caller-side strictness.
 */
export const VendorSlot = VendorSlotImpl as <Id extends ExtensionPointId>(
  props: VendorSlotProps<Id>,
) => ReactElement | null;
