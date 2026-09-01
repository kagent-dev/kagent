import { Menu } from "antd";
import { Link, useNavigate } from "react-router-dom";
import { Puzzle } from "lucide-react";
import { useThemeMode } from "@/appExtensions";
import type { ExtensionNavItemProps } from "@/appExtensions";
import { EXAMPLE_PATH } from "./paths";

/**
 * The whole nav entry, supplied by the extension.
 *
 * The framework offers no label-and-icon shorthand, so a contribution draws its
 * own entry — and this one draws it with the same antd `Menu` the rail uses for
 * the application's own entries. That is the point worth copying: hand-rolling a
 * link is free until it has to line up, and then it has to reproduce the row
 * height, the inset pill, the icon column and the collapsed layout, and drift
 * from all four the next time the library changes any of them. Rendering the same
 * component the shell renders makes the entry indistinguishable from a core one
 * in both palettes and in both widths, for no styling of its own.
 *
 * A contribution that wants to look nothing like the application's navigation is
 * still free to; it just should not have to.
 */
export function ExampleNavItem({ isActive, collapsed }: ExtensionNavItemProps) {
  const { mode } = useThemeMode();
  const navigate = useNavigate();

  return (
    <Menu
      // Follows the palette for the same reason the rail's own menus do: pinned to
      // one, the library paints item text for the wrong background and unselected
      // entries become unreadable.
      theme={mode === "light" ? "light" : "dark"}
      mode="inline"
      inlineCollapsed={collapsed}
      selectedKeys={isActive ? ["example"] : []}
      css={{ borderInlineEnd: "none" }}
      // The label is a real link, so the entry can be opened in a new tab; the
      // router already handles those clicks, so navigating again from here would
      // push a second history entry. Clicks on the icon or the padding still come
      // through, which is what keeps the row's hit area whole.
      onClick={({ domEvent }) => {
        if ((domEvent.target as HTMLElement).closest("a")) return;
        navigate(EXAMPLE_PATH);
      }}
      items={[
        {
          key: "example",
          icon: <Puzzle size={16} />,
          label: (
            <Link to={EXAMPLE_PATH} css={{ color: "inherit" }}>
              Example
            </Link>
          ),
          "data-testid": "nav-example",
        },
      ]}
    />
  );
}
