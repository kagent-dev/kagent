import type { ExtensionRowAction } from "@/appExtensions";
import { buildPath, paths } from "@/router/routes";

/**
 * An item in each agent row's actions menu.
 *
 * Data rather than a component, so the host draws it as a menu item beside Edit and
 * Delete; `visible` keeps it off rows it does not suit, and `link` is told which
 * row it is for. It is a link rather than an `onClick`, so ctrl/middle-click opens a new tab.
 */
export const exampleEditInWindowAction: ExtensionRowAction = {
  id: "exampleEditInWindow",
  menuId: "app_agents_agentsList_rowMenu",
  label: "Open edit page",
  // The edit page rather than a new chat: opening it again changes nothing.
  link: ({ namespace, name }) => buildPath(paths.agentEdit, { namespace, name }),
};
