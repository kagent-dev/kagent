import type { ComponentType } from "react";

/**
 * Every extension point the application offers, in one list.
 *
 * IDs are shaped `app_<area>_<page>_<component>_<slot>` so the name alone says
 * where the point lives. This array is the single source of truth: the ID union
 * is derived from it, so a point cannot exist in the type system without also
 * existing at runtime for validation to check against.
 */
export const EXTENSION_POINT_IDS = [
  "app_shell_appHeader_actions_leading",
  "app_shell_appLayout_contentArea_leadingBanner",
  "app_shell_appLayout_contentArea_globalOverlay",
  "app_shell_appLayout_appSidebar_footer",
  "app_agents_agentsList_pageHeader_actions",
  "app_agents_agentsList_agentListItem_badge",
  "app_agents_agentChat_agentChatMessage_additionalActionsButton",
  "app_dashboard_dashboardOverview_summaryGrid_leadingCard",
] as const;

/** Union of valid point IDs. A typo anywhere is a compile error. */
export type ExtensionPointId = (typeof EXTENSION_POINT_IDS)[number];

/**
 * Constrains the props map below to real point IDs. Written as a constrained
 * pass-through rather than `Record<ExtensionPointId, …>` so that `keyof` stays
 * narrowed to the points that actually take context.
 */
type PropsFor<T extends Partial<Record<ExtensionPointId, object>>> = T;

/**
 * Context each point hands the vendor component it renders. Points absent here
 * take no context — their components render from the vendor's own state.
 */
type ExtensionPointPropsMap = PropsFor<{
  app_agents_agentsList_agentListItem_badge: {
    agentName: string;
    namespace: string;
  };
  app_agents_agentChat_agentChatMessage_additionalActionsButton: {
    messageId: string;
    role: "user" | "agent";
    text: string;
    /**
     * The turn this message belongs to, and when it happened.
     *
     * A message id identifies the message *to this client*; anything asking a backend
     * about the work behind it — a trace, a cost, a replay — is keyed by the turn and
     * the conversation instead. Both are optional because a transport need not group
     * turns, and a contribution that needs them must handle their absence rather than
     * assume a shape this port does not promise.
     */
    taskId?: string;
    /** RFC3339, for a lookup that has to be windowed in time. */
    createdAt?: string;
    /** The conversation this message is part of. */
    sessionId?: string;
  };
}>;

/** The empty context, for points that pass nothing to their component. */
export type NoSlotContext = Record<never, never>;

/** Props the component mounted at `Id` receives. */
export type ExtensionPointProps<Id extends ExtensionPointId> =
  Id extends keyof ExtensionPointPropsMap
    ? ExtensionPointPropsMap[Id]
    : NoSlotContext;

/**
 * How a point puts its component into the DOM.
 *
 * - `inline` — rendered where the slot sits. Correct whenever the vendor
 *   component belongs in the surrounding layout flow.
 * - `portal` — rendered into `document.body` via `createPortal`. Needed only
 *   when the slot must escape its parent's DOM position: the content area is an
 *   `overflow: auto` scroll container with its own padding and stacking
 *   context, so a floating overlay declared inside it would be clipped by the
 *   scroll box and trapped under sibling chrome. The portal lifts it to the
 *   document root while the slot stays declared where it conceptually belongs.
 */
export type ExtensionPointRenderMode = "inline" | "portal";

export const EXTENSION_POINT_RENDER_MODE: Record<
  ExtensionPointId,
  ExtensionPointRenderMode
> = {
  app_shell_appHeader_actions_leading: "inline",
  app_shell_appLayout_contentArea_leadingBanner: "inline",
  app_shell_appLayout_contentArea_globalOverlay: "portal",
  app_shell_appLayout_appSidebar_footer: "inline",
  app_agents_agentsList_pageHeader_actions: "inline",
  app_agents_agentsList_agentListItem_badge: "inline",
  app_agents_agentChat_agentChatMessage_additionalActionsButton: "inline",
  app_dashboard_dashboardOverview_summaryGrid_leadingCard: "inline",
};

/**
 * The components a vendor mounts at extension points. Keys are checked against
 * the ID union, so naming a point that does not exist fails to compile; the
 * component's props are checked against that point's context contract.
 */
export type VendorSlotComponents = {
  [Id in ExtensionPointId]?: ComponentType<ExtensionPointProps<Id>>;
};

const EXTENSION_POINT_ID_SET: ReadonlySet<string> = new Set(EXTENSION_POINT_IDS);

/** Runtime guard for config that arrived as plain JSON rather than typed code. */
export function isExtensionPointId(value: string): value is ExtensionPointId {
  return EXTENSION_POINT_ID_SET.has(value);
}
