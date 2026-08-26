import { useState } from "react";
import { Button, Input, Modal, Tooltip, Typography } from "antd";
import { Pencil } from "lucide-react";
import toast from "react-hot-toast";
import { useTheme } from "@emotion/react";
import {
  apiClient,
  conversationNameProblem,
  MAX_CONVERSATION_NAME_LENGTH,
  type AgentInstance,
} from "@/api";
import { conversationTitle, shortInstanceId } from "./instanceLabels";

const { Paragraph, Text } = Typography;

/**
 * Gives a conversation a name, or takes one away.
 *
 * A conversation is named by the reader — there is nothing else to name it by, since
 * an instance is a row keyed by a UUID and the agent it belongs to is named by its
 * template. `RenameAgentInstance` is the only write on `AgentInstanceService` that is
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
export function RenameConversationButton({
  instance,
  disabled,
  onRenamed,
}: {
  instance: AgentInstance;
  /**
   * Refused for somebody else's conversation.
   *
   * The controller resolves a rename through the creator, exactly as it resolves a
   * read, so this is the controller's rule rather than a preference. Offered and
   * then refused would be worse than plainly unavailable.
   */
  disabled?: boolean;
  onRenamed: () => void | Promise<void>;
}) {
  const theme = useTheme();
  const [isOpen, setOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const [isSaving, setSaving] = useState(false);

  const problem = conversationNameProblem(draft);

  function open() {
    // The stored name, which is empty for an unnamed conversation. See the note
    // above about why this is not the displayed title.
    setDraft(instance.name);
    setOpen(true);
  }

  async function save() {
    if (problem) return;
    setSaving(true);
    try {
      await apiClient.agentInstances.rename(instance.namespace, instance.id, draft);
      // Refreshed before the toast, so the list already shows the new name by the
      // time the reader is told it changed.
      await onRenamed();
      setOpen(false);
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
    <>
      <Tooltip
        title={
          disabled
            ? `Only ${instance.creator || "the person who started it"} can rename this conversation.`
            : "Rename this conversation"
        }
      >
        {/* A span, because antd cannot show a tooltip over a disabled button: it
            stops emitting the pointer events the tooltip listens for, so the state
            that most needs explaining would be the one that could not explain
            itself. */}
        <span>
          <Button
            type="text"
            icon={<Pencil size={16} />}
            disabled={disabled}
            onClick={open}
            data-testid={`conversation-rename-${instance.id}`}
            aria-label={`Rename conversation ${conversationTitle(instance)}`}
          />
        </span>
      </Tooltip>

      <Modal
        open={isOpen}
        title="Name this conversation"
        okText="Save"
        // No test id on the confirm button: antd builds the footer itself, and a
        // prop smuggled through `okButtonProps` lands wherever that component
        // decides. It answers to its accessible name, which is the affordance the
        // reader uses anyway.
        okButtonProps={{ loading: isSaving, disabled: problem !== undefined }}
        cancelText="Cancel"
        onOk={() => void save()}
        onCancel={() => setOpen(false)}
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
    </>
  );
}
