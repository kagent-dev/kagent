/**
 * The question an agent stopped to ask, as something the reader can answer.
 *
 * Raw `ask_user` calls and protocol fallback prose are removed at the client
 * boundary. This is therefore the one pending representation and the only one
 * that can end the turn; once answered it becomes a read-only transcript card.
 *
 * ## What it refuses to do
 *
 * - **It does not offer choices for a request it does not understand.** A request
 *   type added after this build is said plainly and the only control offered is the
 *   one that gives it up.
 * - **It does not let the same answer be sent twice.** The runtime refuses the
 *   second, so a control that stayed live would produce a failure the reader caused
 *   by doing the obvious thing.
 * - **It does not send a partial answer.** Every question is answered or none is:
 *   the runtime pairs answers to questions positionally, so a gap silently answers
 *   the wrong question.
 */

import { useMemo, useState } from "react";
import { Alert, Button, Checkbox, Input, Radio, Space } from "antd";
import { useTheme } from "@emotion/react";
import { Check, ShieldAlert, X } from "lucide-react";
import type { PendingRequest, ToolApprovalDecision } from "@/api";
import { stableJson } from "./stableJson";

export function AskUserPrompt({
  request,
  isBusy,
  onAnswer,
  onToolApproval,
  onDismiss,
  onAnswered,
}: {
  request: PendingRequest;
  /** A turn is in flight — the answer is on its way, or something else is. */
  isBusy: boolean;
  onAnswer: (answers: readonly string[][]) => void;
  onToolApproval: (decisions: readonly ToolApprovalDecision[]) => void;
  onDismiss: () => void;
  /**
   * The answer has gone, and the caret belongs somewhere else now.
   *
   * This field exists only because the composer could not end the turn; once it has,
   * the next thing typed is an ordinary message. Leaving the caret in a field that is
   * now disabled makes the reader find the composer with the mouse to carry on.
   */
  onAnswered?: () => void;
}) {
  const theme = useTheme();

  /*
   * One entry per question, in the order they were asked.
   *
   * Positional, because that is how the runtime pairs them — the answers array is
   * matched to the questions array by index and nothing correlates them otherwise.
   * Keeping the shape identical from here to the wire is what stops an answer
   * arriving against the wrong question.
   */
  const questions = request.kind === "ask_user" ? request.questions : [];
  const [answers, setAnswers] = useState<string[][]>(() => questions.map(() => []));
  const [toolDecisions, setToolDecisions] = useState<Map<string, boolean>>(
    () => new Map(),
  );
  const [rejectionReasons, setRejectionReasons] = useState<Map<string, string>>(
    () => new Map(),
  );
  const [isSent, setSent] = useState(false);

  const answered = useMemo(
    () => questions.length > 0 && answers.every((answer) => answer.length > 0),
    [questions.length, answers],
  );

  const setAnswer = (index: number, value: string[]) =>
    setAnswers((current) => current.map((entry, at) => (at === index ? value : entry)));

  /*
   * One question, answered in prose.
   *
   * The case where this panel is a single text field, which is the only shape where
   * Enter can mean "send": with two questions it would send the one still being
   * filled in, and against choices there is nothing to press Enter in.
   */
  const isSoleTextQuestion =
    questions.length === 1 && questions[0].choices.length === 0;

  function submit() {
    if (!answered || isSent || isBusy) return;
    setSent(true);
    onAnswer(answers);
    onAnswered?.();
  }

  function approveTools() {
    if (request.kind !== "tool_approval" || isSent || isBusy) return;
    setSent(true);
    onToolApproval(request.tools.map((tool) => ({ id: tool.id, approved: true })));
    onAnswered?.();
  }

  function setToolDecision(id: string, approved: boolean) {
    if (isSent || isBusy) return;
    setToolDecisions((current) => new Map(current).set(id, approved));
  }

  function setRejectionReason(id: string, reason: string) {
    setRejectionReasons((current) => new Map(current).set(id, reason));
  }

  function submitToolDecisions() {
    if (request.kind !== "tool_approval" || isSent || isBusy) return;
    if (!request.tools.every((tool) => toolDecisions.has(tool.id))) return;
    setSent(true);
    onToolApproval(
      request.tools.map((tool) => {
        const approved = toolDecisions.get(tool.id)!;
        const rejectionReason = rejectionReasons.get(tool.id)?.trim();
        return {
          id: tool.id,
          approved,
          ...(!approved && rejectionReason ? { rejectionReason } : {}),
        };
      }),
    );
    onAnswered?.();
  }

  const discard = (
    <Button size="small" data-testid="chat-dismiss-question" onClick={onDismiss}>
      {request.kind === "tool_approval" ? "Cancel request" : "Discard the question"}
    </Button>
  );

  if (request.kind === "tool_approval") {
    const title = request.askedBy
      ? `${request.askedBy} is asking permission to run ${request.tools.length === 1 ? "a tool" : "tools"}`
      : `The agent is asking permission to run ${request.tools.length === 1 ? "a tool" : "tools"}`;
    return (
      <Alert
        type="warning"
        icon={<ShieldAlert size={18} />}
        showIcon
        data-testid="chat-awaiting-reply"
        data-kind="tool_approval"
        title={title}
        description={
          <div css={{ display: "grid", gap: theme.space(3), marginBlockStart: theme.space(2) }}>
            {request.hint ? <div>{request.hint}</div> : null}
            {request.tools.map((tool) => {
              const decision = toolDecisions.get(tool.id);
              return (
                <div
                  key={tool.id}
                  css={{
                    display: "grid",
                    gap: theme.space(1),
                    padding: theme.space(3),
                    border: `1px solid ${theme.color.border}`,
                    borderRadius: theme.radius.md,
                  }}
                >
                  <div
                    css={{
                      display: "flex",
                      alignItems: "center",
                      gap: theme.space(2),
                      flexWrap: "wrap",
                    }}
                  >
                    <strong
                      data-testid="chat-approval-tool"
                      css={{ fontFamily: theme.font.mono }}
                    >
                      {tool.name}
                    </strong>
                    {request.tools.length > 1 ? (
                      <Space size={4} css={{ marginInlineStart: "auto" }}>
                        <Button
                          size="small"
                          type={decision === true ? "primary" : "default"}
                          icon={<Check size={13} />}
                          disabled={isSent || isBusy}
                          onClick={() => setToolDecision(tool.id, true)}
                        >
                          Allow
                        </Button>
                        <Button
                          size="small"
                          danger={decision === false}
                          type={decision === false ? "primary" : "default"}
                          icon={<X size={13} />}
                          disabled={isSent || isBusy}
                          onClick={() => setToolDecision(tool.id, false)}
                        >
                          Deny
                        </Button>
                      </Space>
                    ) : null}
                  </div>
                  {Object.keys(tool.args).length > 0 ? (
                    <pre
                      data-testid="chat-approval-args"
                      css={{
                        margin: 0,
                        color: theme.color.textMuted,
                        fontFamily: theme.font.mono,
                        fontSize: 12,
                        whiteSpace: "pre-wrap",
                        overflowWrap: "anywhere",
                      }}
                    >
                      {stableJson(tool.args)}
                    </pre>
                  ) : null}
                  {decision === false ? (
                    <Input.TextArea
                      data-testid={`chat-approval-reason-${tool.id}`}
                      aria-label={`Reason for rejecting ${tool.name}`}
                      autoFocus={request.tools.length === 1}
                      autoSize={{ minRows: 1, maxRows: 4 }}
                      maxLength={1000}
                      disabled={isSent || isBusy}
                      placeholder="Reason for rejection (optional)"
                      value={rejectionReasons.get(tool.id) ?? ""}
                      onChange={(event) => setRejectionReason(tool.id, event.target.value)}
                    />
                  ) : null}
                </div>
              );
            })}
            <Space size={8} wrap>
              {request.tools.length === 1 ? (
                <>
                  {toolDecisions.get(request.tools[0].id) === false ? (
                    <>
                      <Button
                        danger
                        type="primary"
                        size="small"
                        data-testid="chat-approval-submit"
                        disabled={isSent || isBusy}
                        onClick={submitToolDecisions}
                      >
                        Send rejection
                      </Button>
                      <Button
                        size="small"
                        disabled={isSent || isBusy}
                        onClick={() => {
                          setToolDecisions(new Map());
                          setRejectionReasons(new Map());
                        }}
                      >
                        Back
                      </Button>
                    </>
                  ) : (
                    <>
                      <Button
                        type="primary"
                        size="small"
                        data-testid="chat-approval-approve"
                        disabled={isSent || isBusy}
                        onClick={approveTools}
                      >
                        Approve
                      </Button>
                      <Button
                        danger
                        size="small"
                        data-testid="chat-approval-reject"
                        disabled={isSent || isBusy}
                        onClick={() => setToolDecision(request.tools[0].id, false)}
                      >
                        Reject
                      </Button>
                    </>
                  )}
                </>
              ) : (
                <Button
                  type="primary"
                  size="small"
                  data-testid="chat-approval-submit"
                  disabled={
                    isSent ||
                    isBusy ||
                    !request.tools.every((tool) => toolDecisions.has(tool.id))
                  }
                  onClick={submitToolDecisions}
                >
                  Submit decisions
                </Button>
              )}
              {discard}
            </Space>
          </div>
        }
      />
    );
  }

  if (request.kind !== "ask_user" || questions.length === 0) {
    /*
     * Something is being asked and this build cannot render it.
     *
     * A turn parked without a request shape this build understands. The question
     * may exist only as prose and carry no correlation id, so no answer can be
     * routed to it.
     */
    return (
      <Alert
        type="info"
        showIcon
        data-testid="chat-awaiting-reply"
        data-kind={request.kind}
        title="The agent asked you something and is waiting"
        description="Its question is in the conversation above. This build cannot offer you its choices — the turn was started without the extension that carries them, so there is nothing to answer against. Reply in a new conversation, or discard the question."
        action={discard}
      />
    );
  }

  return (
    <Alert
      type="info"
      showIcon
      data-testid="chat-awaiting-reply"
      data-kind="ask_user"
      title={
        request.askedBy
          ? `${request.askedBy} asked you something and is waiting`
          : "The agent asked you something and is waiting"
      }
      description={
        <div css={{ display: "grid", gap: theme.space(4), marginBlockStart: theme.space(2) }}>
          {questions.map((question, index) => (
            <div key={`${question.question}-${index}`} css={{ display: "grid", gap: theme.space(2) }}>
              <div data-testid="chat-question" css={{ fontWeight: 500 }}>
                {question.question}
              </div>

              {question.choices.length === 0 ? (
                // No choices offered means the agent wants prose. Answering in the
                // composer would open a new turn, so the field belongs here.
                <Input
                  data-testid={`chat-answer-text-${index}`}
                  disabled={isSent || isBusy}
                  /* The question arrived while the reader was elsewhere on the page,
                     and the one thing to do about it is type in here. Only when this
                     is the whole panel: with several questions, taking the caret
                     would be choosing which one they answer first. */
                  autoFocus={isSoleTextQuestion}
                  value={answers[index]?.[0] ?? ""}
                  onChange={(event) =>
                    setAnswer(index, event.target.value === "" ? [] : [event.target.value])
                  }
                  onPressEnter={
                    isSoleTextQuestion
                      ? (event) => {
                          // Prevented, or the Enter also reaches whatever this panel
                          // sits inside and the page acts on it twice.
                          event.preventDefault();
                          submit();
                        }
                      : undefined
                  }
                  placeholder="Your answer"
                />
              ) : question.multiple ? (
                <Checkbox.Group
                  data-testid={`chat-choices-${index}`}
                  disabled={isSent || isBusy}
                  value={answers[index]}
                  onChange={(value) => setAnswer(index, value as string[])}
                >
                  <Space orientation="vertical" size={4}>
                    {question.choices.map((choice) => (
                      <Checkbox key={choice} value={choice}>
                        {choice}
                      </Checkbox>
                    ))}
                  </Space>
                </Checkbox.Group>
              ) : (
                <Radio.Group
                  data-testid={`chat-choices-${index}`}
                  disabled={isSent || isBusy}
                  value={answers[index]?.[0]}
                  onChange={(event) => setAnswer(index, [event.target.value as string])}
                >
                  <Space orientation="vertical" size={4}>
                    {question.choices.map((choice) => (
                      <Radio key={choice} value={choice}>
                        {choice}
                      </Radio>
                    ))}
                  </Space>
                </Radio.Group>
              )}
            </div>
          ))}

          <Space size={8}>
            <Button
              type="primary"
              size="small"
              data-testid="chat-answer-send"
              // Every question or none: an answers array with a gap in it is paired
              // positionally against the questions and answers the wrong one.
              disabled={!answered || isSent || isBusy}
              onClick={submit}
            >
              {questions.length > 1 ? "Send answers" : "Send answer"}
            </Button>
            {discard}
          </Space>
        </div>
      }
    />
  );
}
