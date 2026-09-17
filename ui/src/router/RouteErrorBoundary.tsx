import { Button, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { TriangleAlert } from "lucide-react";
import { useNavigate, useRouteError } from "react-router-dom";

const { Text, Title } = Typography;

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  return "An unexpected error occurred.";
}

/**
 * A page crashed. `navigate(0)` re-runs the current route the same way React
 * Router's own docs do, which also clears this error state; going back is the
 * other way out when reloading the same page will not help.
 */
export function RouteErrorBoundary() {
  const theme = useTheme();
  const error = useRouteError();
  const navigate = useNavigate();

  console.error("Route crashed", error);

  return (
    <div
      data-testid="route-error"
      css={{
        display: "grid",
        gap: theme.space(4),
        maxWidth: 480,
        marginInline: "auto",
        paddingBlock: theme.space(10),
      }}
    >
      <Space size={12} align="start">
        <TriangleAlert size={28} color={theme.color.textMuted} aria-hidden />
        <div css={{ display: "grid", gap: theme.space(2) }}>
          <Title level={3} css={{ margin: 0 }}>
            Something went wrong
          </Title>
          <Text css={{ color: theme.color.textMuted }}>{errorMessage(error)}</Text>
        </div>
      </Space>
      <Space size={8}>
        <Button type="primary" onClick={() => navigate(0)}>
          Retry
        </Button>
        <Button onClick={() => navigate(-1)}>Go back</Button>
      </Space>
    </div>
  );
}
