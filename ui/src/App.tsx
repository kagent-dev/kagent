import { useMemo } from "react";
import { ConfigProvider, theme as antdAlgorithm } from "antd";
import { Global, ThemeProvider } from "@emotion/react";
import { RouterProvider } from "react-router-dom";
import { Toaster } from "react-hot-toast";
import { SWRConfig } from "swr";
import { GlobalStyles } from "./theme/GlobalStyles";
import { ThemeModeProvider } from "./theme/themeMode";
import { useThemeMode } from "./theme/useThemeMode";
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
  // A step above the elevated surface on dark, where the card colour sits too close to the page.
  const toastSurface =
    mode === "light"
      ? resolvedTheme.color.bgElevated
      : `color-mix(in srgb, ${resolvedTheme.color.text} 7%, ${resolvedTheme.color.bgElevated})`;

  return (
    <AppExtensionsProvider
      extensions={activeAppExtensions}
      reservedPaths={reservedRoutePaths}
    >
      <ConfigProvider
        modal={{ centered: true }}
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
              <Toaster
                position="bottom-right"
                toastOptions={{
                  style: {
                    // Dark lifts the toast off the page with a lighter surface, a visible edge and a deep shadow.
                    background: toastSurface,
                    color: resolvedTheme.color.text,
                    border: `1px solid ${mode === "light" ? resolvedTheme.color.border : `color-mix(in srgb, ${resolvedTheme.color.borderStrong} 70%, ${toastSurface})`}`,
                    borderRadius: resolvedTheme.radius.md,
                    boxShadow:
                      mode === "light"
                        ? "0 6px 20px rgba(21, 25, 39, 0.12)"
                        : "0 12px 32px rgba(0, 0, 0, 0.6)",
                    padding: "10px 14px",
                    fontFamily: resolvedTheme.font.body,
                    fontSize: 14,
                  },
                  success: {
                    iconTheme: {
                      primary: mode === "light" ? resolvedTheme.color.success : resolvedTheme.color.successText,
                      secondary: toastSurface,
                    },
                  },
                  error: {
                    iconTheme: {
                      primary: mode === "light" ? resolvedTheme.color.danger : resolvedTheme.color.dangerText,
                      secondary: toastSurface,
                    },
                  },
                }}
              />
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
