import { emptyVendorExtensionConfig } from "./types";
import type { VendorExtensionConfig } from "./types";
import { exampleVendorExtension } from "./example/exampleExtension";

/**
 * The one global config the app boots with.
 *
 * Installing a vendor extension is this single edit: import its config object and
 * export it from here. Everything the extension contributes arrives through the
 * extension points, so no other module has to know it exists — which is what lets
 * a build that installs one take changes from upstream as merges.
 *
 * `VITE_VENDOR_EXTENSIONS` selects a different config for the tests that need one:
 * `example` for the framework's own extension-point specs, `none` for the specs that
 * assert the application is bare.
 */
export const activeVendorExtensionConfig: VendorExtensionConfig =
  import.meta.env.VITE_VENDOR_EXTENSIONS === "example"
    ? exampleVendorExtension
    : emptyVendorExtensionConfig;
