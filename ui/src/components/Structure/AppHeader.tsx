import { Button, Layout, Space, Tooltip, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Link, useNavigate } from "react-router-dom";
import { UserRound } from "lucide-react";
import { KagentLogoWithText } from "@/components/branding/KagentLogo";
import { paths } from "@/router/routes";
import { ExtensionSlot, useExtensionBranding } from "@/appExtensions";
import { useAuth } from "@/auth";

const { Text } = Typography;

const { Header } = Layout;

/*
 * There is no Create menu here.
 *
 * Creating a thing is an action on the list of those things, so each list page carries
 * its own — `/agents` offers New agent, `/models` offers New model, and so on. A menu in
 * the header offered the same four routes from every page in the product, including the
 * three pages where creating any of them was beside the point.
 *
 * It also could not be relied on. The header belongs to the default shell, so a
 * distribution supplying its own layout inherited none of it, and a create route reachable
 * only from the header was reachable only by typing the URL. An action on the list belongs
 * to the page, which survives whatever frames it.
 */

/**
 * Who is signed in.
 *
 * Renders nothing at all when the deployment has no auth proxy in front —
 * showing "not signed in" there would imply a sign-in step that does not exist
 * and cannot be completed.
 */
function CurrentUserBadge() {
  const theme = useTheme();
  const navigate = useNavigate();
  const { status, user } = useAuth();

  if (status === "expired") {
    return (
      <Button
        size="small"
        data-testid="header-reauth"
        onClick={() => navigate(paths.login)}
      >
        Session expired — sign in
      </Button>
    );
  }

  if (status !== "authenticated" || !user) return null;

  return (
    <Tooltip title={user.email ?? user.id}>
      <Space
        size={6}
        data-testid="header-user"
        css={{ color: theme.color.textMuted, fontSize: 13 }}
      >
        <UserRound size={14} />
        <Text css={{ color: "inherit" }}>{user.displayName}</Text>
      </Space>
    </Tooltip>
  );
}

export function AppHeader() {
  const theme = useTheme();
  const branding = useExtensionBranding();
  const AppIcon = branding?.AppIcon;

  return (
    <Header
      data-testid="app-header"
      css={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        height: theme.layout.headerHeight,
        paddingInline: theme.space(4),
        borderBottom: `1px solid ${theme.color.border}`,
        // Held at the top while the page scrolls under it. The wordmark is also the
        // link home, so scrolling away took the one always-available way back with it
        // — and left the rail beside it starting partway down the window.
        position: "sticky",
        top: 0,
        // Above the rail, which is sticky too and would otherwise draw over the
        // header's lower edge as it passes.
        zIndex: 2,
      }}
    >
      <Link
        to={paths.dashboard}
        data-testid="app-logo"
        aria-label="kagent — dashboard"
        // The wordmark navigates, so it says so under the pointer. It reads as a logo
        // rather than a control, which is exactly why it needs the states: without them
        // the one piece of chrome every page shows gives no sign it can be clicked, and
        // a reader who tries it cannot tell a slow route change from nothing happening.
        //
        // A tinted plate behind it rather than a colour change on the mark itself: the
        // mark is an SVG a distribution may replace with its own, and dimming somebody
        // else's logo on hover is not this component's business.
        css={{
          display: "flex",
          alignItems: "center",
          // Sized to the mark, with the plate extending a little past it.
          padding: `${theme.space(1)} ${theme.space(2)}`,
          marginInlineStart: `-${theme.space(2)}`,
          borderRadius: theme.radius.md,
          background: "transparent",
          transition: "background 120ms ease, transform 120ms ease",
          "&:hover": { background: theme.color.border },
          // Pressed moves it down a pixel — the same acknowledgement a button gives,
          // and the part that distinguishes "I clicked" from "I hovered".
          "&:active": { transform: "translateY(1px)" },
          "&:focus-visible": {
            outline: `2px solid ${theme.color.primary}`,
            outlineOffset: 2,
          },
        }}
      >
        {/* A distribution shipping under its own name says so here; otherwise the
            project's own wordmark, sized by height so the aspect ratio holds. */}
        {AppIcon ? (
          <AppIcon collapsed={false} />
        ) : (
          <KagentLogoWithText css={{ height: 26, width: "auto", display: "block" }} />
        )}
      </Link>

      <Space size="middle">
        {/* Ahead of the app's own header controls, which is where a deployment
            puts something that scopes the whole page — an active-cluster or
            active-tenant selector, for instance. */}
        <ExtensionSlot id="app_shell_appHeader_actions_leading" />
        <CurrentUserBadge />
      </Space>
    </Header>
  );
}
