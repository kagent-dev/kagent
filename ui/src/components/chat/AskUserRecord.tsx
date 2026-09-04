import { Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { CheckCircle2, MessageCircleQuestion } from "lucide-react";
import type { ChatAskUserPart } from "@/api";

const { Text } = Typography;

/** The durable, read-only transcript form of a completed ask-user exchange. */
export function AskUserRecord({ part }: { part: ChatAskUserPart }) {
  const theme = useTheme();
  const { questions, answers, askedBy } = part.interaction;
  const rows = questions.length > 0
    ? questions.map((question, index) => ({
        question: question.question || `Question ${index + 1}`,
        answer: answers[index] ?? [],
      }))
    : answers.map((answer, index) => ({ question: `Answer ${index + 1}`, answer }));

  return (
    <div
      data-testid="chat-ask-user-record"
      css={{
        border: `1px solid ${theme.color.border}`,
        borderRadius: theme.radius.md,
        background: theme.color.bgElevated,
        padding: theme.space(3),
        display: "grid",
        gap: theme.space(3),
      }}
    >
      <div css={{ display: "flex", alignItems: "center", gap: theme.space(2) }}>
        <MessageCircleQuestion size={16} css={{ color: theme.color.primary }} />
        <Text css={{ color: "inherit", fontWeight: 600 }}>Question answered</Text>
        {askedBy ? (
          <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>for {askedBy}</Text>
        ) : null}
        <Tag color="success" css={{ marginInlineStart: "auto" }}>
          <CheckCircle2 size={12} css={{ marginInlineEnd: 4, verticalAlign: -2 }} />
          Answered
        </Tag>
      </div>

      <div css={{ display: "grid", gap: theme.space(2) }}>
        {rows.map((row, index) => (
          <div key={index} css={{ display: "grid", gap: theme.space(1) }}>
            <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>{row.question}</Text>
            <Text data-testid="chat-ask-user-answer" css={{ fontSize: 13 }}>
              {row.answer.length > 0 ? row.answer.join(", ") : "No answer recorded"}
            </Text>
          </div>
        ))}
      </div>
    </div>
  );
}
