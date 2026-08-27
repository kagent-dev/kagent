import { useMemo } from "react";
import { ConfigProvider, theme as antdAlgorithm } from "antd";
import { Global, ThemeProvider } from "@emotion/react";
import { RouterProvider } from "react-router-dom";
import { Toaster } from "react-hot-toast";
import { SWRConfig } from "swr";
import { GlobalStyles } from "./theme/GlobalStyles";
import { ThemeModeProvider, useThemeMode } from "./theme/themeMode";
import { resolveAntdTheme, resolveAppTheme } from "./vendorExtensions/theme";
import { createAppRouter, reservedRoutePaths } from "./router/router";
import { VendorExtensionProvider, VendorProviders } from "./vendorExtensions";
import { useVendorExtensionConfig } from "./vendorExtensions";
import { activeVendorExtensionConfig } from "./vendorExtensions/activeConfig";
// Side effect: registers the extension's API overrides before the first
// request, which can be issued during the first render.
import "./vendorExtensions/installActiveExtension";

/**
 * Builds the router from the config in context, once. Separate from `App` so
 * it sits inside `VendorExtensionProvider` and can read the vendor's routes.
 */
function AppRouter() {
  const config = useVendorExtensionConfig();
  const router = useMemo(() => createAppRouter(config), [config]);

  return <RouterProvider router={router} />;
}

/**
 * Everything below the theme, once a mode is known.
 *
 * Split out so it can read the mode from context: both the Emotion tokens and the
 * component library's algorithm depend on it, and neither can be resolved above the
 * provider that decides it.
 */
function ThemedApp() {
  const { mode } = useThemeMode();

  // Resolved from the installed config: an extension's tokens restyle the
  // application's own components, so this has to wrap everything that renders.
  const vendorTheme = activeVendorExtensionConfig.theme;
  const resolvedTheme = resolveAppTheme(vendorTheme, mode);
  const resolvedAntd = resolveAntdTheme(vendorTheme, mode);

  return (
    <VendorExtensionProvider
      config={activeVendorExtensionConfig}
      reservedPaths={reservedRoutePaths}
    >
      <ConfigProvider
        theme={{
          ...resolvedAntd,
          // The algorithm follows the mode, not the other way round: it decides
          // every colour the library derives rather than the ones named above, so
          // pinning it dark would leave a light theme with dark inputs and menus.
          algorithm:
            mode === "light"
              ? antdAlgorithm.defaultAlgorithm
              : antdAlgorithm.darkAlgorithm,
        }}
      >
        <ThemeProvider theme={resolvedTheme}>
          <GlobalStyles />
          {/* After the application's own global styles, so an extension wins on
              ties — that is the point of letting it supply any. */}
          {vendorTheme?.globalStyles ? (
            <Global styles={vendorTheme.globalStyles} />
          ) : null}
          {/* Vendor providers sit inside the app's own theming so they can read
              it, and outside the router so they survive navigation. */}
          <VendorProviders>
            <SWRConfig
              value={{ revalidateOnFocus: false, shouldRetryOnError: false }}
            >
              <AppRouter />
              <Toaster position="bottom-right" />
            </SWRConfig>
          </VendorProviders>
        </ThemeProvider>
      </ConfigProvider>
    </VendorExtensionProvider>
  );
}

export function App() {
  return (
    <ThemeModeProvider
      supportedModes={activeVendorExtensionConfig.theme?.supportedModes}
    >
      <ThemedApp />
    </ThemeModeProvider>
  );
}
