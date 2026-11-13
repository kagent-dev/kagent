import { Button, Popconfirm } from "antd";
import { Trash2 } from "lucide-react";
import { DeleteModal, useDeleteConfirm, useModalState, type DeleteConfirmProps } from "./DeleteConfirmModal";

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
  disabled,
  label,
  outlined = false,
  confirmation = "popover",
  ...target
}: DeleteConfirmProps & {
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
  disabled?: boolean;
}) {
  const modal = useModalState();
  const { kind, name } = target;
  const deletion = useDeleteConfirm(target);
  const { isDeleting, setForce, confirm, title, message } = deletion;
  const button = (
    <Button
      // `default` gives it the border; `danger` keeps it red either way, so the
      // extra prominence does not read as encouragement.
      type={outlined ? "default" : "text"}
      danger
      // Full size rather than `small`: at 24px square in a row that is 54px tall this
      // was a smaller target than anything else on the row, and it is the control with
      // the worst consequence for a near miss.
      icon={<Trash2 size={16} />}
      onClick={confirmation === "modal" ? () => { setForce(false); modal.show(); } : undefined}
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
  return confirmation === "modal" ? <>
    {button}
    {modal.mounted && <DeleteModal confirmation={deletion} open={modal.open} onClose={modal.hide} afterClose={modal.afterClose} />}
  </> : (
    <Popconfirm title={title} description={message} okText="Delete"
      onOpenChange={(value) => { if (value) setForce(false); }}
      okButtonProps={{ danger: true, loading: isDeleting }} cancelText="Keep" onConfirm={() => confirm()}>
      {button}
    </Popconfirm>
  );
}
