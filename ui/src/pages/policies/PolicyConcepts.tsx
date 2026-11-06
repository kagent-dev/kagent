import type { ReactNode } from "react";
import { Card } from "antd";
import { useTheme, type Theme } from "@emotion/react";
import { egressCopy } from "@/components/egress-policy-form/egressPolicyCopy";
import { FlowArrow, MergeArrow } from "@/pages/agents/AgentConcepts";

const copy = egressCopy.concepts;

// region Styles

const lineStyle = (theme: Theme) => ({
  margin: 0,
  color: theme.color.textMuted,
  fontSize: 12,
  lineHeight: 1.9,
});

const boxStyle = (theme: Theme) => ({
  flex: "1 1 200px",
  minWidth: 0,
  display: "flex",
  flexDirection: "column" as const,
  justifyContent: "center",
  gap: 2,
  padding: theme.space(3),
  borderRadius: theme.radius.md,
  border: `1px solid ${theme.color.border}`,
  background: theme.color.bg,
});

// region Helpers

function Box({ kind, detail }: { kind: string; detail: ReactNode }) {
  const theme = useTheme();
  return (
    <div css={boxStyle(theme)} data-testid={`policy-concept-${kind}`}>
      <div css={{ fontFamily: theme.font.mono, fontSize: 13, color: theme.color.text }}>{kind}</div>
      <div css={{ fontSize: 12, color: theme.color.textMuted }}>{detail}</div>
    </div>
  );
}

// region Component

/** What an egress policy is, in three sentences and one diagram. */
export function PolicyConcepts() {
  const theme = useTheme();
  const { boxes } = copy;

  return (
    <Card size="small" title={copy.title} data-testid="policy-concepts" css={{ marginBottom: theme.space(4) }}>
      <p css={lineStyle(theme)}>{copy.what}</p>
      <p css={lineStyle(theme)}>{copy.credential}</p>
      <p css={lineStyle(theme)}>{copy.noPolicyNeeded}</p>

      {/* The label stands in for the boxes, which repeat the sentences above as a picture. */}
      <div
        role="img"
        aria-label={copy.diagramLabel}
        data-testid="policy-concepts-diagram"
        css={{ display: "flex", alignItems: "stretch", flexWrap: "wrap", gap: theme.space(3), marginTop: theme.space(5) }}
      >
        <div css={{ display: "grid", gap: theme.space(2), flex: "1 1 220px", minWidth: 0 }}>
          <Box {...boxes.agent} />
          <Box {...boxes.sandboxTemplate} />
        </div>
        <MergeArrow theme={theme} />
        <Box {...boxes.policy} />
        {/* Together, so a narrow window wraps the arrow with the box it points at. */}
        <div css={{ display: "flex", alignItems: "stretch", gap: theme.space(3), flex: "1 1 260px", minWidth: 0 }}>
          <FlowArrow theme={theme} />
          <Box {...boxes.hosts} />
        </div>
      </div>
    </Card>
  );
}
