import { Button, Dropdown } from "antd";
import { Copy, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { isRowActionVisible, runRowAction, useExtensionRowActions, type ExtensionRowMenuId } from "@/appExtensions";
import { DeleteConfirmModal, useModalState, type DeleteConfirmProps } from "./DeleteConfirmModal";

/** Each kind of row, and the extension menu whose items it shows. */
const EXTENSION_MENU_BY_KIND = {
  agent: "app_agents_agentsList_rowMenu",
  "agent template": "app_agents_agentTemplatesList_rowMenu",
  harness: "app_agents_harnessesList_rowMenu",
} as const satisfies Record<string, ExtensionRowMenuId>;

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
  const extensionActions = menuActions.filter((action) => isRowActionVisible(action, row));
  return (
    <>
      <Dropdown
        trigger={["click"]}
        menu={{
          items: [
            { key: "edit", label: `${editLabel} ${kind}`, icon: <Pencil size={14} />, "data-testid": `row-edit-${id}` },
            { key: "copy", label: `Create new ${kind} from this`, icon: <Copy size={14} />, "data-testid": `row-copy-${id}` },
            // Prefixed, so an extension's id can never collide with the built-in keys.
            ...extensionActions.map(({ id: actionId, label, icon }) => ({
              key: `extension:${actionId}`, label, icon, "data-testid": `row-extension-${actionId}-${id}`,
            })),
            { type: "divider" },
            { key: "delete", label: `Delete ${kind}`, icon: <Trash2 size={14} />, danger: true, "data-testid": `row-delete-${id}` },
          ],
          onClick: ({ key }) => {
            if (key === "edit") void navigate(editTo);
            else if (key === "copy") void navigate(copyTo);
            else if (key === "delete") modal.show();
            else {
              const action = extensionActions.find((candidate) => `extension:${candidate.id}` === key);
              if (action) runRowAction(action, row);
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
