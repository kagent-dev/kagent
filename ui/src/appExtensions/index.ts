/**
 * The app extension framework's public surface.
 *
 * An extension imports from here and nothing else; everything below this barrel is
 * free to move. Installing one is two edits in the host app — build an
 * `AppExtensionConfig`, then add it to the array in `activeExtensions.ts`.
 */

export { AppExtensionContext, NO_APP_EXTENSIONS } from "./context";
export { AppExtensionsProvider } from "./AppExtensionsProvider";
export { ExtensionProviders } from "./ExtensionProviders";
export { ExtensionSlot } from "./ExtensionSlot";
export type { ExtensionSlotProps } from "./ExtensionSlot";

export {
  useAppExtensions,
  useExtensionAgentLinks,
  useExtensionApis,
  useExtensionBranding,
  useExtensionFormFields,
  useExtensionNavItems,
  useExtensionNavOverrides,
  useExtensionProviderIcons,
  useExtensionRoutes,
  useExtensionShell,
  useExtensionSlotComponents,
  useExtensionTableColumns,
} from "./hooks";

// The pure folds the hooks are built on. Exported because the router and the
// bootstrap read them directly: both run before there is a provider to read from.
export {
  extensionAgentLinks,
  extensionApis,
  extensionBranding,
  extensionFormFields,
  extensionNavItems,
  extensionNavOverrides,
  extensionProviderIcons,
  extensionProviders,
  extensionRouteHandles,
  extensionRoutes,
  extensionShell,
  extensionSlotComponents,
  extensionTableColumns,
  extensionThemes,
} from "./selectors";
export { defined, mergeDefined } from "./merge";

export {
  EXTENSION_POINT_IDS,
  EXTENSION_POINT_RENDER_MODE,
  isExtensionPointId,
} from "./extensionPoints";
export type {
  ExtensionPointId,
  ExtensionPointProps,
  ExtensionPointRenderMode,
  ExtensionSlotComponents,
  NoSlotContext,
} from "./extensionPoints";

export type {
  AppExtensionConfig,
  ExtensionAgentLinks,
  ExtensionAgentRef,
  ExtensionNavItemContribution,
  ExtensionNavItemProps,
  ExtensionProviderComponent,
  ExtensionRouteContribution,
  ExtensionRouteHandle,
} from "./types";

export {
  EXTENSION_FORM_IDS,
  applyExtensionFieldValues,
  defineExtensionFormField,
  extensionFieldsForForm,
  initialExtensionFieldValues,
  isExtensionFormId,
  readExtensionFieldValues,
  validateExtensionFieldValues,
} from "./formFields";
export type {
  ExtensionFormFieldContribution,
  ExtensionFormFieldProps,
  ExtensionFormId,
  ExtensionFormPayload,
} from "./formFields";

export { buildSidebarSections, isNavPathActive } from "./composition";
export type { SidebarSection } from "./composition";

// One deployment setting, by name. An extension's own settings are named
// `EXTENSION_*` and are not the application's business, so this takes any key and
// a fallback rather than a union of keys it knows about.
export { readEnv } from "@/env";

// The palette currently showing. Re-exported because a contribution that wants to
// look like the application's own chrome has to pick the same one — antd's Menu
// and Table both take a light/dark choice that no design token can stand in for.
export { useThemeMode } from "@/theme/themeMode";
export type { ThemeMode } from "@/theme/theme";

export {
  AppExtensionConfigError,
  validateAppExtensions,
  validateExtensionConfig,
} from "./validateConfig";

// The API-layer contract: declarative operation overrides and transforms, plus
// the installers that fold them into the data layer's registry.
export {
  installExtensionApi,
  installExtensionApis,
} from "./api/installExtensionApi";
export type { ExtensionApi, ExtensionEndpointTransform } from "./api/extensionApi";

// Restyling and shell replacement: how an extension changes the way the
// application itself looks, rather than only what it adds.
export {
  loadExtensionStylesheets,
  resolveAntdTheme,
  resolveAppTheme,
  resolveSupportedModes,
} from "./theme";
export type { ExtensionTheme, ExtensionThemeTokens } from "./theme";
export type {
  ExtensionLayoutProps,
  ExtensionShell,
  ExtensionSidebarProps,
} from "./shell";

// Table columns: a contribution that is a heading, a renderer and a position —
// three things a component slot cannot express together.
export {
  EXTENSION_TABLE_IDS,
  defineExtensionTableColumn,
  extensionColumnsForTable,
  isExtensionTableId,
  withExtensionColumns,
} from "./tableColumns";
export type { ExtensionTableColumn, ExtensionTableId } from "./tableColumns";

// Branding: the product's own name and mark, which is identity rather than
// styling and so should not cost a layout replacement.
export { applyExtensionBranding } from "./branding";
export type { ExtensionAppIconProps, ExtensionBranding } from "./branding";

// Navigation overrides: the other half of contributing an entry — changing one
// the application already has, for a product that lists the same pages
// differently or supplies its own version of a destination.
export { applyNavOverrides, mergeExtensionNavOverrides } from "./navOverrides";
export type {
  CoreNavKey,
  ExtensionNavOverride,
  ExtensionNavOverrides,
} from "./navOverrides";
