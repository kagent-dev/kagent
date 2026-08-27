import { useContext, useMemo } from "react";
import { VendorExtensionContext } from "./context";
import { vendorColumnsForTable } from "./tableColumns";
import type { VendorTableColumn, VendorTableId } from "./tableColumns";
import { vendorFieldsForForm } from "./formFields";
import type { VendorFormFieldContribution, VendorFormId } from "./formFields";
import type { ExtensionPointId, ExtensionPointProps } from "./extensionPoints";
import type {
  VendorExtensionConfig,
  VendorNavItemContribution,
  VendorRouteContribution,
} from "./types";
import type { VendorApiExtension } from "./api/apiExtension";
import type { ComponentType } from "react";

/** The whole global config. */
export function useVendorExtensionConfig(): VendorExtensionConfig {
  return useContext(VendorExtensionContext);
}

/**
 * The component mounted at `id`, or `undefined` when nothing is. Prefer
 * `<VendorSlot>`; reach for this when the surrounding markup should also
 * disappear along with the component.
 */
export function useVendorSlotComponent<Id extends ExtensionPointId>(
  id: Id,
): ComponentType<ExtensionPointProps<Id>> | undefined {
  const { slots } = useVendorExtensionConfig();
  return slots?.[id];
}

/** Vendor nav contributions, in `order`. */
export function useVendorNavItems(): readonly VendorNavItemContribution[] {
  const { navItems } = useVendorExtensionConfig();
  return useMemo(
    () => [...(navItems ?? [])].sort((a, b) => a.order - b.order),
    [navItems],
  );
}

/** Vendor page contributions. */
export function useVendorRoutes(): readonly VendorRouteContribution[] {
  return useVendorExtensionConfig().routes ?? [];
}

/** The vendor fields for one form, in render order. */
export function useVendorFormFields(
  formId: VendorFormId,
): VendorFormFieldContribution[] {
  const { formFields } = useVendorExtensionConfig();
  return useMemo(
    () => vendorFieldsForForm(formFields, formId),
    [formFields, formId],
  );
}

/** The API overrides, for the fetch layer to resolve endpoints against. */
export function useVendorApiExtension(): VendorApiExtension | undefined {
  return useVendorExtensionConfig().api;
}

/**
 * Columns an extension contributes to one of the application's tables.
 *
 * Returns a stable empty array when nothing is installed, so a page can fold the
 * result in unconditionally.
 */
export function useVendorTableColumns<TRow>(
  tableId: VendorTableId,
): VendorTableColumn<TRow>[] {
  const { tableColumns } = useVendorExtensionConfig();
  return useMemo(
    () => vendorColumnsForTable<TRow>(tableColumns, tableId),
    [tableColumns, tableId],
  );
}
