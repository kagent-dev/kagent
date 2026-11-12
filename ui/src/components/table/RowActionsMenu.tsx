import { useState } from "react";
import { Button, Dropdown } from "antd";
import { Copy, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { DeleteConfirmModal, type DeleteConfirmProps } from "./DeleteConfirmModal";

/**
 * The ellipsis menu on a list row: open or edit it, create a copy of it, delete it.
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
  kind: string;
  namespace: string;
  name: string;
  editTo: string;
  editLabel?: string;
  /** The "new" form's address, carrying `?from=` so it opens prefilled. */
  copyTo: string;
  remove: Pick<DeleteConfirmProps, "onDelete" | "onDeleted" | "description">;
}) {
  const navigate = useNavigate();
  const [deleting, setDeleting] = useState(false);
  // Namespaced, so same-named resources in two namespaces stay distinguishable, and
  // prefixed, so these never collide with another list's `edit-<name>`.
  const id = `${namespace}/${name}`;
  return (
    <>
      <Dropdown
        trigger={["click"]}
        menu={{
          items: [
            { key: "edit", label: `${editLabel} ${kind}`, icon: <Pencil size={14} />, "data-testid": `row-edit-${id}` },
            { key: "copy", label: `Create new ${kind} from this`, icon: <Copy size={14} />, "data-testid": `row-copy-${id}` },
            { type: "divider" },
            { key: "delete", label: `Delete ${kind}`, icon: <Trash2 size={14} />, danger: true, "data-testid": `row-delete-${id}` },
          ],
          onClick: ({ key }) => {
            if (key === "edit") void navigate(editTo);
            else if (key === "copy") void navigate(copyTo);
            else setDeleting(true);
          },
        }}
      >
        <Button type="text" icon={<MoreHorizontal size={16} />} data-testid={`row-actions-${id}`} aria-label={`Actions for ${kind} ${name}`} />
      </Dropdown>
      {deleting && <DeleteConfirmModal kind={kind} name={name} {...remove} onClose={() => setDeleting(false)} />}
    </>
  );
}
