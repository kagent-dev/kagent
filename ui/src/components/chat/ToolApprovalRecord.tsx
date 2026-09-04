import { Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { CheckCircle2, ShieldCheck, XCircle } from "lucide-react";
import type { ChatToolApprovalPart } from "@/api";
import { stableJson } from "./stableJson";

const { Text } = Typography;

/** The durable transcript form of a completed human approval exchange. */
export function ToolApprovalRecord({ part }: { part: ChatToolApprovalPart }) {
  const theme = useTheme();
  const { tools, decisions, askedBy } = part.approval;
  const names = new Map(tools.map((tool) => [tool.id, tool]));
  const approved = decisions.filter((decision) => decision.approved).length;
  const rejected = decisions.length - approved;
  const title =
    approved > 0 && rejected > 0
      ? "Tool permissions decided"
      : rejected > 0
        ? "Tool access rejected"
        : "Tool access approved";

  return (
    <div
      data-testid="chat-tool-approval-record"
      css={{
        border: `1px solid ${rejected > 0 ? theme.color.warningBorder : theme.color.successBorder}`,
        borderRadius: theme.radius.md,
        background: rejected > 0 ? theme.color.warningBg : theme.color.successBg,
        padding: theme.space(3),
        display: "grid",
        gap: theme.space(2),
      }}
    >
      <div css={{ display: "flex", alignItems: "center", gap: theme.space(2) }}>
        <ShieldCheck
          size={16}
          css={{ color: rejected > 0 ? theme.color.warningText : theme.color.successText }}
        />
        <Text css={{ color: "inherit", fontWeight: 600 }}>{title}</Text>
        {askedBy ? (
          <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>for {askedBy}</Text>
        ) : null}
      </div>

      <div css={{ display: "grid", gap: theme.space(1) }}>
        {decisions.map((decision) => {
          const tool = names.get(decision.id);
          const hasDetails =
            (tool !== undefined && Object.keys(tool.args).length > 0) ||
            decision.rejectionReason !== undefined;
          return (
            <details key={decision.id}>
              <summary
                css={{
                  cursor: hasDetails ? "pointer" : "default",
                  display: "flex",
                  alignItems: "center",
                  gap: theme.space(2),
                  listStyle: "none",
                }}
                onClick={hasDetails ? undefined : (event) => event.preventDefault()}
              >
                {decision.approved ? (
                  <CheckCircle2 size={14} css={{ color: theme.color.successText }} />
                ) : (
                  <XCircle size={14} css={{ color: theme.color.dangerText }} />
                )}
                <Text css={{ color: "inherit", fontFamily: theme.font.mono, fontSize: 12 }}>
                  {tool?.name || "Tool"}
                </Text>
                <Tag color={decision.approved ? "success" : "error"}>
                  {decision.approved ? "Approved" : "Rejected"}
                </Tag>
                {hasDetails ? (
                  <Text css={{ color: theme.color.textMuted, fontSize: 11 }}>Details</Text>
                ) : null}
              </summary>
              {tool && Object.keys(tool.args).length > 0 ? (
                <pre
                  data-testid="chat-approval-record-args"
                  css={{
                    margin: `${theme.space(2)} 0 0 ${theme.space(5)}`,
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
              {decision.rejectionReason ? (
                <div css={{ margin: `${theme.space(1)} 0 0 ${theme.space(5)}`, fontSize: 12 }}>
                  {decision.rejectionReason}
                </div>
              ) : null}
            </details>
          );
        })}
      </div>
    </div>
  );
}
