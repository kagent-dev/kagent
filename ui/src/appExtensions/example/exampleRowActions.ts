import type { ExtensionRowAction } from "@/appExtensions";
import { buildPath, paths } from "@/router/routes";

/**
 * An item in each agent row's actions menu.
 *
 * Data rather than a component, so the host draws it as a menu item beside Edit and
 * Delete; `visible` keeps it off rows it does not suit, and `onClick` is told which
 * row it was chosen on.
 */
export const exampleEditInWindowAction: ExtensionRowAction = {
  id: "exampleEditInWindow",
  menuId: "app_agents_agentsList_rowMenu",
  label: "Edit in new window",
  onClick: ({ namespace, name }) => {
    // The edit page rather than a new chat: opening it again changes nothing.
    window.open(buildPath(paths.agentEdit, { namespace, name }), "_blank", "noopener");
  },
};
