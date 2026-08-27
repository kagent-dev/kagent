import { useTheme } from "@emotion/react";
import { Link } from "react-router-dom";
import { Sparkles } from "lucide-react";
import type { VendorNavItemProps } from "@/vendorExtensions";
import { EXAMPLE_INSIGHTS_PATH } from "./paths";

/**
 * The whole nav entry, supplied by the extension.
 *
 * The framework offers no label-and-icon shorthand, so this is what a vendor
 * writes: their own markup, their own link, styled from the host theme when
 * they want to blend in and however they like when they do not.
 */
export function ExampleNavItem({ isActive }: VendorNavItemProps) {
  const theme = useTheme();

  return (
    <Link
      to={EXAMPLE_INSIGHTS_PATH}
      data-testid="nav-example-insights"
      css={{
        display: "flex",
        alignItems: "center",
        gap: theme.space(3),
        // Matches the geometry antd's Menu gives the core items, so a
        // contribution sits in the list rather than beside it.
        margin: `${theme.space(1)} ${theme.space(1)}`,
        padding: `${theme.space(2)} ${theme.space(4)}`,
        borderRadius: theme.radius.md,
        fontSize: 14,
        color: isActive ? theme.color.text : theme.color.textMuted,
        background: isActive ? theme.color.primary : "transparent",
        "&:hover": { color: theme.color.text },
      }}
    >
      <Sparkles size={16} />
      Example Insights
    </Link>
  );
}
