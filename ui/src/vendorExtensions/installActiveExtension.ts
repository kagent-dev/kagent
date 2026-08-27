import { activeVendorExtensionConfig } from "./activeConfig";
import { installVendorApiExtension } from "./api/installVendorApiExtension";

/**
 * Installs the active extension's API overrides into the data layer.
 *
 * A module side effect, imported for its effect from `App.tsx`, because the
 * registry has to be populated before the first request goes out — which can
 * happen during the very first render, ahead of any effect.
 */
installVendorApiExtension(activeVendorExtensionConfig.api);
