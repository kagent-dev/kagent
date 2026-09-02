import { defined, mergeDefined } from "./merge";
import { extensionColumnsForTable } from "./tableColumns";
import type { ExtensionTableColumn, ExtensionTableId } from "./tableColumns";
import { extensionFieldsForForm } from "./formFields";
import type { ExtensionFormFieldContribution, ExtensionFormId } from "./formFields";
import { mergeExtensionNavOverrides } from "./navOverrides";
import type { ExtensionNavOverrides } from "./navOverrides";
import type { ExtensionBranding } from "./branding";
import type { ExtensionShell } from "./shell";
import type { ExtensionTheme } from "./theme";
import type { ExtensionPointId, ExtensionPointProps } from "./extensionPoints";
import type {
  AppExtensionConfig,
  ExtensionAgentLinks,
  ExtensionNavItemContribution,
  ExtensionRouteContribution,
  ExtensionRouteHandle,
} from "./types";
import type { ExtensionApi } from "./api/extensionApi";
import type { ComponentType } from "react";

/**
 * One installed array, folded down to the one answer each caller needs.
 *
 * Every function here is pure and takes the whole install, which is what keeps the
 * composition rules in one readable place and testable without rendering anything.
 * `hooks.ts` is the React binding over these; the router and the bootstrap read them
 * directly, because they run before there is a provider to read from.
 *
 * Which fold each capability gets — concatenate, or merge with later winning — is
 * explained in `merge.ts`.
 */

/** Every component mounted at `id`, in install order. */
export function extensionSlotComponents<Id extends ExtensionPointId>(
  extensions: readonly AppExtensionConfig[],
  id: Id,
): ComponentType<ExtensionPointProps<Id>>[] {
  // The annotation is the same narrowing the slot map already guarantees:
  // `ExtensionSlotComponents` is keyed by the point id and each value typed
  // against that point's context, but TypeScript will not reduce the indexed
  // access while `Id` is still generic.
  const mounted = extensions.map(
    (extension) =>
      extension.slots?.[id] as ComponentType<ExtensionPointProps<Id>> | undefined,
  );

  return defined(mounted);
}

/** Contributed nav entries from every extension, in `order`. */
export function extensionNavItems(
  extensions: readonly AppExtensionConfig[],
): ExtensionNavItemContribution[] {
  return extensions
    .flatMap((extension) => extension.navItems ?? [])
    .sort((a, b) => a.order - b.order);
}

/** Contributed pages from every extension, in install order. */
export function extensionRoutes(
  extensions: readonly AppExtensionConfig[],
): ExtensionRouteContribution[] {
  return extensions.flatMap((extension) => extension.routes ?? []);
}

/** Shell data for the application's own routes, keyed by route key. */
export function extensionRouteHandles(
  extensions: readonly AppExtensionConfig[],
): Readonly<Record<string, ExtensionRouteHandle>> {
  return mergeDefined(extensions.map((extension) => extension.routeHandles));
}

/** The contributed fields for one form, in render order. */
export function extensionFormFields(
  extensions: readonly AppExtensionConfig[],
  formId: ExtensionFormId,
): ExtensionFormFieldContribution[] {
  return extensionFieldsForForm(
    extensions.flatMap((extension) => extension.formFields ?? []),
    formId,
  );
}

/** The contributed columns for one table, in install order. */
export function extensionTableColumns<TRow>(
  extensions: readonly AppExtensionConfig[],
  tableId: ExtensionTableId,
): ExtensionTableColumn<TRow>[] {
  return extensionColumnsForTable<TRow>(
    extensions.flatMap((extension) => extension.tableColumns ?? []),
    tableId,
  );
}

/** Every extension's app-level context providers, outermost first. */
export function extensionProviders(
  extensions: readonly AppExtensionConfig[],
): ComponentType<{ children: React.ReactNode }>[] {
  return extensions.flatMap((extension) => extension.providers ?? []);
}

/** Each extension's API overrides, in install order. */
export function extensionApis(
  extensions: readonly AppExtensionConfig[],
): ExtensionApi[] {
  return defined(extensions.map((extension) => extension.api));
}

/** Each extension's theme, in install order. */
export function extensionThemes(
  extensions: readonly AppExtensionConfig[],
): ExtensionTheme[] {
  return defined(extensions.map((extension) => extension.theme));
}

/** The shell regions the install replaces, later extensions winning per region. */
export function extensionShell(
  extensions: readonly AppExtensionConfig[],
): ExtensionShell {
  return mergeDefined(extensions.map((extension) => extension.shell));
}

/** The identity the shell should state, later extensions winning per field. */
export function extensionBranding(
  extensions: readonly AppExtensionConfig[],
): ExtensionBranding {
  return mergeDefined(extensions.map((extension) => extension.branding));
}

/** Changes to the application's own nav entries, from every extension. */
export function extensionNavOverrides(
  extensions: readonly AppExtensionConfig[],
): ExtensionNavOverrides {
  return mergeExtensionNavOverrides(
    extensions.map((extension) => extension.navOverrides),
  );
}

/** Agent destinations, later extensions winning per link. */
export function extensionAgentLinks(
  extensions: readonly AppExtensionConfig[],
): ExtensionAgentLinks {
  return mergeDefined(extensions.map((extension) => extension.agentLinks));
}

/** Provider icons from every extension, later ones replacing earlier keys. */
export function extensionProviderIcons(
  extensions: readonly AppExtensionConfig[],
): Readonly<Record<string, ComponentType>> {
  return mergeDefined(extensions.map((extension) => extension.providerIcons));
}
