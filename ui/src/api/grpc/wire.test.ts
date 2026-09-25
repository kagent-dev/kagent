import { describe, expect, it } from "vitest";
import { wrap } from "./wire";

describe("wrap", () => {
  // protobuf-es turns an `undefined`-valued key into a Struct entry with no kind
  // set, which Go refuses to unmarshal — the whole write fails as an invalid
  // resource. Omitting the key is the only shape that survives the round trip.
  it("drops keys whose value is undefined, at every depth", () => {
    const { value } = wrap("RemoteMCPServer", {
      metadata: { name: "s", namespace: "kagent" },
      spec: {
        url: "http://blah.com/mcp",
        protocol: "STREAMABLE_HTTP",
        sseReadTimeout: undefined,
        timeout: undefined,
        nested: { kept: 1, gone: undefined },
      },
    });

    const spec = (value as { spec: Record<string, unknown> }).spec;
    expect("sseReadTimeout" in spec).toBe(false);
    expect("timeout" in spec).toBe(false);
    expect(spec.nested).toEqual({ kept: 1 });
    expect(spec.url).toBe("http://blah.com/mcp");
  });

  it("keeps the kind it was given and the values that are set", () => {
    const wrapped = wrap("AgentTemplate", { spec: { replicas: 0, on: false, note: "" } });
    expect(wrapped.kind).toBe("AgentTemplate");
    expect(wrapped.value).toEqual({ spec: { replicas: 0, on: false, note: "" } });
  });
});
