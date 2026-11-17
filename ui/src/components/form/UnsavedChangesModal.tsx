import type { ReactNode } from "react";
import { Modal, Typography } from "antd";
import type { Blocker } from "react-router-dom";

interface UnsavedChangesModalProps {
  /** From `useUnsavedChangesGuard`. */
  blocker: Blocker;
  /** What is lost and what is not, e.g. "the model on the cluster is unchanged". */
  children: ReactNode;
  /** Prefix for `-discard-modal` and `-discard-body` test ids. */
  testIdPrefix?: string;
}

/** Confirms a blocked navigation. Discard continues to wherever the user was going. */
export function UnsavedChangesModal({ blocker, children, testIdPrefix = "form" }: UnsavedChangesModalProps) {
  return (
    // Controlled rather than `Modal.confirm`, whose static form cannot read context.
    <Modal
      open={blocker.state === "blocked"}
      title="Discard your changes?"
      okText="Discard"
      okButtonProps={{ danger: true }}
      cancelText="Keep editing"
      onOk={() => blocker.proceed?.()}
      onCancel={() => blocker.reset?.()}
      // On the dialog itself: a `data-testid` prop lands on antd's root, which has no box.
      modalRender={(dialog) => <div data-testid={`${testIdPrefix}-discard-modal`}>{dialog}</div>}
    >
      <Typography.Paragraph css={{ margin: 0 }} data-testid={`${testIdPrefix}-discard-body`}>
        {children}
      </Typography.Paragraph>
    </Modal>
  );
}
