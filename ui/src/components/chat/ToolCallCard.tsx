import { Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { ChevronRight, Wrench } from "lucide-react";
import type { ChatDataPart } from "@/api";
import { stableJson } from "./stableJson";

const { Text } = Typography;

/**
 * A tool call or its result.
 *
 * Rendered as a compact card rather than raw JSON: what a reader needs at a
 * glance is which tool ran and whether it worked, with the payload available but
 * not shouting. A failed result is called out, because a tool that errored and a
 * tool that returned nothing look identical otherwise.
 */
export function ToolCallCard({ part }: { part: ChatDataPart }) {
  const theme = useTheme();
  const isCall = part.dataKind === "tool_call";
  const name = typeof part.data.name === "string" ? part.data.name : "tool";
  const { body, failed } = describe(part);

  return (
    <div
      data-testid={isCall ? "chat-tool-call" : "chat-tool-result"}
      data-tool-name={name}
      css={{
        border: `1px solid ${failed ? theme.color.danger : theme.color.border}`,
        borderRadius: theme.radius.md,
        background: theme.color.bgElevated,
        padding: theme.space(3),
        display: "grid",
        gap: theme.space(2),
      }}
    >
      <div css={{ display: "flex", alignItems: "center", gap: theme.space(2) }}>
        {isCall ? (
          <Wrench size={14} css={{ color: theme.color.textMuted }} />
        ) : (
          <ChevronRight size={14} css={{ color: theme.color.textMuted }} />
        )}
        <Text css={{ fontWeight: 600, fontSize: 13 }}>{name}</Text>
        <Tag
          color={failed ? "error" : isCall ? "processing" : "success"}
          css={{ marginInlineStart: "auto" }}
        >
          {failed ? "failed" : isCall ? "called" : "result"}
        </Tag>
      </div>
      <pre
        data-testid="chat-tool-payload"
        css={{
          margin: 0,
          fontFamily: theme.font.mono,
          fontSize: 12,
          color: theme.color.textMuted,
          /*
           * Not wrapped. A tool's payload is JSON, and wrapping it breaks lines wherever
           * the column happens to end — so indentation stops meaning depth, keys and
           * values split across lines, and an identifier or URL is cut mid-token. It
           * scrolls sideways instead, which keeps every line the line the tool actually
           * returned.
           */
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

/** Pulls the readable payload out of either shape, and notices a failure. */
function describe(part: ChatDataPart): { body: string; failed: boolean } {
  if (part.dataKind === "tool_call") {
    return { body: stableJson(part.data.args ?? {}), failed: false };
  }

  const response = part.data.response;
  if (response && typeof response === "object") {
    // `output` is what the controller sends; `result` is the other spelling seen
    // in the wild. Neither is guaranteed, so an unrecognised response is printed
    // whole rather than reduced to `{}` — a tool that returned a page of text
    // should never be rendered as though it returned nothing, which is exactly
    // what happened when only one of these names was read.
    const { output, result, isError, error } = response as {
      output?: unknown;
      result?: unknown;
      isError?: unknown;
      error?: unknown;
    };
    const payload = output ?? result;

    return {
      body: typeof payload === "string" ? payload : stableJson(payload ?? response),
      failed: isError === true || error !== undefined,
    };
  }
  return { body: stableJson(part.data), failed: false };
}
