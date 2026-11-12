import { useState } from "react";
import { Button, Checkbox, Modal, Popconfirm } from "antd";
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
  confirmation = "popover",
  forceLabel,
  open: controlledOpen,
  onClose,
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
  confirmation?: "popover" | "modal";
  /** Offer an explicit force option only for resources that support it. */
  forceLabel?: string;
  onDelete: (force: boolean) => Promise<void>;
  /** Called after a successful delete, to refresh whatever listed it. */
  onDeleted: () => void | Promise<void>;
  disabled?: boolean;
} & (
  | { open?: undefined; onClose?: undefined }
  /**
   * Driven from outside, by a menu item that stands in for the button. Renders only
   * the confirmation, as a modal: a popover has no button to hang from. The two props
   * come together, so a caller cannot open a modal it has no way to close.
   */
  | { open: boolean; onClose: () => void }
)) {
  const [ownOpen, setOwnOpen] = useState(false);
  const controlled = controlledOpen !== undefined;
  const open = controlled ? controlledOpen : ownOpen;
  const setOpen = (value: boolean) => (controlled ? !value && onClose?.() : setOwnOpen(value));
  const [isDeleting, setDeleting] = useState(false);
  const [force, setForce] = useState(false);

  async function confirm() {
    setDeleting(true);
    try {
      await onDelete(forceLabel !== undefined && force);
      // Refreshed before the toast, so the row is gone by the time the reader is told
      // it is. Swallowed, because every caller passes an `ApiResource.refresh` and
      // those rethrow: unguarded, a failed re-read after a successful delete reports
      // the delete as failed, in a toast that never goes away.
      await Promise.resolve(onDeleted()).catch(() => {});
      setOpen(false);
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

  const button = controlled ? null : (
    <Button
      // `default` gives it the border; `danger` keeps it red either way, so the
      // extra prominence does not read as encouragement.
      type={outlined ? "default" : "text"}
      danger
      // Full size rather than `small`: at 24px square in a row that is 54px tall this
      // was a smaller target than anything else on the row, and it is the control with
      // the worst consequence for a near miss.
      icon={<Trash2 size={16} />}
      onClick={confirmation === "modal" ? () => { setForce(false); setOpen(true); } : undefined}
      loading={isDeleting}
      disabled={disabled}
      data-testid={`delete-${name}`}
      // Kept even when `label` is set: the visible label says "Delete template",
      // while a screen reader listing controls out of context needs to know *which*.
      aria-label={`Delete ${kind} ${name}`}
    >
      {label}
    </Button>
  );
  const title = `Delete ${kind} ${name}?`;
  const message = <>
    {description ?? "This removes it from the cluster and cannot be undone."}
    {forceLabel && <div><Checkbox checked={force} disabled={isDeleting}
      onChange={(event) => setForce(event.target.checked)}>{forceLabel}</Checkbox></div>}
  </>;

  return confirmation === "modal" || controlled ? <>
    {button}
    <Modal open={open} title={title} onOk={confirm} onCancel={() => setOpen(false)}
      okText="Delete" okButtonProps={{ danger: true }} confirmLoading={isDeleting}
      cancelText="Keep" cancelButtonProps={{ disabled: isDeleting }} closable={!isDeleting}
      mask={{ closable: !isDeleting }} keyboard={!isDeleting}>
      {message}
    </Modal>
  </> : (
    <Popconfirm title={title} description={message} okText="Delete"
      onOpenChange={(value) => { if (value) setForce(false); }}
      okButtonProps={{ danger: true, loading: isDeleting }} cancelText="Keep" onConfirm={confirm}>
      {button}
    </Popconfirm>
  );
}
