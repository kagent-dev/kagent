import { useEffect } from "react";
import { Button, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { TriangleAlert } from "lucide-react";
import { useNavigate, useRouteError } from "react-router-dom";
import { ChevronRight } from "lucide-react";
import { formatError } from "@/components/common/formatError";

const { Text, Title } = Typography;

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  return "An unexpected error occurred.";
}

/**
 * A page crashed. Retry reloads the document, which is what clears the error
 * state the router is holding; going back is the other way out when loading the
 * same page again will not help.
 */
export function RouteErrorBoundary() {
  const theme = useTheme();
  const error = useRouteError();
  const navigate = useNavigate();

  useEffect(() => {
    console.error("Route crashed", error);
  }, [error]);

  const details = formatError(error);

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
      {details === errorMessage(error) ? null : (
        <details
          css={{
            borderTop: `1px solid ${theme.color.border}`,
            paddingTop: theme.space(3),
            "& > summary": {
              display: "flex",
              alignItems: "center",
              gap: theme.space(2),
              color: theme.color.primaryText,
              fontSize: 13,
              listStyle: "none",
              cursor: "pointer",
              userSelect: "none",
            },
            "& > summary::-webkit-details-marker": { display: "none" },
            "& > summary:hover": { opacity: 0.8 },
            "& > summary:active": { opacity: 0.65 },
            "& > summary:focus-visible": {
              outline: `2px solid ${theme.color.primaryText}`,
              outlineOffset: 3,
              borderRadius: 4,
            },
            "&[open] .routeError-chevron": { transform: "rotate(90deg)" },
          }}
        >
          <summary>
            <ChevronRight
              className="routeError-chevron"
              size={13}
              css={{ transition: "transform 150ms ease" }}
              aria-hidden
            />
            Error details
          </summary>
          <pre
            css={{
              margin: `${theme.space(3)}px 0 0`,
              padding: theme.space(3),
              border: `1px solid ${theme.color.border}`,
              borderRadius: 8,
              background: theme.color.bg,
              overflow: "auto",
              maxHeight: "40vh",
            }}
          >
            <code
              data-testid="route-error-details"
              css={{
                fontFamily: theme.font.mono,
                fontSize: 12,
                lineHeight: 1.6,
                color: theme.color.text,
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
              }}
            >
              {details}
            </code>
          </pre>
        </details>
      )}
    </div>
  );
}
