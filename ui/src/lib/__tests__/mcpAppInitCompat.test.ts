import { describe, expect, test } from "@jest/globals";
import {
  installMcpAppInitCompat,
  normalizeMcpAppInitializeMessage,
} from "@/lib/mcpAppInitCompat";

describe("normalizeMcpAppInitializeMessage", () => {
  test("maps clientInfo to appInfo when appInfo is missing", () => {
    const data = {
      jsonrpc: "2.0",
      id: 1,
      method: "ui/initialize",
      params: {
        protocolVersion: "2026-01-26",
        capabilities: {},
        appCapabilities: { availableDisplayModes: ["inline"] },
        clientInfo: { name: "weather-dashboard", version: "1.0.0" },
      },
    };
    expect(normalizeMcpAppInitializeMessage(data)).toBe(true);
    expect(data.params).toMatchObject({
      appInfo: { name: "weather-dashboard", version: "1.0.0" },
    });
  });

  test("preserves an optional title from clientInfo", () => {
    const data = {
      method: "ui/initialize",
      params: { clientInfo: { name: "w", version: "1.0.0", title: "Weather" } },
    };
    normalizeMcpAppInitializeMessage(data);
    expect((data.params as { appInfo?: unknown }).appInfo).toEqual({
      name: "w",
      version: "1.0.0",
      title: "Weather",
    });
  });

  test("leaves conformant appInfo untouched", () => {
    const data = {
      method: "ui/initialize",
      params: {
        appInfo: { name: "conformant", version: "2.0.0" },
        clientInfo: { name: "ignored", version: "9.9.9" },
      },
    };
    expect(normalizeMcpAppInitializeMessage(data)).toBe(false);
    expect(data.params.appInfo).toEqual({ name: "conformant", version: "2.0.0" });
  });

  test("ignores non-initialize messages", () => {
    expect(
      normalizeMcpAppInitializeMessage({
        method: "ui/notifications/size-changed",
        params: { width: 760, height: 600 },
      }),
    ).toBe(false);
  });

  test("ignores initialize without a usable clientInfo", () => {
    expect(
      normalizeMcpAppInitializeMessage({
        method: "ui/initialize",
        params: { clientInfo: { name: "missing-version" } },
      }),
    ).toBe(false);
  });

  test("is null-safe for non-object input", () => {
    expect(normalizeMcpAppInitializeMessage(null)).toBe(false);
    expect(normalizeMcpAppInitializeMessage("ui/initialize")).toBe(false);
    expect(normalizeMcpAppInitializeMessage(undefined)).toBe(false);
  });
});

describe("installMcpAppInitCompat (runtime mechanism)", () => {
  // Mirrors production: the compat listener is installed first, then the
  // ext-apps transport attaches its listener on connect. The transport must
  // observe the appInfo we patched onto the same stable event.data reference.
  test("fix is visible to a later-registered transport-style listener", () => {
    installMcpAppInitCompat();

    let appInfoSeenByTransport: unknown;
    window.addEventListener("message", (event: MessageEvent) => {
      appInfoSeenByTransport = (event.data as { params?: { appInfo?: unknown } })?.params?.appInfo;
    });

    window.dispatchEvent(
      new MessageEvent("message", {
        data: {
          jsonrpc: "2.0",
          id: 1,
          method: "ui/initialize",
          params: { clientInfo: { name: "weather-dashboard", version: "1.0.0" } },
        },
      }),
    );

    expect(appInfoSeenByTransport).toEqual({
      name: "weather-dashboard",
      version: "1.0.0",
    });
  });
});
