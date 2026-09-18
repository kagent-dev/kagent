/**
 * What the model form holds while it is being filled in, and how it maps to and
 * from a stored configuration.
 *
 * Kept apart from the form so the mapping — the fiddly part: which parameters go
 * in which provider block, how a credential is expressed, how an Ollama tag rides
 * on the model name — can be read, reversed, and tested without a DOM.
 */

import {
  toRef,
  type CreateModelConfigRequest,
  type ModelConfig,
  type ModelConfigSpec,
  type TLSConfig,
} from "@/api";
import { DEFAULT_NAMESPACE } from "@/components/common/resourceName";
import { OLLAMA_DEFAULT_TAG, OLLAMA_PROVIDER } from "./providerInfo";

/**
 * How the model authenticates:
 *  - `apiKey`      — a key typed here, materialised into a Secret the backend owns
 *  - `secret`      — an existing Kubernetes Secret referenced by name and key
 *  - `passthrough` — the caller's own credential is forwarded; nothing is stored
 *  - `none`        — no credential (a local or unauthenticated endpoint)
 */
export type ModelAuthType = "apiKey" | "secret" | "passthrough" | "none";

/** One editable default-header row; `id` keeps rows stable across edits. */
export interface HeaderRow {
  id: string;
  key: string;
  value: string;
}

/** The TLS options the form edits, flattened from `TLSConfig`. */
export interface ModelTls {
  caCertSecretRef: string;
  caCertSecretKey: string;
  disableSystemCAs: boolean;
  disableVerify: boolean;
}

export interface ModelDraft {
  namespace: string;
  name: string;
  /** The provider's `type` (its enum), e.g. `"OpenAI"`. */
  provider?: string;
  /** The base model name, without any Ollama `:tag`. */
  model?: string;
  /** Ollama only: the version tag appended to the model as `model:tag`. */
  modelTag: string;
  authType: ModelAuthType;
  /** An inline API key (auth `apiKey`); materialised into a Secret on create. */
  apiKey: string;
  /** An existing Secret's name and key (auth `secret`). */
  apiKeySecret: string;
  apiKeySecretKey: string;
  /** Provider parameter values, keyed by the parameter name the provider reports. */
  params: Record<string, string>;
  /** Headers injected into every request to the provider (advanced). */
  defaultHeaders: HeaderRow[];
  /** TLS options for the connection to the provider (advanced). */
  tls: ModelTls;
}

let headerSeq = 0;
export function newHeaderRow(): HeaderRow {
  headerSeq += 1;
  return { id: `header-${headerSeq}`, key: "", value: "" };
}

function emptyTls(): ModelTls {
  return {
    caCertSecretRef: "",
    caCertSecretKey: "",
    disableSystemCAs: false,
    disableVerify: false,
  };
}

export function emptyModelDraft(): ModelDraft {
  return {
    namespace: DEFAULT_NAMESPACE,
    name: "",
    provider: undefined,
    model: undefined,
    modelTag: "",
    authType: "apiKey",
    apiKey: "",
    apiKeySecret: "",
    apiKeySecretKey: "",
    params: {},
    defaultHeaders: [],
    tls: emptyTls(),
  };
}

/** Provider `type` → the `spec` block its parameters live in. */
const PROVIDER_SPEC_KEY: Record<string, keyof ModelConfigSpec> = {
  OpenAI: "openAI",
  Anthropic: "anthropic",
  AzureOpenAI: "azureOpenAI",
  Ollama: "ollama",
  Gemini: "gemini",
  GeminiVertexAI: "geminiVertexAI",
  AnthropicVertexAI: "anthropicVertexAI",
  Bedrock: "bedrock",
  SAPAICore: "sapAICore",
  Foundry: "foundry",
};

/** Parameters the CRD types as numbers rather than strings. */
const NUMERIC_PARAM_KEYS = new Set([
  "maxTokens",
  "maxOutputTokens",
  "topK",
  "seed",
  "n",
  "timeout",
  "candidateCount",
]);

/**
 * The provider block from the form's flat string values.
 *
 * Empty values are dropped rather than sent blank, and the few numeric fields are
 * parsed so the controller receives the type its CRD declares. A numeric field
 * that will not parse is left out — validation flags it before this runs.
 */
export function coerceParams(
  params: Record<string, string>,
): Record<string, string | number> {
  const out: Record<string, string | number> = {};
  for (const [key, raw] of Object.entries(params)) {
    const value = raw.trim();
    if (!value) continue;
    if (NUMERIC_PARAM_KEYS.has(key)) {
      const parsed = Number(value);
      if (!Number.isNaN(parsed)) out[key] = parsed;
    } else {
      out[key] = value;
    }
  }
  return out;
}

/** Whether a numeric-typed parameter value fails to parse (for validation). */
export function isBadNumber(key: string, raw: string): boolean {
  const value = raw.trim();
  return (
    value !== "" && NUMERIC_PARAM_KEYS.has(key) && Number.isNaN(Number(value))
  );
}

/**
 * The draft an existing configuration was built from.
 *
 * The credential itself never leaves the cluster, so `apiKey` starts empty; the
 * auth mode is inferred from what the spec carries. The provider block is
 * flattened back into the form's string params, and an Ollama `model:tag` is split
 * so the tag edits on its own.
 */
export function modelDraftFrom(config: ModelConfig): ModelDraft {
  const [namespace, name] = splitRef(config.ref);
  const provider = config.spec.provider;

  const rawModel = config.spec.model ?? "";
  let model = rawModel;
  let modelTag = "";
  if (provider === OLLAMA_PROVIDER && rawModel.includes(":")) {
    const at = rawModel.indexOf(":");
    model = rawModel.slice(0, at);
    modelTag = rawModel.slice(at + 1);
  }

  return {
    namespace: namespace || DEFAULT_NAMESPACE,
    name,
    provider,
    model,
    modelTag,
    authType: authTypeFrom(config.spec),
    apiKey: "",
    apiKeySecret: config.spec.apiKeySecret ?? "",
    apiKeySecretKey: config.spec.apiKeySecretKey ?? "",
    params: paramsFromSpec(config.spec),
    defaultHeaders: Object.entries(config.spec.defaultHeaders ?? {}).map(
      ([key, value]) => ({ ...newHeaderRow(), key, value }),
    ),
    tls: {
      caCertSecretRef: config.spec.tls?.caCertSecretRef ?? "",
      caCertSecretKey: config.spec.tls?.caCertSecretKey ?? "",
      disableSystemCAs: config.spec.tls?.disableSystemCAs ?? false,
      disableVerify: config.spec.tls?.disableVerify ?? false,
    },
  };
}

/** Which auth mode an existing spec expresses. */
function authTypeFrom(spec: ModelConfigSpec): ModelAuthType {
  if (spec.provider === OLLAMA_PROVIDER) return "none";
  if (spec.apiKeyPassthrough) return "passthrough";
  if (spec.apiKeySecret) return "secret";
  return "none";
}

/** The stored provider block, flattened to the form's string values. */
function paramsFromSpec(spec: ModelConfigSpec): Record<string, string> {
  const key = spec.provider ? PROVIDER_SPEC_KEY[spec.provider] : undefined;
  const block = key
    ? (spec[key] as Record<string, unknown> | undefined)
    : undefined;
  if (!block) return {};

  const out: Record<string, string> = {};
  for (const [param, value] of Object.entries(block)) {
    if (value === undefined || value === null) continue;
    // Maps and arrays (e.g. Ollama `options`, Vertex `stopSequences`) have no plain
    // text form, so they are left for the reader to set rather than stringified.
    if (typeof value === "object") continue;
    out[param] = String(value);
  }
  return out;
}

/** The request body for a draft. Empty optional fields are omitted, not sent blank. */
export function buildModelPayload(draft: ModelDraft): CreateModelConfigRequest {
  if (!draft.provider || !draft.model) {
    throw new Error("A provider and a model are required.");
  }

  const isOllama = draft.provider === OLLAMA_PROVIDER;

  // Ollama carries its version as `model:tag`; the default tag is left implicit.
  const tag = draft.modelTag.trim();
  const model =
    isOllama && tag && tag !== OLLAMA_DEFAULT_TAG
      ? `${draft.model}:${tag}`
      : draft.model;

  const spec: ModelConfigSpec = { model, provider: draft.provider };

  const blockKey = PROVIDER_SPEC_KEY[draft.provider];
  const block = coerceParams(draft.params);
  if (blockKey && Object.keys(block).length > 0) {
    // The block shape is provider-specific and built from dynamic keys, so it is
    // assigned through an index rather than named — the values are validated first.
    (spec as unknown as Record<string, unknown>)[blockKey] = block;
  }

  // Authentication. Ollama takes none; otherwise the mode decides what is sent.
  let inlineKey = "";
  if (!isOllama) {
    if (draft.authType === "apiKey") {
      inlineKey = draft.apiKey.trim();
    } else if (draft.authType === "secret") {
      if (draft.apiKeySecret.trim()) {
        spec.apiKeySecret = draft.apiKeySecret.trim();
        if (draft.apiKeySecretKey.trim()) {
          spec.apiKeySecretKey = draft.apiKeySecretKey.trim();
        }
      }
    } else if (draft.authType === "passthrough") {
      spec.apiKeyPassthrough = true;
    }
  }

  const headers = headersToRecord(draft.defaultHeaders);
  if (Object.keys(headers).length > 0) spec.defaultHeaders = headers;

  const tls = buildTls(draft.tls);
  if (tls) spec.tls = tls;

  return {
    ref: toRef(draft.namespace.trim(), draft.name.trim()),
    spec,
    // The backend materialises this into a Secret and fills in the spec's ref.
    ...(inlineKey ? { apiKey: inlineKey } : {}),
  };
}

/** Non-empty header rows, last write winning on a duplicate name. */
function headersToRecord(rows: HeaderRow[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (key) out[key] = row.value;
  }
  return out;
}

/** A `TLSConfig` with only the options the reader set, or `undefined` if none. */
function buildTls(tls: ModelTls): TLSConfig | undefined {
  const out: TLSConfig = {};
  if (tls.caCertSecretRef.trim()) {
    out.caCertSecretRef = tls.caCertSecretRef.trim();
    if (tls.caCertSecretKey.trim())
      out.caCertSecretKey = tls.caCertSecretKey.trim();
  }
  if (tls.disableSystemCAs) out.disableSystemCAs = true;
  if (tls.disableVerify) out.disableVerify = true;
  return Object.keys(out).length > 0 ? out : undefined;
}

/**
 * What the form will not let through, in the words a reader needs.
 *
 * Returned as a list rather than a boolean so the form can show every problem at
 * once. Being told about one missing field, fixing it, and being told about the
 * next is a worse experience than being told about all of them.
 */
export function modelDraftIssues(
  draft: ModelDraft,
  opts: {
    isValidName: (value: string) => boolean;
    nameHint: string;
    /** The selected provider's required parameter names. */
    requiredParamKeys: string[];
    /** Editing an existing configuration — an inline key is then optional. */
    isEdit: boolean;
  },
): string[] {
  const issues: string[] = [];
  const name = draft.name.trim();

  if (!name) issues.push("A name is required.");
  else if (!opts.isValidName(name)) issues.push(opts.nameHint);
  if (!draft.namespace.trim()) issues.push("A namespace is required.");
  if (!draft.provider) issues.push("Choose a provider.");
  if (!draft.model) issues.push("Choose a model.");

  for (const key of opts.requiredParamKeys) {
    if (!(draft.params[key] ?? "").trim()) {
      issues.push(`${key} is required.`);
    }
  }

  for (const [key, value] of Object.entries(draft.params)) {
    if (isBadNumber(key, value)) issues.push(`${key} must be a number.`);
  }

  const isOllama = draft.provider === OLLAMA_PROVIDER;
  if (!isOllama && draft.provider) {
    // A new inline key is needed when creating; an edit keeps its existing one.
    if (draft.authType === "apiKey" && !opts.isEdit && !draft.apiKey.trim()) {
      issues.push(
        "An API key is required, or choose another authentication type.",
      );
    }
    // A secret key without a secret names nothing.
    if (
      draft.authType === "secret" &&
      draft.apiKeySecretKey.trim() &&
      !draft.apiKeySecret.trim()
    ) {
      issues.push("A secret key needs the secret that holds it.");
    }
  }

  return issues;
}

function splitRef(ref: string): [string, string] {
  const at = ref.indexOf("/");
  return at === -1 ? ["", ref] : [ref.slice(0, at), ref.slice(at + 1)];
}
