import { activeAppExtensions } from "./activeExtensions";
import { installExtensionApis } from "./api/installExtensionApi";

/**
 * Installs the active extensions' API overrides into the data layer.
 *
 * A module side effect, imported for its effect from `App.tsx`, because the
 * registry has to be populated before the first request goes out — which can
 * happen during the very first render, ahead of any effect.
 */
installExtensionApis(activeAppExtensions);
