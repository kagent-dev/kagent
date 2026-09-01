import { Button, Card, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useNavigate } from "react-router-dom";
import { paths } from "@/router/routes";
import { reauthenticationUrl } from "@/auth/reauthenticate";
import { useAuth } from "@/auth";

const { Title, Paragraph, Text } = Typography;

/**
 * The sign-in page, which does something different in each of the three auth
 * states — and in two of them does not sign anyone in.
 *
 * The redirect to the identity provider is only ever offered when a proxy has
 * actually answered and rejected the session. With no proxy in front there is
 * nothing at `/oauth2/start` to redirect to, so the button enters the app
 * instead; that is what stops a local or unsecured deployment bouncing between
 * this page and a 404. The redirect is also user-initiated rather than
 * automatic, so even a misconfigured proxy cannot put the browser in a loop.
 */
export function LoginPage() {
  const theme = useTheme();
  const navigate = useNavigate();
  const { status, user } = useAuth();

  const enterApp = () => navigate(paths.dashboard);
  // Carries `rd`, so signing in returns the reader to where they were rather than to
  // whatever the proxy defaults to. Dropping it is how re-authenticating used to cost
  // somebody the page they were reading.
  const startSso = () => {
    window.location.assign(reauthenticationUrl(window.location));
  };

  const copy =
    status === "expired"
      ? {
          blurb: "Your session has expired. Sign in again to continue.",
          action: "Sign in with SSO",
          onClick: startSso,
        }
      : status === "authenticated"
        ? {
            blurb: `Signed in as ${user?.displayName ?? "your account"}.`,
            action: "Continue",
            onClick: enterApp,
          }
        : {
            blurb: "No authentication proxy is configured for this deployment.",
            action: "Continue",
            onClick: enterApp,
          };

  return (
    <div
      data-testid="login-page"
      css={{
        minHeight: "100vh",
        display: "grid",
        placeItems: "center",
        padding: theme.space(6),
      }}
    >
      <Card css={{ width: 360, textAlign: "center" }}>
        <Title level={3}>kagent</Title>
        <Paragraph css={{ color: theme.color.textMuted }}>{copy.blurb}</Paragraph>
        <Button
          type="primary"
          block
          data-testid="login-submit"
          data-auth-status={status}
          onClick={copy.onClick}
        >
          {copy.action}
        </Button>
        {status === "unsecured" ? (
          <Text
            data-testid="login-unsecured-note"
            css={{
              display: "block",
              marginTop: theme.space(3),
              fontSize: 12,
              color: theme.color.textMuted,
            }}
          >
            Anyone who can reach this page can use it.
          </Text>
        ) : null}
      </Card>
    </div>
  );
}
