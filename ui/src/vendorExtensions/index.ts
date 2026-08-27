/**
 * The vendor extension framework's public surface.
 *
 * An extension imports from here and nothing else; everything below this
 * barrel is free to move. Installing an extension is two edits in the host
 * app — build a `VendorExtensionConfig`, then point `activeConfig.ts` at it.
 */

export { VendorExtensionContext } from "./context";
export { VendorExtensionProvider } from "./VendorExtensionProvider";
export { VendorProviders } from "./VendorProviders";
export { VendorSlot } from "./VendorSlot";
export type { VendorSlotProps } from "./VendorSlot";

export {
  useVendorApiExtension,
  useVendorExtensionConfig,
  useVendorFormFields,
  useVendorNavItems,
  useVendorRoutes,
  useVendorSlotComponent,
} from "./hooks";

export {
  EXTENSION_POINT_IDS,
  EXTENSION_POINT_RENDER_MODE,
  isExtensionPointId,
} from "./extensionPoints";
export type {
  ExtensionPointId,
  ExtensionPointProps,
  ExtensionPointRenderMode,
  NoSlotContext,
  VendorSlotComponents,
} from "./extensionPoints";

export { emptyVendorExtensionConfig } from "./types";
export type {
  VendorAgentLinks,
  VendorRouteHandle,
  VendorExtensionConfig,
  VendorNavItemContribution,
  VendorNavItemProps,
  VendorProviderComponent,
  VendorRouteContribution,
} from "./types";

export {
  VENDOR_FORM_IDS,
  applyVendorFieldValues,
  defineVendorFormField,
  initialVendorFieldValues,
  isVendorFormId,
  readVendorFieldValues,
  validateVendorFieldValues,
  vendorFieldsForForm,
} from "./formFields";
export type {
  VendorFormFieldContribution,
  VendorFormFieldProps,
  VendorFormId,
  VendorFormPayload,
} from "./formFields";

export { buildSidebarSections, isNavPathActive } from "./composition";
export type { SidebarSection } from "./composition";

export {
  VendorExtensionConfigError,
  validateVendorExtensionConfig,
} from "./validateConfig";

// The API-layer contract: the declarative shape a vendor's endpoint overrides
// and transforms take in the global config, plus the installer that folds them
// into the data layer's registry. Resolution itself belongs to src/api.
export { installVendorApiExtension } from "./api/installVendorApiExtension";
export type {
  VendorApiExtension,
  VendorEndpointTransform,
} from "./api/apiExtension";

// Restyling and shell replacement: how an extension changes the way the
// application itself looks, rather than only what it adds.
export {
  loadVendorStylesheets,
  resolveAntdTheme,
  resolveAppTheme,
} from "./theme";
export type { VendorTheme, VendorThemeTokens } from "./theme";
export type { VendorShell, VendorSidebarProps } from "./shell";

// Table columns: a contribution that is a heading, a renderer and a position —
// three things a component slot cannot express together.
export {
  VENDOR_TABLE_IDS,
  defineVendorTableColumn,
  isVendorTableId,
  vendorColumnsForTable,
  withVendorColumns,
} from "./tableColumns";
export type { VendorTableColumn, VendorTableId } from "./tableColumns";
export { useVendorTableColumns } from "./hooks";

// Branding: the product's own name and mark, which is identity rather than
// styling and so should not cost a layout replacement.
export { applyVendorDocumentTitle } from "./branding";
export type { VendorAppIconProps, VendorBranding } from "./branding";
export type { VendorLayoutProps } from "./shell";

// Navigation overrides: the other half of contributing an entry — changing one
// the application already has, for a product that lists the same pages
// differently or supplies its own version of a destination.
export { applyNavOverrides } from "./navOverrides";
export type { CoreNavKey, VendorNavOverride, VendorNavOverrides } from "./navOverrides";
