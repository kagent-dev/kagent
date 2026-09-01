import { useState } from "react";
import { Button, Popconfirm } from "antd";
import { Trash2 } from "lucide-react";
import toast from "react-hot-toast";

/**
 * Deletes one row's resource, having asked first.
 *
 * Shared by every list that offers a delete, so that "are you sure" is worded the
 * same way and a failure is reported the same way wherever it happens. A list that
 * rolled its own would eventually disagree with the others about whether a failed
 * delete is a toast, an inline message, or nothing at all.
 *
 * The confirmation names the resource. A dialog reading "Delete this agent?" is no
 * help in a table of twelve of them — the one question the reader has is *which*,
 * and it is the one thing a generic prompt leaves out.
 */
export function DeleteResourceButton({
  kind,
  name,
  onDelete,
  onDeleted,
  disabled,
  description,
  label,
  outlined = false,
}: {
  /** What this is, in words a reader would use: "agent", "model configuration". */
  kind: string;
  /** How the row is identified, shown in the confirmation. */
  name: string;
  /**
   * What deleting this actually costs, when "cannot be undone" is not the whole
   * story.
   *
   * Defaults to the generic sentence, which is right for a resource whose deletion
   * affects only itself. Some do not: deleting an `AgentTemplate` leaves the agents
   * already cut from it running — they hold a prepared revision the collector
   * refuses to reclaim — while preventing any new one. A reader deciding whether to
   * delete needs that, and a prompt that omits it is the one that gets clicked
   * through.
   */
  description?: React.ReactNode;
  /**
   * A visible label beside the icon.
   *
   * Omitted in a table row, where the column and the row say what it acts on and a
   * bare icon is the least cluttered control that can. On a page about one resource
   * there is no row to lend it that meaning, so an unlabelled trash can is a
   * guess — and the wrong guess is destructive.
   */
  label?: string;
  /**
   * Draw it as a bordered control rather than as text.
   *
   * A text button is right in a table row, where a border on every row would be
   * noise. On a page header it reads as a link, and a destructive action that
   * looks like a link is one somebody clicks while meaning to navigate. Defaults
   * to off so no existing row changes.
   */
  outlined?: boolean;
  onDelete: () => Promise<void>;
  /** Called after a successful delete, to refresh whatever listed it. */
  onDeleted: () => void | Promise<void>;
  disabled?: boolean;
}) {
  const [isDeleting, setDeleting] = useState(false);

  async function confirm() {
    setDeleting(true);
    try {
      await onDelete();
      // Refreshed before the toast, so the row is gone by the time the reader is
      // told it is. The other order shows a success over a table still listing it.
      await onDeleted();
      toast.success(`Deleted ${kind} ${name}`);
    } catch (cause: unknown) {
      // Deliberately not a toast that disappears: a delete that failed leaves the
      // resource in place, and a reader who missed the message would believe it
      // gone. `duration: Infinity` makes it dismissable rather than transient.
      toast.error(
        `Could not delete ${kind} ${name}: ${
          cause instanceof Error ? cause.message : String(cause)
        }`,
        { duration: Infinity },
      );
    } finally {
      setDeleting(false);
    }
  }

  return (
    <Popconfirm
      title={`Delete ${kind} ${name}?`}
      description={description ?? "This removes it from the cluster and cannot be undone."}
      okText="Delete"
      okButtonProps={{ danger: true, loading: isDeleting }}
      cancelText="Keep"
      onConfirm={confirm}
    >
      <Button
        // `default` gives it the border; `danger` keeps it red either way, so the
        // extra prominence does not read as encouragement.
        type={outlined ? "default" : "text"}
        danger
        // Full size rather than `small`: at 24px square in a row that is 54px tall this
        // was a smaller target than anything else on the row, and it is the control with
        // the worst consequence for a near miss.
        icon={<Trash2 size={16} />}
        loading={isDeleting}
        disabled={disabled}
        data-testid={`delete-${name}`}
        // Kept even when `label` is set: the visible label says "Delete template",
        // while a screen reader listing controls out of context needs to know *which*.
        aria-label={`Delete ${kind} ${name}`}
      >
        {label}
      </Button>
    </Popconfirm>
  );
}
