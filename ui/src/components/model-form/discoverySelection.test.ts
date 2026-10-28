import { describe, expect, it } from "vitest";
import { invocationSettings } from "./discoverySelection";
import { buildModelPayload, emptyModelDraft } from "./modelDraft";

describe("discovered model invocation", () => {
  it.each(["chatCompletions", "responses", "messages"])("creates %s configuration with independent credentials", format => {
    const settings = invocationSettings(format, "https://agw.example/team/v1");
    const payload = buildModelPayload({...emptyModelDraft(), ...settings, name:"selected", model:"zai-org/glm-5.2-maas", authType:"secret", apiKeySecret:"invocation", apiKeySecretKey:"token"});
    expect(payload.spec.model).toBe("zai-org/glm-5.2-maas");
    expect(payload.spec.apiKeySecret).toBe("invocation");
    if(format === "messages") {
      expect(payload.spec.provider).toBe("Anthropic");
      expect(payload.spec.anthropic?.baseUrl).toBe("https://agw.example/team");
    } else {
      expect(payload.spec.openAI).toEqual({baseUrl:"https://agw.example/team/v1",apiFormat:format});
    }
  });
  it("preserves arbitrary bases for explicit editing", () => {
    expect(invocationSettings("messages", "https://agw.example/team/api").params.baseUrl).toBe("https://agw.example/team/api");
  });
});
