import { describe, expect, it } from "vitest";
import type { ModelConfig } from "@/api";
import {
  buildModelPayload,
  emptyModelDraft,
  modelDraftFrom,
  modelDraftIssues,
  newHeaderRow,
  type ModelDraft,
} from "./modelDraft";

function draft(overrides: Partial<ModelDraft> = {}): ModelDraft {
  return {
    ...emptyModelDraft(),
    name: "my-model",
    namespace: "kagent",
    provider: "OpenAI",
    model: "gpt-4.1",
    ...overrides,
  };
}

const validName = (v: string) => /^[a-z0-9-]+$/.test(v);
const issuesFor = (
  d: ModelDraft,
  extra: Partial<Parameters<typeof modelDraftIssues>[1]> = {},
) =>
  modelDraftIssues(d, {
    isValidName: validName,
    nameHint: "bad name",
    requiredParamKeys: [],
    isEdit: false,
    ...extra,
  });

describe("buildModelPayload — provider params", () => {
  it("puts params in the provider's spec block, coercing numeric fields", () => {
    const req = buildModelPayload(
      draft({ params: { temperature: "0.7", maxTokens: "1024", baseUrl: "" } }),
    );
    expect(req.spec.openAI).toEqual({ temperature: "0.7", maxTokens: 1024 });
    expect(req.spec.openAI).not.toHaveProperty("baseUrl");
  });

  it("routes params to the right block per provider (Azure required params)", () => {
    const req = buildModelPayload(
      draft({
        provider: "AzureOpenAI",
        model: "gpt-4o",
        params: {
          azureEndpoint: "https://x.openai.azure.com",
          apiVersion: "2024-06-01",
        },
      }),
    );
    expect(req.spec.azureOpenAI).toEqual({
      azureEndpoint: "https://x.openai.azure.com",
      apiVersion: "2024-06-01",
    });
    expect(req.spec.openAI).toBeUndefined();
  });
});

describe("buildModelPayload — authentication modes", () => {
  it("apiKey: sends an inline key and lets the backend name the Secret", () => {
    const req = buildModelPayload(draft({ authType: "apiKey", apiKey: "sk-123" }));
    expect(req.apiKey).toBe("sk-123");
    expect(req.spec.apiKeySecret).toBeUndefined();
    expect(req.spec.apiKeyPassthrough).toBeUndefined();
  });

  it("secret: references an existing Secret and sends no inline key", () => {
    const req = buildModelPayload(
      draft({
        authType: "secret",
        apiKeySecret: "kagent-openai",
        apiKeySecretKey: "OPENAI_API_KEY",
      }),
    );
    expect(req.apiKey).toBeUndefined();
    expect(req.spec.apiKeySecret).toBe("kagent-openai");
    expect(req.spec.apiKeySecretKey).toBe("OPENAI_API_KEY");
  });

  it("passthrough: sets apiKeyPassthrough and stores nothing", () => {
    const req = buildModelPayload(draft({ authType: "passthrough" }));
    expect(req.spec.apiKeyPassthrough).toBe(true);
    expect(req.apiKey).toBeUndefined();
    expect(req.spec.apiKeySecret).toBeUndefined();
  });

  it("none: sends no credential at all", () => {
    const req = buildModelPayload(draft({ authType: "none", apiKey: "ignored" }));
    expect(req.apiKey).toBeUndefined();
    expect(req.spec.apiKeySecret).toBeUndefined();
    expect(req.spec.apiKeyPassthrough).toBeUndefined();
  });

  it("Ollama never sends a credential whatever the mode says", () => {
    const req = buildModelPayload(
      draft({ provider: "Ollama", model: "llama3.2", authType: "apiKey", apiKey: "sk" }),
    );
    expect(req.apiKey).toBeUndefined();
    expect(req.spec.apiKeyPassthrough).toBeUndefined();
  });
});

describe("buildModelPayload — headers, TLS, Ollama tag", () => {
  it("builds defaultHeaders from non-empty rows", () => {
    const req = buildModelPayload(
      draft({
        defaultHeaders: [
          { ...newHeaderRow(), key: "X-Api-Version", value: "2024-01" },
          { ...newHeaderRow(), key: "", value: "dropped" },
        ],
      }),
    );
    expect(req.spec.defaultHeaders).toEqual({ "X-Api-Version": "2024-01" });
  });

  it("builds spec.tls only from the options that are set", () => {
    const req = buildModelPayload(
      draft({
        tls: {
          caCertSecretRef: "my-ca",
          caCertSecretKey: "ca.crt",
          disableSystemCAs: false,
          disableVerify: true,
        },
      }),
    );
    expect(req.spec.tls).toEqual({
      caCertSecretRef: "my-ca",
      caCertSecretKey: "ca.crt",
      disableVerify: true,
    });
  });

  it("omits spec.tls when nothing is set", () => {
    expect(buildModelPayload(draft()).spec.tls).toBeUndefined();
  });

  it("appends a non-default Ollama tag as model:tag", () => {
    const req = buildModelPayload(
      draft({ provider: "Ollama", model: "llama3.2", modelTag: "8b" }),
    );
    expect(req.spec.model).toBe("llama3.2:8b");
  });
});

describe("modelDraftFrom — round trip", () => {
  it("infers the auth mode and carries headers and TLS back", () => {
    const config: ModelConfig = {
      ref: "kagent/gpt",
      spec: {
        provider: "OpenAI",
        model: "gpt-4.1",
        openAI: { maxTokens: 1024 },
        apiKeySecret: "gpt-secret",
        apiKeySecretKey: "OPENAI_API_KEY",
        defaultHeaders: { "X-Trace": "on" },
        tls: { caCertSecretRef: "ca", disableVerify: true },
      },
    };
    const d = modelDraftFrom(config);
    expect(d.authType).toBe("secret");
    expect(d.apiKeySecret).toBe("gpt-secret");
    expect(d.params).toEqual({ maxTokens: "1024" });
    expect(d.defaultHeaders.map((h) => [h.key, h.value])).toEqual([["X-Trace", "on"]]);
    expect(d.tls.caCertSecretRef).toBe("ca");
    expect(d.tls.disableVerify).toBe(true);
  });

  it("reads passthrough and splits an Ollama tag", () => {
    expect(
      modelDraftFrom({
        ref: "kagent/x",
        spec: { provider: "OpenAI", model: "gpt-4.1", apiKeyPassthrough: true },
      }).authType,
    ).toBe("passthrough");

    const ollama = modelDraftFrom({
      ref: "kagent/llama",
      spec: { provider: "Ollama", model: "llama3.2:8b" },
    });
    expect(ollama.model).toBe("llama3.2");
    expect(ollama.modelTag).toBe("8b");
    expect(ollama.authType).toBe("none");
  });
});

describe("modelDraftIssues", () => {
  it("requires an inline key only when creating in apiKey mode", () => {
    const msg = "An API key is required, or choose another authentication type.";
    expect(issuesFor(draft({ authType: "apiKey", apiKey: "" }))).toContain(msg);
    expect(issuesFor(draft({ authType: "apiKey", apiKey: "" }), { isEdit: true })).not.toContain(msg);
    expect(issuesFor(draft({ authType: "secret" }))).not.toContain(msg);
    expect(issuesFor(draft({ authType: "none" }))).not.toContain(msg);
    expect(
      issuesFor(draft({ provider: "Ollama", model: "llama3.2", authType: "apiKey", apiKey: "" })),
    ).not.toContain(msg);
  });

  it("flags a secret key given without a secret", () => {
    expect(
      issuesFor(draft({ authType: "secret", apiKeySecret: "", apiKeySecretKey: "K" })),
    ).toContain("A secret key needs the secret that holds it.");
  });

  it("flags a missing required parameter and a non-numeric numeric field", () => {
    expect(
      issuesFor(draft({ authType: "none", params: {} }), { requiredParamKeys: ["region"] }),
    ).toContain("region is required.");
    expect(
      issuesFor(draft({ authType: "none", params: { maxTokens: "lots" } })),
    ).toContain("maxTokens must be a number.");
  });
});
