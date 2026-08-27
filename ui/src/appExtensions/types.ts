import type { ComponentType, ReactElement, ReactNode } from "react";
import type { ExtensionSlotComponents } from "./extensionPoints";
import type { ExtensionFormFieldContribution } from "./formFields";
import type { AgentInstance, ApiCallId } from "@/api";
import type { ExtensionApi } from "./api/extensionApi";
import type { ExtensionTheme } from "./theme";
import type { ExtensionShell } from "./shell";
import type { ExtensionBranding } from "./branding";
import type { ExtensionNavOverrides } from "./navOverrides";
import type { ExtensionTableColumn } from "./tableColumns";

/** Props a contributed nav item's renderer receives. */
export interface ExtensionNavItemProps {
  /** True when the current location matches the item's `path`. */
  isActive: boolean;
  /**
   * True while the rail is showing its narrow form.
   *
   * The renderer cannot work this out for itself — the rail owns the state, and
   * it changes on a breakpoint as well as on the collapse control — so an entry
   * that was not told would keep its label while every entry beside it had
   * shrunk to an icon. A contribution that only ever renders a mark can ignore it.
   */
  collapsed: boolean;
}

/**
 * A navigation entry contributed by an extension.
 *
 * The extension supplies the whole renderer — the framework offers no label/icon
 * shorthand on purpose. `order` is the only positioning input: core items sit
 * at multiples of 100, so 250 lands between Agents (200) and Models (300).
 */
export interface ExtensionNavItemContribution {
  /** Unique across core and extension nav items. */
  key: string;
  order: number;
  /**
   * Used for active-state matching only. The component renders its own link,
   * so this is optional for items that do not navigate.
   */
  path?: string;
  /**
   * What this page is called, for anything that has to *name* it rather than render it.
   *
   * The component draws the entry, so nothing here needs a label to show the reader —
   * chrome that states which page is open does, and it cannot open a component to find
   * out. Without this it has to guess from the URL, which turns `/evals` into "Evals"
   * on a page titled Evaluations.
   *
   * Optional: an entry nobody needs to name can leave it out, and a shell that never
   * names the active page never reads it.
   */
  label?: string;
  Component: ComponentType<ExtensionNavItemProps>;
}

/**
 * Arbitrary data a product attaches to a route and reads back in its own shell.
 *
 * The application puts it on the route's React Router `handle` and never looks
 * inside: whatever goes in comes back out of `useMatches()[i].handle`, values and
 * functions alike. That is the whole contract — a product that wants per-page
 * information its shell needs, and the application does not, describes it here
 * rather than asking for a field of its own.
 *
 * Deliberately opaque. A shell that draws a trail of ancestors, one that shows a
 * page-level icon, one that decides which help article a page maps to — none of
 * those are concepts the application has to learn in order to carry the data, and
 * naming any of them here would make this seam only good for that one.
 */
export type ExtensionRouteHandle = Readonly<Record<string, unknown>>;

/** A whole page contributed by an extension, merged into the router. */
export interface ExtensionRouteContribution {
  /** React Router path. Must not collide with a core route. */
  path: string;
  element: ReactElement;
  /**
   * When true the route renders outside `AppLayout` — no header, no sidebar.
   * Use for full-screen flows such as an extension's own login.
   */
  standalone?: boolean;
  /**
   * The key of an application route this one takes the place of.
   *
   * Without it, a path the application already claims is rejected — an
   * accidental collision should always be an error. Declaring it is how a
   * product says "this destination is mine": the named route is dropped and
   * this one serves the path instead.
   *
   * Reach for it only when the destination genuinely belongs to the product
   * rather than being a variant of the application's. Replacing a page the
   * application maintains means its improvements stop arriving.
   */
  replaces?: string;
  /**
   * Data for this route that only the product's own shell reads.
   *
   * Attached verbatim to the route's React Router `handle`, so it comes back from
   * `useMatches()` while the page is active. The application does not interpret
   * it; see `ExtensionRouteHandle`.
   */
  handle?: ExtensionRouteHandle;
}

/**
 * How an agent is addressed: a namespace and an `AgentInstance` id.
 *
 * An agent *is* an instance, and an instance has no name — its id is a UUID, which
 * is what every one of its RPCs takes. So this carries an id where it used to carry
 * a name.
 */
export interface ExtensionAgentRef {
  namespace: string;
  id: string;
}

/**
 * Agent-navigation links an extension may redefine.
 *
 * A product that serves its own agent surfaces at its own addresses can share the
 * application's agent rail instead of keeping a copy of it: the rail is the same
 * navigation either way, and only its destinations differ. Each is a function rather
 * than a path template so a product builds its own URLs however it already does.
 *
 * Every one is optional, and an omitted link falls back to the application's own
 * route — a product that redirects the conversation but not the details page gets
 * exactly that.
 */
export interface ExtensionAgentLinks {
  /**
   * Where selecting an agent on the list page should navigate.
   */
  fromAgentsList?: (instance: AgentInstance) => string;
  /*
   * The two surfaces one agent has, unprefixed: the interface already says these are
   * an agent's.
   *
   * There is no `conversation` and no `settings` any more, and neither is an
   * omission. A conversation *is* an instance, so a row in the rail is a `chat` link
   * to a different instance rather than a session beneath this one; and an instance
   * has no spec to edit, so configuration lives on the `AgentTemplate` and the
   * `Harness` rather than behind a per-agent settings page.
   */
  /** Where the rail's conversation rows and the list's agent names point. */
  chat?: (ref: ExtensionAgentRef) => string;
  /** Where the rail's "Agent Details" entry points. */
  details?: (ref: ExtensionAgentRef) => string;
}

/** A React context provider the extension wraps the whole app in. */
export type ExtensionProviderComponent = ComponentType<{ children: ReactNode }>;

/**
 * One extension's whole declaration, and the only thing an extension author writes.
 *
 * Everything is optional but `id` and `name`: a config that only contributes a
 * nav item is a complete, valid config.
 *
 * Several of these are installed at once, as the ordered `activeAppExtensions`
 * array. That changes nothing about what one extension may declare — the shape here
 * is the same whether it is installed alone or alongside others — only about how two
 * declarations about the same thing are reconciled. Additive contributions (nav
 * items, routes, slots, form fields, table columns, providers) all take effect, in
 * array order; singular ones (theme, shell, branding, nav overrides, agent links,
 * provider icons, API overrides) are merged field by field with later entries
 * winning. See `merge.ts`.
 */
export interface AppExtensionConfig {
  /** Stable machine identifier, e.g. `"example"`. */
  id: string;
  /** Human-readable name, shown wherever the app names its extension. */
  name: string;
  /** Extra nav entries, positioned among the core items by `order`. */
  navItems?: readonly ExtensionNavItemContribution[];
  /** Whole pages merged into the router. */
  routes?: readonly ExtensionRouteContribution[];
  /** Components mounted at named extension points. */
  slots?: ExtensionSlotComponents;
  /** Extra fields injected into core forms. */
  formFields?: readonly ExtensionFormFieldContribution[];
  /**
   * Extra columns added to core tables — how a product whose domain is wider
   * than the application's shows that dimension on a page the application owns.
   */
  tableColumns?: readonly ExtensionTableColumn<never>[];
  /**
   * Operation overrides, endpoint overrides and payload transforms, keyed by the
   * data layer's own call ids so an override naming a call that does not exist
   * fails to compile.
   */
  api?: ExtensionApi<ApiCallId>;
  /**
   * Extra React context providers wrapped around the app, outermost first.
   * The extension's own query client, feature flags, telemetry, and so on.
   */
  providers?: readonly ExtensionProviderComponent[];
  /**
   * How the application should look. Overriding tokens restyles the
   * application's own components, not merely the extension's.
   */
  theme?: ExtensionTheme;
  /**
   * Whole shell regions replaced outright, for navigation that is a different
   * shape rather than differently styled.
   */
  shell?: ExtensionShell;
  /** The product's own name and mark, wherever the shell states its identity. */
  branding?: ExtensionBranding;
  /**
   * Changes to the application's own navigation entries — hide, rename,
   * re-order, or send one somewhere else. For a product that lists the same
   * pages differently, or that supplies its own version of a destination.
   */
  navOverrides?: ExtensionNavOverrides;
  /**
   * Shell data for the application's *own* routes, keyed by route key (the same
   * keys `replaces` names — see `coreRouteKeys`).
   *
   * The way a product says something about a page it did not contribute. Each
   * entry is attached to that route's `handle` and read back by the product's
   * shell; the application neither defines nor inspects the contents, so a page
   * of its own never carries data only somebody else's chrome would use.
   *
   * A key naming no route is ignored — an entry for a page a future upstream drops
   * costs a dead line, not a crash. Routes the product contributes carry theirs on
   * the contribution itself (`ExtensionRouteContribution.handle`).
   */
  routeHandles?: Readonly<Record<string, ExtensionRouteHandle>>;
  /**
   * Extra or replacement 16×16 provider icons for the model-config form's
   * provider select, keyed by the provider type string (e.g. `"GeminiOnGDC"`).
   *
   * Extension icons are merged on top of the application's own map, so a key that
   * already exists in the core set is replaced, and an unknown key is simply
   * added. Components must take no props.
   */
  providerIcons?: Readonly<Record<string, ComponentType>>;
  /**
   * Agent destinations the application resolves from this configuration.
   */
  agentLinks?: ExtensionAgentLinks;
}
