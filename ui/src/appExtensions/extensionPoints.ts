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
  "app_agents_agentRail_chatRow_menuItems",
  "app_agents_agentRail_chatRow_marker",
  "app_agents_agentRail_gutter_actions",
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
 * Context each point hands the extension component it renders. Points absent here
 * take no context — their components render from the extension's own state.
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
  /*
   * The two rail-row points below take the same context — the conversation the row is
   * for — because they are one affordance in two places: an entry in the row's menu,
   * and a mark on the row saying where that entry already took you. Splitting the
   * contract would let the two disagree about which conversation they mean.
   *
   * The whole record, not an id. A contribution has to label itself for a screen
   * reader and may want the conversation's age or agent, and a point that hands over
   * an id forces every contributor to re-read a record the row is already holding.
   */
  app_agents_agentRail_chatRow_menuItems: AgentRailChatRowContext;
  app_agents_agentRail_chatRow_marker: AgentRailChatRowContext;
  /*
   * The gutter beside an open conversation, which is the same subject seen from the
   * other side: the row points at a conversation, the gutter is standing in one. So it
   * takes the same context rather than a third shape that would have to be kept in step
   * with this one every time either changes.
   */
  app_agents_agentRail_gutter_actions: AgentRailChatRowContext;
}>;

/**
 * What a contribution to a chat row in the agent rail is told about that row.
 *
 * Structural rather than an import of the API's `AgentInstance`: this module is the
 * contract between the application and code that does not live in it, and a contract
 * that names a generated type changes shape whenever that type is regenerated. The two
 * fields here are the ones the row itself uses, so they are the ones it can promise.
 */
export type AgentRailChatRowContext = {
  /** The conversation's id — the key every one of its addresses is built from. */
  instanceId: string;
  /**
   * The row's own label, already resolved.
   *
   * A contribution that needs to name the conversation — in a tooltip, or an
   * `aria-label` — must say the same thing the row says. Deriving it again from the
   * record produces a second answer the moment the rail's own naming changes, and the
   * reader sees one conversation called two things a few pixels apart.
   */
  label: string;
};

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
 * - `inline` — rendered where the slot sits. Correct whenever the extension
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
  app_agents_agentRail_chatRow_menuItems: "inline",
  app_agents_agentRail_chatRow_marker: "inline",
  app_agents_agentRail_gutter_actions: "inline",
  app_dashboard_dashboardOverview_summaryGrid_leadingCard: "inline",
};

/**
 * The components an extension mounts at extension points. Keys are checked against
 * the ID union, so naming a point that does not exist fails to compile; the
 * component's props are checked against that point's context contract.
 */
export type ExtensionSlotComponents = {
  [Id in ExtensionPointId]?: ComponentType<ExtensionPointProps<Id>>;
};

const EXTENSION_POINT_ID_SET: ReadonlySet<string> = new Set(EXTENSION_POINT_IDS);

/** Runtime guard for config that arrived as plain JSON rather than typed code. */
export function isExtensionPointId(value: string): value is ExtensionPointId {
  return EXTENSION_POINT_ID_SET.has(value);
}
