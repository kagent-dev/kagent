import { useState } from "react";
import { Checkbox, Modal } from "antd";
import toast from "react-hot-toast";

/** What a delete confirmation needs to know; shared by the button's popover and modal and by row menus. */
export interface DeleteConfirmProps {
  /** What this is, in words a reader would use: "agent", "model configuration". */
  kind: string;
  /** How the row is identified, shown in the confirmation. */
  name: string;
  /**
   * What deleting this actually costs, when "cannot be undone" is not the whole
   * story. Defaults to the generic sentence, which is right for a resource whose
   * deletion affects only itself.
   */
  description?: React.ReactNode;
  /** Offer an explicit force option only for resources that support it. */
  forceLabel?: string;
  onDelete: (force: boolean) => Promise<void>;
  /** Called after a successful delete, to refresh whatever listed it. */
  onDeleted: () => void | Promise<void>;
}

/** The delete, its failure reporting and the confirmation's text, for whatever surface asks. */
export function useDeleteConfirm({ kind, name, description, forceLabel, onDelete, onDeleted }: DeleteConfirmProps) {
  const [isDeleting, setDeleting] = useState(false);
  const [force, setForce] = useState(false);

  /** `afterDelete` runs once the delete has succeeded and the list is refreshed. */
  async function confirm(afterDelete?: () => void) {
    setDeleting(true);
    try {
      await onDelete(forceLabel !== undefined && force);
      // Refreshed before the toast, so the row is gone by the time the reader is told
      // it is. Swallowed, because every caller passes an `ApiResource.refresh` and
      // those rethrow: unguarded, a failed re-read after a successful delete reports
      // the delete as failed, in a toast that never goes away.
      await Promise.resolve(onDeleted()).catch(() => {});
      afterDelete?.();
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

  const title = `Delete ${kind} ${name}?`;
  const message = <>
    {description ?? "This removes it from the cluster and cannot be undone."}
    {forceLabel && <div><Checkbox checked={force} disabled={isDeleting}
      onChange={(event) => setForce(event.target.checked)}>{forceLabel}</Checkbox></div>}
  </>;
  return { isDeleting, setForce, confirm, title, message };
}

/** The modal itself, over a `useDeleteConfirm` the caller owns so its trigger can share the state. */
export function DeleteModal({ confirmation: { isDeleting, confirm, title, message }, onClose }: {
  confirmation: ReturnType<typeof useDeleteConfirm>;
  onClose: () => void;
}) {
  return (
    <Modal open title={title} onOk={() => void confirm(onClose)} onCancel={onClose}
      okText="Delete" okButtonProps={{ danger: true }} confirmLoading={isDeleting}
      cancelText="Keep" cancelButtonProps={{ disabled: isDeleting }} closable={!isDeleting}
      mask={{ closable: !isDeleting }} keyboard={!isDeleting}>
      {message}
    </Modal>
  );
}

/**
 * The delete confirmation as a modal. Mount it while it should be showing: it starts
 * from fresh state each time, so a ticked force option never carries over to the next.
 */
export function DeleteConfirmModal({ onClose, ...props }: DeleteConfirmProps & { onClose: () => void }) {
  return <DeleteModal confirmation={useDeleteConfirm(props)} onClose={onClose} />;
}
