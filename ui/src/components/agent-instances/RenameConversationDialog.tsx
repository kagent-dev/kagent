import { useState } from "react";
import { Input, Modal, Typography } from "antd";
import { SubmitError } from "@/components/common/SubmitError";
import { FormField } from "@/components/form/FormField";
import { toastSaved, toastSuccess } from "@/components/form/saveToast";
import { useSubmitValidation } from "@/components/form/useSubmitValidation";
import { useTheme } from "@emotion/react";
import {
  apiClient,
  conversationNameProblem,
  MAX_CONVERSATION_NAME_LENGTH,
  useInvalidateConversations,
  type AgentInstance,
} from "@/api";
import { shortInstanceId } from "./instanceLabels";

const { Paragraph } = Typography;

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
  onClose,
  onRenamed,
}: {
  instance: AgentInstance;
  /**
   * Rendered only while it is open, which is what makes the box start on the stored
   * name without an effect to put it there.
   *
   * The first shape of this took an `open` prop and stayed mounted, and then needed an
   * effect to reset the draft each time it opened — otherwise a cancelled edit was
   * still sitting in the field the next time, reading as a name the conversation does
   * not have. Mounting it with the state it should have is the same fix without the
   * cascading render, and the callers say `{isRenaming && …}` instead of passing a
   * flag through.
   */
  onClose: () => void;
  /**
   * Anything beyond re-reading the conversation, which this does for itself.
   *
   * Every surface showing this conversation is re-read whichever one the rename was
   * started from, so a caller needs this only when a rename means something to it over
   * and above the new name — which so far nothing does.
   */
  onRenamed?: () => void | Promise<void>;
}) {
  const theme = useTheme();
  const [draft, setDraft] = useState(instance.name);
  const [isSaving, setSaving] = useState(false);
  const invalidateConversations = useInvalidateConversations();

  const [failure, setFailure] = useState<unknown>();
  const problem = conversationNameProblem(draft);
  const validation = useSubmitValidation(problem ? [{ field: "name", message: problem }] : []);

  async function save() {
    if (isSaving || !validation.attempt()) return;
    setSaving(true);
    setFailure(undefined);
    try {
      await apiClient.agentInstances.rename(instance.id, draft);
      // Awaited before the toast, so every surface showing this conversation already
      // has the new name by the time the reader is told it changed.
      await invalidateConversations();
      await onRenamed?.();
      onClose();
      if (draft === "") toastSuccess(`Cleared the name of conversation ${shortInstanceId(instance.id)}`);
      else toastSaved("saved", "conversation name", draft);
    } catch (cause: unknown) {
      setFailure(cause);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open
      title="Name this conversation"
      okText="Save"
      // No test id on the confirm button: antd builds the footer itself, and a
      // prop smuggled through `okButtonProps` lands wherever that component
      // decides. It answers to its accessible name, which is the affordance the
      // reader uses anyway.
      okButtonProps={{ loading: isSaving }}
      cancelText="Cancel"
      onOk={() => void save()}
      onCancel={onClose}
      destroyOnHidden
    >
      <Paragraph css={{ color: theme.color.textMuted, fontSize: 13 }}>
        A conversation is named by you. Leave this empty to clear the name and go
        back to being identified by its id, {shortInstanceId(instance.id)}.
      </Paragraph>
      <FormField label="Conversation name" required={false} error={validation.errorFor("name")}>
        <Input
          value={draft}
          id="conversation-rename-field"
          data-testid="conversation-rename-input"
          autoFocus
          maxLength={MAX_CONVERSATION_NAME_LENGTH}
          showCount
          placeholder={`Untitled · ${shortInstanceId(instance.id)}`}
          onChange={(event) => setDraft(event.target.value)}
          onPressEnter={() => void save()}
        />
      </FormField>
      {failure !== undefined ? (
        <SubmitError
          error={failure}
          what="conversation name"
          outcome="saved"
          onRetry={() => void save()}
          data-testid="conversation-rename-error"
        />
      ) : null}
    </Modal>
  );
}
