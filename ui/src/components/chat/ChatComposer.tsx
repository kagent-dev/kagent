import { useImperativeHandle, useRef, useState, type Ref } from "react";
import { Button, Input, Space } from "antd";
import type { TextAreaRef } from "antd/es/input/TextArea";
import { useTheme } from "@emotion/react";
import { Send, Square } from "lucide-react";
import type { ChatController } from "@/api";

/** What a page can ask of the box from outside it: put the caret back in it. */
export type ChatComposerHandle = { focus: () => void };

/**
 * The message box.
 *
 * While a turn is streaming the send action becomes a stop action rather than
 * sitting disabled: the useful thing to offer someone watching a long answer is
 * a way out of it, not a greyed-out button.
 */
/**
 * Where a message is typed.
 *
 * Takes a `send` rather than the whole controller because the first message of a
 * conversation has nowhere to go yet: there is no session to send it to, and the page
 * uses that message to create one. Both cases are the same box, and this is what lets
 * them be — see `AgentChatPage`.
 */
export function ChatComposer({
  send,
  isStreaming = false,
  onCancel,
  disabled = false,
  variant = "docked",
  autoFocus = false,
  ref,
}: {
  send: (text: string) => Promise<void>;
  isStreaming?: boolean;
  /** Absent before a conversation exists — there is no stream to stop. */
  onCancel?: ChatController["cancel"];
  /**
   * Whether the agent can be sent to at all.
   *
   * Disabled rather than absent, because a missing composer reads as a rendering
   * fault where a disabled one — under a message saying why — explains itself. The
   * A2A gateway refuses any call for an instance that is not ready, so a box that
   * accepted text would swallow it.
   */
  disabled?: boolean;
  /**
   * Where this box is and what it has to do there.
   *
   * `docked` sits under a transcript: one control among the things already on screen.
   * `inviting` is the whole of an empty page — nothing else is competing for attention,
   * so the box takes the room and the bigger type instead.
   *
   * Neither draws a rule. `docked` used to, and it was reported as a defect: the panel
   * around this box is sticky, so the line rode over the conversation as it scrolled
   * under it, with a band of empty space above — a divider that looked like a rendering
   * fault rather than a separation. The composer is already separated from the
   * transcript by the fade the page paints behind it, which moves with the scroll
   * instead of cutting across it.
   */
  variant?: "docked" | "inviting";
  /**
   * Take the caret on arrival.
   *
   * For a page whose whole purpose is this box — a conversation that does not exist
   * yet has nothing else to read, so a reader who has to click before typing is being
   * asked to say twice that they came here to talk.
   */
  autoFocus?: boolean;
  /**
   * A way back to the caret for whatever took it.
   *
   * Answering the agent's question happens in a field inside the transcript, and
   * when that field is finished with, the next thing typed belongs here. Exposed as
   * a handle rather than found in the DOM by the answering component, which has no
   * business knowing this box exists.
   */
  ref?: Ref<ChatComposerHandle>;
}) {
  const theme = useTheme();
  const [draft, setDraft] = useState("");
  const inputRef = useRef<TextAreaRef>(null);

  useImperativeHandle(ref, () => ({ focus: () => inputRef.current?.focus() }), []);

  async function submit() {
    const text = draft.trim();
    if (!text || isStreaming || disabled) return;
    // Cleared before awaiting so the box is ready for the next message
    // immediately, rather than holding text that has already been sent.
    setDraft("");
    await send(text);
  }

  return (
    <div
      data-testid="chat-composer"
      css={{
        display: "flex",
        gap: theme.space(2),
        alignItems: "flex-end",
      }}
    >
      <Input.TextArea
        ref={inputRef}
        autoFocus={autoFocus}
        data-testid="chat-input"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onPressEnter={(event) => {
          // Enter sends, Shift+Enter breaks the line — the convention every
          // other chat box uses, so doing otherwise is its own bug report.
          if (event.shiftKey) return;
          event.preventDefault();
          void submit();
        }}
        disabled={disabled}
        placeholder="Ask the agent something…"
        /* One line to begin with, wherever it is. The inviting variant opened three
           rows deep, which on an empty page made the box look like a form to fill in
           rather than a question to ask; it grows as soon as there is anything to
           grow for. */
        autoSize={{ minRows: 1, maxRows: 6 }}
        css={{
          flex: 1,
          // Bigger type when the box *is* the page, but no extra padding: the panel
          // around it supplies that, and doubling it left the text floating.
          ...(variant === "inviting" ? { fontSize: 15 } : {}),
          /*
           * Opaque while disabled, not faded.
           *
           * antd fades a disabled control, which over a dark page makes the composer
           * look like it is still loading rather than deliberately unavailable — and
           * the message above it explaining why is then competing with something that
           * looks broken. It keeps its own surface and says its state through the
           * muted text and the cursor instead.
           */
          "&:disabled, &.ant-input-disabled": {
            opacity: 1,
            background: theme.color.bgElevated,
            color: theme.color.textMuted,
            cursor: "not-allowed",
          },
        }}
      />

      <Space size={8}>
        {isStreaming && onCancel ? (
          <Button
            data-testid="chat-cancel"
            icon={<Square size={14} />}
            onClick={() => void onCancel()}
          >
            Stop
          </Button>
        ) : (
          <Button
            type="primary"
            data-testid="chat-send"
            icon={<Send size={14} />}
            disabled={disabled || draft.trim() === ""}
            // Opaque for the same reason as the box: a faded primary button reads as a
            // page still settling rather than a control waiting for input.
            css={{ "&:disabled": { opacity: 1 } }}
            onClick={() => void submit()}
          >
            Send
          </Button>
        )}
      </Space>
    </div>
  );
}
