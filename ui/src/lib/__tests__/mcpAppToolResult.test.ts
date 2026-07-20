import { describe, expect, test } from "@jest/globals";
import {
  buildMcpAppRenderPayload,
  normalizeMcpToolArgs,
  toMcpAppToolResult,
  withMcpAppToolInput,
} from "@/lib/mcpAppToolResult";

describe("mcpAppToolResult", () => {
  test("normalizeMcpToolArgs unwraps nested arguments", () => {
    expect(normalizeMcpToolArgs({ arguments: { location: "New York" } })).toEqual({
      location: "New York",
    });
  });

  test("normalizeMcpToolArgs parses JSON strings", () => {
    expect(normalizeMcpToolArgs('{"location":"Chicago"}')).toEqual({ location: "Chicago" });
  });

  test("toMcpAppToolResult maps ADK output wrapper to structuredContent", () => {
    const result = toMcpAppToolResult(
      { output: { temperature: 33, conditions: "Cloudy", humidity: 82 } },
      "",
    );
    expect(result?.structuredContent).toEqual({
      temperature: 33,
      conditions: "Cloudy",
      humidity: 82,
    });
  });

  test("toMcpAppToolResult preserves MCP structuredContent when content is compacted", () => {
    const result = toMcpAppToolResult(
      {
        content: [{ type: "text", text: "rendered" }],
        structuredContent: { temperature: 36, conditions: "Rain", humidity: 82 },
      },
      "",
    );
    expect(result?.structuredContent).toEqual({
      temperature: 36,
      conditions: "Rain",
      humidity: 82,
    });
  });

  test("withMcpAppToolInput merges location into structuredContent", () => {
    const merged = withMcpAppToolInput(
      {
        content: [{ type: "text", text: "ok" }],
        structuredContent: { temperature: 36, conditions: "Rain", humidity: 82 },
      },
      { location: "Chicago" },
    );
    expect(merged?.structuredContent).toEqual({
      location: "Chicago",
      temperature: 36,
      conditions: "Rain",
      humidity: 82,
    });
  });

  test("buildMcpAppRenderPayload passes location to toolInput and structuredContent", () => {
    const { toolInput, toolResult } = buildMcpAppRenderPayload(
      { output: { temperature: 33, conditions: "Cloudy", humidity: 82 } },
      "",
      { location: "New York" },
    );
    expect(toolInput).toEqual({ location: "New York" });
    expect(toolResult?.structuredContent).toEqual({
      location: "New York",
      temperature: 33,
      conditions: "Cloudy",
      humidity: 82,
    });
  });
});
