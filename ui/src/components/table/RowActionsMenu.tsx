import { Button, Dropdown } from "antd";
import { Copy, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { isRowActionVisible, rowActionLink, runRowAction, useExtensionRowActions, type ExtensionRowAction, type ExtensionRowMenuId } from "@/appExtensions";
import { DeleteConfirmModal, useModalState, type DeleteConfirmProps } from "./DeleteConfirmModal";

/** Each kind of row, and the extension menu whose items it shows. */
const EXTENSION_MENU_BY_KIND = {
  agent: "app_agents_agentsList_rowMenu",
  "agent template": "app_agents_agentTemplatesList_rowMenu",
  harness: "app_agents_harnessesList_rowMenu",
} as const satisfies Record<string, ExtensionRowMenuId>;

/**
 * A menu label that is a real link, so ctrl/middle-click opens a new tab. A plain click on
 * it navigates through the router; the rest of the item (icon, padding, Enter on the
 * focused item) navigates through the menu's `onClick`, which skips clicks on the anchor.
 */
function linkLabel(to: string, label: string, testId: string) {
  return (
    <Link to={to} data-testid={testId}>
      {label}
    </Link>
  );
}

/**
 * The ellipsis menu on a list row: open or edit it, create a copy of it, delete it.
 * Installed extensions can add items of their own between copy and delete, for the row's `kind`.
 *
 * Shared by the agent, agent template and harness lists so the three offer the same
 * actions in the same order. Delete shares `DeleteResourceButton`'s confirmation; the
 * menu item only opens it.
 */
export function RowActionsMenu({
  kind,
  namespace,
  name,
  editTo,
  editLabel = "Edit",
  copyTo,
  remove,
}: {
  kind: keyof typeof EXTENSION_MENU_BY_KIND;
  namespace: string;
  name: string;
  editTo: string;
  editLabel?: string;
  /** The "new" form's address, carrying `?from=` so it opens prefilled. */
  copyTo: string;
  remove: Pick<DeleteConfirmProps, "onDelete" | "onDeleted" | "description">;
}) {
  const navigate = useNavigate();
  const modal = useModalState();
  // Namespaced, so same-named resources in two namespaces stay distinguishable, and
  // prefixed, so these never collide with another list's `edit-<name>`.
  const id = `${namespace}/${name}`;
  const row = { kind, namespace, name };
  const menuActions = useExtensionRowActions(EXTENSION_MENU_BY_KIND[kind]);
  // Not memoized: `visible` may read state outside the row (a flag, the signed-in user),
  // and it is expected to be cheap.
  const extensionActions: { action: ExtensionRowAction; to: string | undefined }[] = menuActions
    .filter((action) => isRowActionVisible(action, row))
    // A link that cannot be built leaves its item out, as a throwing `visible` does.
    .flatMap((action): { action: ExtensionRowAction; to: string | undefined }[] => {
      if (!action.link) return [{ action, to: undefined }];
      const to = rowActionLink(action, row);
      return to === undefined ? [] : [{ action, to }];
    });
  // Where each link item goes, by menu key.
  const linkTargets: Record<string, string> = { edit: editTo, copy: copyTo };
  for (const { action, to } of extensionActions) if (to) linkTargets[`extension:${action.id}`] = to;
  return (
    <>
      <Dropdown
        trigger={["click"]}
        menu={{
          items: [
            { key: "edit", label: linkLabel(editTo, `${editLabel} ${kind}`, `row-edit-${id}`), icon: <Pencil size={14} /> },
            { key: "copy", label: linkLabel(copyTo, `Create new ${kind} from this`, `row-copy-${id}`), icon: <Copy size={14} /> },
            // Prefixed, so an extension's id can never collide with the built-in keys.
            ...extensionActions.map(({ action, to }) => {
              const testId = `row-extension-${action.id}-${id}`;
              return to
                ? { key: `extension:${action.id}`, label: linkLabel(to, action.label, testId), icon: action.icon }
                : { key: `extension:${action.id}`, label: action.label, icon: action.icon, "data-testid": testId };
            }),
            { type: "divider" },
            { key: "delete", label: `Delete ${kind}`, icon: <Trash2 size={14} />, danger: true, "data-testid": `row-delete-${id}` },
          ],
          onClick: ({ key, domEvent }) => {
            if (key in linkTargets) {
              // The anchor already navigated for a click on it; anything else lands here.
              if (!(domEvent.target instanceof Element && domEvent.target.closest("a"))) void navigate(linkTargets[key]);
            } else if (key === "delete") modal.show();
            else {
              const action = extensionActions.find((candidate) => `extension:${candidate.action.id}` === key)?.action;
              if (action?.onClick) runRowAction(action, row);
            }
          },
        }}
      >
        <Button type="text" icon={<MoreHorizontal size={16} />} data-testid={`row-actions-${id}`} aria-label={`Actions for ${kind} ${name}`} />
      </Dropdown>
      {modal.mounted && <DeleteConfirmModal kind={kind} name={name} {...remove} open={modal.open} onClose={modal.hide} afterClose={modal.afterClose} />}
    </>
  );
}
