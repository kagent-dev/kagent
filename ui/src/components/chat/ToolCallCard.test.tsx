import { ThemeProvider } from "@emotion/react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { themeFor } from "@/theme/theme";
import { ToolCallCard } from "./ToolCallCard";

describe("ToolCallCard", () => {
  it.each([
    [{ data: "observed alert", error: null, status: "success" }, false],
    [{ output: "observed alert" }, false],
    [{ error: "upstream unavailable" }, true],
    [{ isError: true, error: null }, true],
    [{ status: "error", data: "upstream unavailable", error: null }, true],
  ])("renders tool result failure from %j", (response, failed) => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ToolCallCard
          part={{
            kind: "data",
            dataKind: "tool_result",
            data: { name: "alerts_list_alerts", response },
          }}
        />
      </ThemeProvider>,
    );

    expect(screen.queryByText("failed") !== null).toBe(failed);
  });

  it.each([
    { output: '{"z":2,"a":1}' },
    { result: { z: 2, a: 1 } },
    { data: { z: 2, a: 1 } },
    { data: { z: 2, a: 1 }, error: null, status: "success" },
    { data: '{"z":2,"a":1}', error: null, schema_version: "robusta:v1.0.0" },
  ])("renders structured tool payloads without their envelope: %j", (response) => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ToolCallCard
          part={{ kind: "data", dataKind: "tool_result", data: { name: "lookup", response } }}
        />
      </ThemeProvider>,
    );

    expect(screen.getByTestId("chat-tool-payload").textContent).toBe('{\n  "a": 1,\n  "z": 2\n}');
    expect(screen.queryByText(/schema_version/)).toBeNull();
  });

  it.each(["plain output", "{incomplete JSON", "123", false, 0, ""])(
    "preserves text and scalar tool outputs: %j",
    (output) => {
      render(
        <ThemeProvider theme={themeFor("dark")}>
          <ToolCallCard
            part={{
              kind: "data",
              dataKind: "tool_result",
              data: { name: "lookup", response: { output } },
            }}
          />
        </ThemeProvider>,
      );

      expect(screen.getByTestId("chat-tool-payload").textContent).toBe(String(output));
    },
  );

  it.each([
    [{ custom: "retained" }, '{\n  "custom": "retained"\n}'],
    [
      { data: ["observed alert"], meta: { total: 50 } },
      '{\n  "data": [\n    "observed alert"\n  ],\n  "meta": {\n    "total": 50\n  }\n}',
    ],
    [
      { error: "timeout", partial: ["observed alert"] },
      '{\n  "error": "timeout",\n  "partial": [\n    "observed alert"\n  ]\n}',
    ],
  ])("keeps unrecognised result shapes visible: %j", (response, expected) => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ToolCallCard
          part={{
            kind: "data",
            dataKind: "tool_result",
            data: { name: "lookup", response },
          }}
        />
      </ThemeProvider>,
    );

    expect(screen.getByTestId("chat-tool-payload").textContent).toBe(expected);
  });

  it.each([
    '{"id":12345678901234567890}',
    '{"id":-12345678901234567890}',
    '{"items":[{"id":9007199254740993}]}',
    '[9007199254740993]',
    '{"id":1e20}',
    '{"value":1e400}',
  ])("preserves JSON strings with unsafe numbers: %s", (output) => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ToolCallCard
          part={{
            kind: "data",
            dataKind: "tool_result",
            data: { name: "lookup", response: { output } },
          }}
        />
      </ThemeProvider>,
    );

    expect(screen.getByTestId("chat-tool-payload").textContent).toBe(output);
  });

  it("formats safe integers and numeric strings", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ToolCallCard
          part={{
            kind: "data",
            dataKind: "tool_result",
            data: {
              name: "lookup",
              response: { output: '{"text":"12345678901234567890","id":9007199254740991}' },
            },
          }}
        />
      </ThemeProvider>,
    );

    expect(screen.getByTestId("chat-tool-payload").textContent).toBe(
      '{\n  "id": 9007199254740991,\n  "text": "12345678901234567890"\n}',
    );
  });

  it("keeps invocation arguments in the tool call card", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ToolCallCard
          part={{
            kind: "data",
            dataKind: "tool_call",
            data: {
              name: "k8s_get_resources",
              args: { namespace: "default", kind: "Pod" },
            },
          }}
        />
      </ThemeProvider>,
    );

    expect(screen.getByText(/"namespace": "default"/)).toBeTruthy();
    expect(screen.getByText(/"kind": "Pod"/)).toBeTruthy();
  });

  it("renders a denied legacy invocation as not run rather than failed", () => {
    render(
      <ThemeProvider theme={themeFor("dark")}>
        <ToolCallCard
          part={{
            kind: "data",
            dataKind: "tool_not_run",
            data: {
              name: "k8s_get_resources",
              response: { error: 'error tool "k8s_get_resources" call is rejected' },
            },
          }}
        />
      </ThemeProvider>,
    );

    expect(screen.getByText("not run")).toBeTruthy();
    expect(screen.getByText("Permission was denied, so this tool was not run.")).toBeTruthy();
    expect(screen.queryByText("failed")).toBeNull();
    expect(screen.queryByText(/call is rejected/)).toBeNull();
  });
});
