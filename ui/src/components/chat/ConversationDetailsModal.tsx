import { useState } from "react";
import { Alert, Descriptions, Modal, Skeleton } from "antd";
import { useTheme } from "@emotion/react";
import { instanceFields } from "@/components/agent-instances/instanceFields";
import { RenameConversationDialog } from "@/components/agent-instances/RenameConversationDialog";
import { agentPageUrl } from "@/components/agent/agentUrl";
import { bareName, type AgentInstance, type ApiResource } from "@/api";

/**
 * One conversation's record, over the conversation.
 *
 * ## Why a modal rather than a page
 *
 * This was an entry in the agent rail, which meant leaving the conversation to read
 * four facts about it and then finding the way back. The record is reference — an id to
 * copy into a CLI, the revision it runs, when it was last touched — and reference that
 * costs a navigation is reference nobody consults.
 *
 * The fields are the ones the details page renders, from the same function. Two copies
 * of a record drift, and the one nobody opens is the one that stops showing a field the
 * controller started sending.
 */
export function ConversationDetailsModal({
  instance,
  open,
  onClose,
}: {
  instance: ApiResource<AgentInstance>;
  open: boolean;
  onClose: () => void;
}) {
  const theme = useTheme();
  const data = instance.data;
  const [isRenaming, setRenaming] = useState(false);

  const agentHref = data?.agentTemplate && data.harness
    ? agentPageUrl({
        namespace: data.namespace,
        agentTemplate: bareName(data.agentTemplate),
        harness: bareName(data.harness),
      })
    : undefined;

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={720}
      title="Conversation details"
      data-testid="conversation-details-modal"
    >
      {instance.error ? (
        <Alert
          type="error"
          showIcon
          data-testid="conversation-details-error"
          title="Could not read this conversation"
          description={instance.error.message}
        />
      ) : !data ? (
        <Skeleton active paragraph={{ rows: 6 }} />
      ) : (
        <>


        <Descriptions
          bordered
          size="small"
          column={2}
          items={instanceFields(data, theme, agentHref, () => setRenaming(true))}
          data-testid="conversation-details-fields"
        />

        {/*
          A dialog over a modal, which is worth a word because it is usually a smell.
          The alternative was editing the name in place in this table, and that means a
          second copy of the field's validation and of the write behind it — and this
          record exists in the first place because two copies of it drifted. antd stacks
          these correctly and returns focus here on close, and what the reader gets is
          the same box the table and the rail open.
        */}
        {isRenaming ? (
          <RenameConversationDialog instance={data} onClose={() => setRenaming(false)} />
        ) : null}
        </>
      )}
    </Modal>
  );
}
