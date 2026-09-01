import { useContext, useMemo } from "react";
import { AppExtensionContext } from "./context";
import {
  extensionAgentLinks,
  extensionApis,
  extensionBranding,
  extensionFormFields,
  extensionNavItems,
  extensionNavOverrides,
  extensionProviderIcons,
  extensionRoutes,
  extensionShell,
  extensionSlotComponents,
  extensionTableColumns,
} from "./selectors";
import type { ExtensionTableColumn, ExtensionTableId } from "./tableColumns";
import type { ExtensionFormFieldContribution, ExtensionFormId } from "./formFields";
import type { ExtensionNavOverrides } from "./navOverrides";
import type { ExtensionBranding } from "./branding";
import type { ExtensionShell } from "./shell";
import type { ExtensionPointId, ExtensionPointProps } from "./extensionPoints";
import type {
  AppExtensionConfig,
  ExtensionAgentLinks,
  ExtensionNavItemContribution,
  ExtensionRouteContribution,
} from "./types";
import type { ExtensionApi } from "./api/extensionApi";
import type { ComponentType } from "react";

/**
 * The React binding over `selectors.ts`.
 *
 * Each hook is the same two lines — read the install from context, fold it with the
 * selector that owns that capability's composition rule — so no page ever loops over
 * the installed extensions itself, and adding an extension changes nothing at any
 * call site.
 */

/** The installed extensions, in order. */
export function useAppExtensions(): readonly AppExtensionConfig[] {
  return useContext(AppExtensionContext);
}

/**
 * Every component mounted at `id`, in install order, or an empty array when none
 * is. Prefer `<ExtensionSlot>`; reach for this when the surrounding markup should
 * also disappear along with the components.
 */
export function useExtensionSlotComponents<Id extends ExtensionPointId>(
  id: Id,
): ComponentType<ExtensionPointProps<Id>>[] {
  const extensions = useAppExtensions();
  return useMemo(() => extensionSlotComponents(extensions, id), [extensions, id]);
}

/** Contributed nav entries from every extension, in `order`. */
export function useExtensionNavItems(): readonly ExtensionNavItemContribution[] {
  const extensions = useAppExtensions();
  return useMemo(() => extensionNavItems(extensions), [extensions]);
}

/** Contributed pages from every extension, in install order. */
export function useExtensionRoutes(): readonly ExtensionRouteContribution[] {
  const extensions = useAppExtensions();
  return useMemo(() => extensionRoutes(extensions), [extensions]);
}

/** The contributed fields for one form, in render order. */
export function useExtensionFormFields(
  formId: ExtensionFormId,
): ExtensionFormFieldContribution[] {
  const extensions = useAppExtensions();
  return useMemo(() => extensionFormFields(extensions, formId), [extensions, formId]);
}

/**
 * Columns contributed to one of the application's tables.
 *
 * Returns a stable empty array when nothing is installed, so a page can fold the
 * result in unconditionally.
 */
export function useExtensionTableColumns<TRow>(
  tableId: ExtensionTableId,
): ExtensionTableColumn<TRow>[] {
  const extensions = useAppExtensions();
  return useMemo(
    () => extensionTableColumns<TRow>(extensions, tableId),
    [extensions, tableId],
  );
}

/** Each extension's API overrides, for the fetch layer to resolve calls against. */
export function useExtensionApis(): readonly ExtensionApi[] {
  const extensions = useAppExtensions();
  return useMemo(() => extensionApis(extensions), [extensions]);
}

/** The shell regions the install replaces, later extensions winning per region. */
export function useExtensionShell(): ExtensionShell {
  const extensions = useAppExtensions();
  return useMemo(() => extensionShell(extensions), [extensions]);
}

/** The identity the shell should state, later extensions winning per field. */
export function useExtensionBranding(): ExtensionBranding {
  const extensions = useAppExtensions();
  return useMemo(() => extensionBranding(extensions), [extensions]);
}

/** Changes to the application's own nav entries, from every extension. */
export function useExtensionNavOverrides(): ExtensionNavOverrides {
  const extensions = useAppExtensions();
  return useMemo(() => extensionNavOverrides(extensions), [extensions]);
}

/** Agent destinations, later extensions winning per link. */
export function useExtensionAgentLinks(): ExtensionAgentLinks {
  const extensions = useAppExtensions();
  return useMemo(() => extensionAgentLinks(extensions), [extensions]);
}

/** Provider icons from every extension, later ones replacing earlier keys. */
export function useExtensionProviderIcons(): Readonly<
  Record<string, ComponentType>
> {
  const extensions = useAppExtensions();
  return useMemo(() => extensionProviderIcons(extensions), [extensions]);
}
