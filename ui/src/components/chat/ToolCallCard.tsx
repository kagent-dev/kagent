import { Tag, Typography } from "antd";
import { useMemo } from "react";
import { useTheme } from "@emotion/react";
import { ChevronRight, Wrench } from "lucide-react";
import type { ChatDataPart } from "@/api";
import { stableJson } from "./stableJson";
import { splitToolImages, type ToolImage } from "./toolImages";

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
  const wasNotRun = part.dataKind === "tool_not_run";
  const name = typeof part.data.name === "string" ? part.data.name : "tool";
  /*
   * Memoised because an image's `src` is its whole base64 payload, megabytes for a
   * dashboard panel, and the card re-renders while the chat streams. `part` keeps
   * its identity across streamed updates to other messages; only a history re-read
   * (paused while a turn runs) hands over new objects.
   */
  const { body, failed, sources } = useMemo(() => {
    const { images, ...rest } = describe(part);
    return { ...rest, sources: images.map((image) => `data:${image.mimeType};base64,${image.data}`) };
  }, [part]);

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
          color={failed ? "error" : isCall ? "processing" : wasNotRun ? "default" : "success"}
          css={{ marginInlineStart: "auto" }}
        >
          {failed ? "failed" : isCall ? "called" : wasNotRun ? "not run" : "result"}
        </Tag>
      </div>
      <pre
        data-testid="chat-tool-payload"
        css={{
          margin: 0,
          padding: theme.space(2),
          border: `1px solid ${theme.color.border}`,
          borderRadius: theme.radius.sm,
          background: theme.color.bg,
          fontFamily: theme.font.mono,
          fontSize: 12,
          color: theme.color.text,
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
      {/* Not through markdown: react-markdown strips `data:` URIs. */}
      {sources.map((src, index) => (
        <img
          key={index}
          data-testid="chat-tool-image"
          src={src}
          alt={`${name} result image ${index + 1}`}
          css={{
            display: "block",
            maxWidth: "100%",
            maxHeight: 480,
            borderRadius: theme.radius.sm,
            border: `1px solid ${theme.color.border}`,
          }}
        />
      ))}
    </div>
  );
}

/** Pulls the readable payload out of either shape, and notices a failure. */
function describe(part: ChatDataPart): { body: string; failed: boolean; images: ToolImage[] } {
  if (part.dataKind === "tool_call") {
    return { body: stableJson(part.data.args ?? {}), failed: false, images: [] };
  }
  if (part.dataKind === "tool_not_run") {
    return { body: "Permission was denied, so this tool was not run.", failed: false, images: [] };
  }

  const response = part.data.response;
  if (response && typeof response === "object") {
    // `output` is what the controller sends; `result` is the other spelling seen
    // in the wild. Neither is guaranteed, so an unrecognised response is printed
    // whole rather than reduced to `{}` — a tool that returned a page of text
    // should never be rendered as though it returned nothing, which is exactly
    // what happened when only one of these names was read.
    const { images, rest } = splitToolImages(response as Record<string, unknown>);
    const { output, result, isError, error } = rest as {
      output?: unknown;
      result?: unknown;
      isError?: unknown;
      error?: unknown;
    };
    const payload = output ?? result;

    return {
      body: typeof payload === "string" ? payload : stableJson(payload ?? rest),
      failed: isError === true || error !== undefined,
      images,
    };
  }
  return { body: stableJson(part.data), failed: false, images: [] };
}
