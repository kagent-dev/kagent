import { describe, expect, it } from "vitest";
import type { ModelConfig } from "@/api";
import {
  authTypeForProvider,
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
  it("apiKey: creates an owned Secret and references it in headers", () => {
    const req = buildModelPayload(draft({ authType: "apiKey", apiKey: "sk-123" }));
    expect(req.apiKey).toBeUndefined();
    expect(req.secrets).toEqual([{ name: "my-model", key: "OPENAI_API_KEY", value: "sk-123" }]);
    expect(req.spec.headers).toEqual([{ name: "Authorization", prefix: "Bearer ", valueFrom: { secretKeyRef: { name: "my-model", key: "OPENAI_API_KEY" } } }]);
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
    expect(req.spec.headers?.[0]?.valueFrom.secretKeyRef?.name).toBe("kagent-openai");
    expect(req.spec.headers?.[0]?.valueFrom.secretKeyRef?.key).toBe("OPENAI_API_KEY");
  });

  it("passthrough: sets tokenPassthrough and stores nothing", () => {
    const req = buildModelPayload(draft({ authType: "passthrough" }));
    expect(req.spec.headers?.[0]?.valueFrom.tokenPassthrough).toEqual({});
    expect(req.apiKey).toBeUndefined();
    expect(req.spec.headers?.[0]?.valueFrom.secretKeyRef?.name).toBeUndefined();
  });

  it("none: sends no credential at all", () => {
    const req = buildModelPayload(draft({ authType: "none", apiKey: "ignored" }));
    expect(req.apiKey).toBeUndefined();
    expect(req.spec.headers?.[0]?.valueFrom.secretKeyRef?.name).toBeUndefined();
    expect(req.spec.headers?.[0]?.valueFrom.tokenPassthrough).toBeUndefined();
  });

  it("Ollama passthrough uses the shared header contract", () => {
    const req = buildModelPayload(draft({ provider: "Ollama", model: "llama3.2", authType: "passthrough" }));
    expect(req.spec.headers).toEqual([{ name: "Authorization", prefix: "Bearer ", valueFrom: { tokenPassthrough: {} } }]);
  });

  it("Ollama names a Secret, which is what authenticates an Ollama Cloud model", () => {
    const req = buildModelPayload(
      draft({
        provider: "Ollama",
        model: "deepseek-v4-flash:0731-cloud",
        authType: "secret",
        apiKeySecret: "ollama-cloud-key",
        apiKeySecretKey: "OLLAMA_API_KEY",
      }),
    );
    expect(req.spec.headers?.[0]?.valueFrom.secretKeyRef?.name).toBe("ollama-cloud-key");
    expect(req.spec.headers?.[0]?.valueFrom.secretKeyRef?.key).toBe("OLLAMA_API_KEY");
    // The key is mounted for the runtime, never serialized into the request.
    expect(req.apiKey).toBeUndefined();
    expect(req.spec.headers?.[0]?.valueFrom.tokenPassthrough).toBeUndefined();
  });

  it("Ollama defaults the Secret key when only the Secret is named", () => {
    const req = buildModelPayload(
      draft({
        provider: "Ollama",
        model: "deepseek-v4-flash:0731-cloud",
        authType: "secret",
        apiKeySecret: "ollama-key",
      }),
    );
    expect(req.spec.headers?.[0]?.valueFrom.secretKeyRef?.name).toBe("ollama-key");
    expect(req.spec.headers?.[0]?.valueFrom.secretKeyRef?.key).toBe("OLLAMA_API_KEY");
  });

  it("Ollama keeps an explicit Secret key name when one is given", () => {
    const req = buildModelPayload(
      draft({
        provider: "Ollama",
        model: "deepseek-v4-flash:0731-cloud",
        authType: "secret",
        apiKeySecret: "ollama-key",
        apiKeySecretKey: "MY_OLLAMA_KEY",
      }),
    );
    expect(req.spec.headers?.[0]?.valueFrom.secretKeyRef?.key).toBe("MY_OLLAMA_KEY");
  });

  it("Ollama with no credential stays credential-free", () => {
    const req = buildModelPayload(
      draft({ provider: "Ollama", model: "llama3.2", authType: "none" }),
    );
    expect(req.apiKey).toBeUndefined();
    expect(req.spec.headers?.[0]?.valueFrom.secretKeyRef?.name).toBeUndefined();
    expect(req.spec.headers?.[0]?.valueFrom.tokenPassthrough).toBeUndefined();
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
          disableVerify: true,
        },
      }),
    );
    expect(req.spec.tls).toEqual({
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
        headers: [{ name: "Authorization", prefix: "Bearer ", valueFrom: { secretKeyRef: { name: "gpt-secret", key: "OPENAI_API_KEY" } } }],
        defaultHeaders: { "X-Trace": "on" },
        tls: { disableVerify: true },
      },
    };
    const d = modelDraftFrom(config);
    expect(d.authType).toBe("secret");
    expect(d.apiKeySecret).toBe("gpt-secret");
    expect(d.params).toEqual({ maxTokens: "1024" });
    expect(d.defaultHeaders.map((h) => [h.key, h.value])).toEqual([["X-Trace", "on"]]);
    expect(d.tls.disableVerify).toBe(true);
  });

  it("reads passthrough and splits an Ollama tag", () => {
    expect(
      modelDraftFrom({
        ref: "kagent/x",
        spec: { provider: "OpenAI", model: "gpt-4.1", headers: [{ name: "Authorization", valueFrom: { tokenPassthrough: {} } }] },
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

  it("surfaces an Ollama Secret rather than collapsing it to none", () => {
    // The Secret is a real choice for Ollama — it is what authenticates a cloud
    // model — so reopening the form must not silently drop it.
    const d = modelDraftFrom({
      ref: "kagent/ollama-cloud",
      spec: {
        provider: "Ollama",
        model: "deepseek-v4-flash:0731-cloud",
        headers: [{ name: "Authorization", prefix: "Bearer ", valueFrom: { secretKeyRef: { name: "ollama-cloud-key", key: "OLLAMA_API_KEY" } } }],
      },
    });
    expect(d.authType).toBe("secret");
    expect(d.apiKeySecret).toBe("ollama-cloud-key");
    expect(d.apiKeySecretKey).toBe("OLLAMA_API_KEY");
  });
});

describe("modelDraftIssues", () => {
  it("requires an inline key only when creating in apiKey mode", () => {
    const msg = "An API key is required, or choose another authentication type.";
    expect(issuesFor(draft({ authType: "apiKey", apiKey: "" }))).toContain(msg);
    expect(issuesFor(draft({ authType: "apiKey", apiKey: "", apiKeySecret: "owned" }), { isEdit: true })).not.toContain(msg);
    expect(issuesFor(draft({ authType: "secret" }))).not.toContain(msg);
    expect(issuesFor(draft({ authType: "none" }))).not.toContain(msg);
    expect(
      issuesFor(draft({ provider: "Ollama", model: "llama3.2", authType: "apiKey", apiKey: "" })),
    ).toContain(msg);
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

// A new draft starts on "apiKey", but Ollama offers only "secret" and "none".
// Leaving the mode alone on a provider switch rendered a radio group with
// nothing selected and no way back to no credential.
describe("authTypeForProvider", () => {
  it("moves off a mode Ollama does not offer", () => {
    expect(authTypeForProvider("Ollama", "apiKey")).toBe("none");
    expect(authTypeForProvider("Ollama", "passthrough")).toBe("passthrough");
  });

  it("keeps the modes Ollama does offer", () => {
    expect(authTypeForProvider("Ollama", "secret")).toBe("secret");
    expect(authTypeForProvider("Ollama", "none")).toBe("none");
  });

  it("keeps a passthrough selection where the provider allows it", () => {
    expect(authTypeForProvider("OpenAI", "passthrough")).toBe("passthrough");
  });

  it("keeps passthrough for Gemini header authentication", () => {
    expect(authTypeForProvider("Gemini", "passthrough")).toBe("passthrough");
  });

  it("keeps the other modes unchanged", () => {
    expect(authTypeForProvider("OpenAI", "secret")).toBe("secret");
    expect(authTypeForProvider("Anthropic", "apiKey")).toBe("apiKey");
  });
});

it("preserves multiple credential headers when editing unrelated model settings", () => {
  const config: ModelConfig = { ref: "kagent/gpt", spec: { provider: "OpenAI", model: "gpt", headers: [
    { name: "Authorization", prefix: "Bearer ", valueFrom: { tokenPassthrough: {} } },
    { name: "X-Tenant-Key", prefix: "Token ", valueFrom: { secretKeyRef: { name: "tenant", key: "key" } } },
  ] } };
  const edited = modelDraftFrom(config);
  edited.model = "gpt-new";
  expect(buildModelPayload(edited).spec.headers).toEqual(config.spec.headers);
  edited.additionalCredentialHeaders[0].name = "X-Changed";
  expect(config.spec.headers?.[1].name).toBe("X-Tenant-Key");
});
