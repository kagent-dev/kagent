import { useState } from "react";
import { Button, Tooltip } from "antd";
import { Pencil } from "lucide-react";
import type { AgentInstance } from "@/api";
import { conversationTitle } from "./instanceLabels";
import { RenameConversationDialog } from "./RenameConversationDialog";

/**
 * The button-shaped way in to renaming a conversation.
 *
 * A table row has somewhere to put an icon and something for a tooltip to explain, so
 * this is what the conversations table uses. The rail's action menu and the details
 * modal have neither, and reach for `RenameConversationDialog` directly — which is
 * where the write, the validation and the reasoning about all of it live.
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
  /** Passed through: the dialog re-reads every surface showing this conversation. */
  onRenamed?: () => void | Promise<void>;
}) {
  const [isOpen, setOpen] = useState(false);

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
            onClick={() => setOpen(true)}
            data-testid={`conversation-rename-${instance.id}`}
            aria-label={`Rename conversation ${conversationTitle(instance)}`}
          />
        </span>
      </Tooltip>

      {isOpen ? (
        <RenameConversationDialog
          instance={instance}
          onClose={() => setOpen(false)}
          onRenamed={onRenamed}
        />
      ) : null}
    </>
  );
}
