import { Button, Space, Typography } from "antd";
import { Compass } from "lucide-react";
import { useTheme } from "@emotion/react";
import { Link, useLocation } from "react-router-dom";
import { agentTemplatesTab, paths } from "@/router/routes";

const { Text, Title } = Typography;

/**
 * An address this application does not serve.
 *
 * ## What it says, and why
 *
 * **The address it tried.** A reader arrives here from a stale bookmark, a link
 * somebody sent, or a typed URL, and "that page does not exist" tells them nothing
 * they can act on — they cannot see which of those it was without reading the address
 * bar themselves. Showing the path back is how they recognise a truncated link or a
 * copy-paste that lost a segment.
 *
 * **Somewhere to go, not one button back.** The old page offered a single "Back to
 * dashboard", which is the right destination only if that is where they were headed.
 * The entries below are the things this application actually has, so a reader whose
 * link went stale can carry on rather than start over.
 *
 * It deliberately does not guess what they meant. Suggesting a nearest match is a
 * plausible-looking answer that is wrong often enough to send somebody to the wrong
 * agent, and this application does not put a guess where it can put a fact.
 */
export function NotFoundPage() {
  const theme = useTheme();
  const location = useLocation();

  const destinations = [
    { to: paths.agents, label: "Agents", hint: "and the conversations people have had with them" },
    { to: agentTemplatesTab, label: "Agent templates", hint: "what an agent does" },
    { to: paths.models, label: "Model configurations", hint: "what an agent thinks with" },
    { to: paths.mcpServers, label: "MCP servers", hint: "the tools agents can reach" },
    { to: paths.prompts, label: "Prompts", hint: "reusable prompt libraries" },
  ];

  return (
    <div
      data-testid="not-found"
      css={{
        display: "grid",
        gap: theme.space(5),
        maxWidth: 640,
        marginInline: "auto",
        paddingBlock: theme.space(10),
      }}
    >
      <Space size={12} align="start">
        <Compass size={28} color={theme.color.textMuted} aria-hidden />
        <div css={{ display: "grid", gap: theme.space(2) }}>
          <Title level={3} css={{ margin: 0 }}>
            There is no page at this address
          </Title>
          {/* The path, wrapped rather than truncated: the whole point is that a reader
              can compare it with the link they followed, and an elided middle is where
              the mistake usually is. */}
          <Text
            data-testid="not-found-path"
            css={{
              fontFamily: theme.font.mono,
              fontSize: 13,
              color: theme.color.textMuted,
              wordBreak: "break-all",
            }}
          >
            {location.pathname}
          </Text>
        </div>
      </Space>

      <div css={{ display: "grid", gap: theme.space(3) }}>
        <Text
          css={{
            color: theme.color.textMuted,
            fontSize: 11,
            textTransform: "uppercase",
            letterSpacing: 0.6,
          }}
        >
          Where you can go
        </Text>
        {destinations.map((destination) => (
          <Link
            key={destination.to}
            to={destination.to}
            data-testid={`not-found-link-${destination.label.toLowerCase().replace(/\s+/g, "-")}`}
            css={{
              display: "grid",
              gap: theme.space(1),
              padding: theme.space(3),
              borderRadius: theme.radius.md,
              border: `1px solid ${theme.color.border}`,
              background: theme.color.bgElevated,
              "&:hover": { borderColor: theme.color.primary },
            }}
          >
            <Text css={{ color: theme.color.text, fontWeight: 500 }}>
              {destination.label}
            </Text>
            <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
              {destination.hint}
            </Text>
          </Link>
        ))}
      </div>

      <Link to={paths.dashboard}>
        <Button data-testid="not-found-dashboard">Back to the dashboard</Button>
      </Link>
    </div>
  );
}
