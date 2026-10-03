import { Button, Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Copy, FileJson } from "lucide-react";
import toast from "react-hot-toast";
import type { ChatDataPart } from "@/api";
import { copyText } from "@/components/common/copyText";
import { stableJson } from "./stableJson";

const { Text } = Typography;

/** A schema-constrained terminal answer, distinct from runtime tool traffic. */
export function StructuredOutputCard({ part }: { part: ChatDataPart }) {
  const theme = useTheme();
  const body = stableJson(part.data);

  async function copy(): Promise<void> {
    if (await copyText(body)) toast.success("JSON copied");
    else toast.error("Could not copy JSON");
  }

  return (
    <div
      data-testid="chat-structured-output"
      css={{
        border: `1px solid ${theme.color.border}`,
        borderRadius: theme.radius.md,
        background: theme.color.bgElevated,
        padding: theme.space(3),
        display: "grid",
        gap: theme.space(2),
      }}
    >
      <div css={{ display: "flex", alignItems: "center", gap: theme.space(2) }}>
        <FileJson size={15} css={{ color: theme.color.textMuted }} />
        <Text css={{ fontWeight: 600, fontSize: 13 }}>Structured result</Text>
        <Tag color="success">JSON</Tag>
        <Button
          size="small"
          icon={<Copy size={13} />}
          onClick={() => void copy()}
          css={{ marginInlineStart: "auto" }}
          data-testid="chat-structured-output-copy"
        >
          Copy JSON
        </Button>
      </div>
      <pre
        data-testid="chat-structured-output-json"
        css={{
          margin: 0,
          padding: theme.space(2),
          border: `1px solid ${theme.color.border}`,
          borderRadius: theme.radius.sm,
          background: theme.color.bg,
          color: theme.color.text,
          fontFamily: theme.font.mono,
          fontSize: 12,
          whiteSpace: "pre",
          overflowX: "auto",
          maxWidth: "100%",
        }}
      >
        {body}
      </pre>
    </div>
  );
}
