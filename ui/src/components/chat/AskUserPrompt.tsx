/**
 * The question an agent stopped to ask, as something the reader can answer.
 *
 * The payload is already in the transcript twice — as the `ask_user` tool call and
 * as prose — and both are unusable: one is a JSON blob and the other is a sentence
 * with no way to reply to it. This is the third rendering and the only one that can
 * end the turn.
 *
 * ## What it refuses to do
 *
 * - **It does not offer choices for a request it does not understand.** A
 *   `tool_approval_request`, or a type added after this build, is said plainly and
 *   the only control offered is the one that gives it up. Buttons that post an
 *   answer the runtime will not read are worse than no buttons: the turn resumes,
 *   the agent replies, and nothing reports that the answer went nowhere.
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
import type { PendingRequest } from "@/api";

export function AskUserPrompt({
  request,
  isBusy,
  onAnswer,
  onDismiss,
}: {
  request: PendingRequest;
  /** A turn is in flight — the answer is on its way, or something else is. */
  isBusy: boolean;
  onAnswer: (answers: readonly string[][]) => void;
  onDismiss: () => void;
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
  const [isSent, setSent] = useState(false);

  const answered = useMemo(
    () => questions.length > 0 && answers.every((answer) => answer.length > 0),
    [questions.length, answers],
  );

  const setAnswer = (index: number, value: string[]) =>
    setAnswers((current) => current.map((entry, at) => (at === index ? value : entry)));

  const discard = (
    <Button size="small" data-testid="chat-dismiss-question" onClick={onDismiss}>
      Discard the question
    </Button>
  );

  if (request.kind !== "ask_user" || questions.length === 0) {
    /*
     * Something is being asked and this build cannot render it.
     *
     * Two ways to arrive here, and both are honest to say out loud rather than to
     * guess at: a tool-approval request, which needs its own controls and its own
     * response shape, and a turn that parked without the extension having been
     * activated — in which case the question exists only as prose and carries no
     * correlation id, so no answer of any kind can be routed to it.
     */
    return (
      <Alert
        type="info"
        showIcon
        data-testid="chat-awaiting-reply"
        data-kind={request.kind}
        title={
          request.kind === "tool_approval"
            ? "The agent is asking permission to run a tool"
            : "The agent asked you something and is waiting"
        }
        description={
          request.kind === "tool_approval"
            ? `It wants to run ${request.tools.map((tool) => tool.name).join(", ")}. Approving from here is not something this build can do yet, so the way on is to discard the request.`
            : "Its question is in the conversation above. This build cannot offer you its choices — the turn was started without the extension that carries them, so there is nothing to answer against. Reply in a new conversation, or discard the question."
        }
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
                  value={answers[index]?.[0] ?? ""}
                  onChange={(event) =>
                    setAnswer(index, event.target.value === "" ? [] : [event.target.value])
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
              onClick={() => {
                setSent(true);
                onAnswer(answers);
              }}
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
