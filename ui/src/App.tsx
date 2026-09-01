import { useMemo } from "react";
import { ConfigProvider, theme as antdAlgorithm } from "antd";
import { Global, ThemeProvider } from "@emotion/react";
import { RouterProvider } from "react-router-dom";
import { Toaster } from "react-hot-toast";
import { SWRConfig } from "swr";
import { GlobalStyles } from "./theme/GlobalStyles";
import { ThemeModeProvider, useThemeMode } from "./theme/themeMode";
import {
  resolveAntdTheme,
  resolveAppTheme,
  resolveSupportedModes,
} from "./appExtensions/theme";
import { createAppRouter, reservedRoutePaths } from "./router/router";
import { AppExtensionsProvider, ExtensionProviders } from "./appExtensions";
import { extensionThemes, useAppExtensions } from "./appExtensions";
import { activeAppExtensions } from "./appExtensions/activeExtensions";
// Side effect: registers the extensions' API overrides before the first
// request, which can be issued during the first render.
import "./appExtensions/installActiveExtensions";

/**
 * The installed extensions' themes, resolved once.
 *
 * Module scope rather than a hook: the install is fixed at build time, so this
 * cannot change while the app is open, and computing it here keeps the identity
 * stable for the memos below.
 */
const themes = extensionThemes(activeAppExtensions);

/**
 * Builds the router from the install in context, once. Separate from `App` so
 * it sits inside `AppExtensionsProvider` and can read the contributed routes.
 */
function AppRouter() {
  const extensions = useAppExtensions();
  const router = useMemo(() => createAppRouter(extensions), [extensions]);

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

  // Resolved from the whole install: an extension's tokens restyle the
  // application's own components, so this has to wrap everything that renders.
  const resolvedTheme = resolveAppTheme(themes, mode);
  const resolvedAntd = resolveAntdTheme(themes, mode);

  return (
    <AppExtensionsProvider
      extensions={activeAppExtensions}
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
              ties — that is the point of letting it supply any. Emitted in install
              order, so the later extension wins a tie between two of them for the
              same reason. */}
          {themes.map((theme, index) =>
            theme.globalStyles ? (
              <Global key={index} styles={theme.globalStyles} />
            ) : null,
          )}
          {/* Extension providers sit inside the app's own theming so they can read
              it, and outside the router so they survive navigation. */}
          <ExtensionProviders>
            <SWRConfig
              value={{ revalidateOnFocus: false, shouldRetryOnError: false }}
            >
              <AppRouter />
              <Toaster position="bottom-right" />
            </SWRConfig>
          </ExtensionProviders>
        </ThemeProvider>
      </ConfigProvider>
    </AppExtensionsProvider>
  );
}

export function App() {
  return (
    <ThemeModeProvider supportedModes={resolveSupportedModes(themes)}>
      <ThemedApp />
    </ThemeModeProvider>
  );
}
