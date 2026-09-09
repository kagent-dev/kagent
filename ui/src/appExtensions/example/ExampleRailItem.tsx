import { Link } from "react-router-dom";
import { useTheme } from "@emotion/react";
import { Puzzle } from "lucide-react";
import { agentRailEntryStyles } from "@/appExtensions";
import type { ExtensionAgentRailItemProps } from "@/appExtensions";
import { EXAMPLE_PATH } from "./paths";

/**
 * A rail entry, supplied by the extension, sitting between the application's two.
 *
 * Drawn with `agentRailEntryStyles`, which is what the rail styles its own entries
 * with, for the reason `ExampleNavItem` gives at length: hand-rolling the row is
 * free until it has to line up, and then it has to reproduce the height, the inset
 * pill, the icon column and the active weight, and drift from all four the next time
 * any of them changes. A contribution that wants to look nothing like the rail is
 * free to; it should not have to.
 *
 * The agent it was handed is used rather than ignored, because an entry beneath a
 * conversation usually wants to be about that conversation.
 */
export function ExampleRailItem({ isActive, agent }: ExtensionAgentRailItemProps) {
  const theme = useTheme();
  const to = agent ? `${EXAMPLE_PATH}?agent=${agent.id}` : EXAMPLE_PATH;

  return (
    <Link
      to={to}
      data-testid="agent-rail-example"
      data-active={isActive}
      aria-current={isActive ? "page" : undefined}
      css={{
        ...agentRailEntryStyles(theme, isActive),
        fontSize: 13,
        fontWeight: isActive ? 600 : 400,
      }}
    >
      <Puzzle size={14} aria-hidden />
      Example
    </Link>
  );
}
