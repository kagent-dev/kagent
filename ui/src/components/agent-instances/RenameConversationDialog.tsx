import { useEffect, useState } from "react";
import { Input, Modal, Typography } from "antd";
import toast from "react-hot-toast";
import { useTheme } from "@emotion/react";
import {
  apiClient,
  conversationNameProblem,
  MAX_CONVERSATION_NAME_LENGTH,
  type AgentInstance,
} from "@/api";
import { shortInstanceId } from "./instanceLabels";

const { Paragraph, Text } = Typography;

/**
 * Gives a conversation a name, or takes one away.
 *
 * A conversation is named by the reader — there is nothing else to name it by, since
 * an instance is a row keyed by a UUID and the agent it belongs to is named by its
 * template. `UpdateAgentInstanceName` is the only write on `AgentInstanceService` that is
 * not a lifecycle operation, and it authorises as a write: its policy entry is
 * `AccessUpdate`, so a read-only share link cannot retitle a conversation for
 * everybody holding it.
 *
 * ## The box opens on the stored name, not on what is on screen
 *
 * An unnamed conversation renders as "Untitled · 6f1c9d20", and pre-filling the box
 * with that would make clearing a title impossible: saving would turn an honest
 * placeholder into a literal one. So the field starts empty for an unnamed
 * conversation, with the placeholder saying what it would otherwise be called.
 *
 * ## Validated here in the controller's own words
 *
 * `conversationNameProblem` is `validateName` from the service, copied. Two of its
 * rules are surprising enough to be worth stating rather than discovering: leading
 * and trailing spaces are *refused* rather than trimmed — quietly rewriting what
 * somebody typed reads on screen as a rename that did not take — and an empty name
 * is valid, because that is how a title is cleared.
 */
/**
 * ## Why this is a dialog rather than a button
 *
 * Three surfaces offer this now — the conversations table, the rail's action menu, and
 * the details modal — and only the first of them is a button. A menu item and a row in a
 * modal have nothing to hang a button's tooltip on, so what they share is the dialog and
 * the write behind it. `RenameConversationButton` is the button-shaped wrapper.
 */
export function RenameConversationDialog({
  instance,
  open,
  onClose,
  onRenamed,
}: {
  instance: AgentInstance;
  open: boolean;
  onClose: () => void;
  onRenamed: () => void | Promise<void>;
}) {
  const theme = useTheme();
  const [draft, setDraft] = useState(instance.name);
  const [isSaving, setSaving] = useState(false);

  const problem = conversationNameProblem(draft);

  /*
   * Reset on opening, not on mounting.
   *
   * The dialog stays mounted between openings — its callers render it beside a menu
   * item or a table cell rather than conditionally — so a draft left over from a
   * cancelled edit would be waiting there the next time it opened, which reads as the
   * conversation already having a name it does not have. `destroyOnHidden` clears the
   * modal's own contents and not this state, which lives a level above it.
   */
  useEffect(() => {
    if (open) setDraft(instance.name);
  }, [open, instance.name]);

  async function save() {
    if (problem) return;
    setSaving(true);
    try {
      await apiClient.agentInstances.rename(instance.namespace, instance.id, draft);
      // Refreshed before the toast, so the list already shows the new name by the
      // time the reader is told it changed.
      await onRenamed();
      onClose();
      toast.success(
        draft === ""
          ? `Cleared the name of conversation ${shortInstanceId(instance.id)}`
          : `Renamed to “${draft}”`,
      );
    } catch (cause: unknown) {
      // Deliberately not transient: a rename that failed leaves the old name in
      // place, and a reader who missed the message would believe it changed.
      toast.error(
        `Could not rename conversation ${shortInstanceId(instance.id)}: ${
          cause instanceof Error ? cause.message : String(cause)
        }`,
        { duration: Infinity },
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      title="Name this conversation"
      okText="Save"
      // No test id on the confirm button: antd builds the footer itself, and a
      // prop smuggled through `okButtonProps` lands wherever that component
      // decides. It answers to its accessible name, which is the affordance the
      // reader uses anyway.
      okButtonProps={{ loading: isSaving, disabled: problem !== undefined }}
      cancelText="Cancel"
      onOk={() => void save()}
      onCancel={onClose}
      destroyOnHidden
    >
      <Paragraph css={{ color: theme.color.textMuted, fontSize: 13 }}>
        A conversation is named by you. Leave this empty to clear the name and go
        back to being identified by its id, {shortInstanceId(instance.id)}.
      </Paragraph>
      {/* The id is on a wrapper this app owns rather than on the `Input`: antd
          spreads unknown props onto its inner `<input>`, so an id handed to the
          component lands somewhere a test cannot reliably reason about. */}
      <div data-testid="conversation-rename-input">
        <Input
          value={draft}
          autoFocus
          maxLength={MAX_CONVERSATION_NAME_LENGTH}
          showCount
          placeholder={`Untitled · ${shortInstanceId(instance.id)}`}
          status={problem ? "error" : undefined}
          onChange={(event) => setDraft(event.target.value)}
          onPressEnter={() => void save()}
          aria-label="Conversation name"
        />
      </div>
      {problem ? (
        <Text
          data-testid="conversation-rename-problem"
          css={{ color: theme.color.dangerText, fontSize: 12 }}
        >
          {problem}
        </Text>
      ) : null}
    </Modal>
  );
}
