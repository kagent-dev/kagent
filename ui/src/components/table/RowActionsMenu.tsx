import { useState, type ComponentProps } from "react";
import { Button, Dropdown, Tooltip } from "antd";
import { Copy, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { DeleteResourceButton } from "./DeleteResourceButton";

/**
 * The ellipsis menu on a list row: open or edit it, create a copy of it, delete it.
 *
 * Shared by the agent, agent template and harness lists so the three offer the same
 * actions in the same order. Delete keeps `DeleteResourceButton`'s confirmation; the
 * menu item only opens it.
 */
export function RowActionsMenu({
  kind,
  namespace,
  name,
  editTo,
  editLabel = "Edit",
  copyTo,
  disabledReason,
  remove,
}: {
  kind: string;
  namespace: string;
  name: string;
  editTo: string;
  editLabel?: string;
  /** The "new" form's address, carrying `?from=` so it opens prefilled. */
  copyTo: string;
  /** Why Delete is unavailable, shown on hover when `remove.disabled` is set. */
  disabledReason?: string;
  remove: Pick<
    ComponentProps<typeof DeleteResourceButton>,
    "onDelete" | "onDeleted" | "description" | "disabled"
  >;
}) {
  const navigate = useNavigate();
  const [deleting, setDeleting] = useState(false);
  // Counts opens, and keys the confirmation by it: each open starts from a fresh one, so a
  // ticked force option never carries over to the next.
  const [opens, setOpens] = useState(0);
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
            { key: "delete", label: remove.disabled && disabledReason ? <Tooltip title={disabledReason}>{`Delete ${kind}`}</Tooltip> : `Delete ${kind}`, icon: <Trash2 size={14} />, danger: true, disabled: remove.disabled, "data-testid": `row-delete-${id}` },
          ],
          onClick: ({ key }) => {
            if (key === "edit") void navigate(editTo);
            else if (key === "copy") void navigate(copyTo);
            else {
              setOpens((n) => n + 1);
              setDeleting(true);
            }
          },
        }}
      >
        <Button type="text" icon={<MoreHorizontal size={16} />} data-testid={`row-actions-${id}`} aria-label={`Actions for ${kind} ${name}`} />
      </Dropdown>
      <DeleteResourceButton key={opens} kind={kind} name={name} {...remove} open={deleting} onClose={() => setDeleting(false)} />
    </>
  );
}

/** The "new" form's address for a copy of `source`; read back with `useSearchParams().get("from")`. */
export function copyPath(newPath: string, source: { namespace: string; name: string }) {
  return `${newPath}?from=${encodeURIComponent(`${source.namespace}/${source.name}`)}`;
}

/**
 * The resource a "new" form should copy, from its `?from=` parameter.
 *
 * Anything but `namespace/name` is no source at all, so a hand-edited address opens a
 * blank form rather than waiting on a resource that cannot exist.
 */
export function copySource(params: URLSearchParams): { namespace: string; name: string } | undefined {
  const [namespace, name, ...rest] = (params.get("from") ?? "").split("/");
  return namespace && name && rest.length === 0 ? { namespace, name } : undefined;
}
