import { useMemo, useState } from "react";
import {
  Alert,
  AutoComplete,
  Button,
  Checkbox,
  Col,
  Collapse,
  Form,
  Input,
  InputNumber,
  Radio,
  Row,
  Select,
  Space,
  Tooltip,
  Typography,
} from "antd";
import { keyframes, useTheme } from "@emotion/react";
import { AlertTriangle, ExternalLink, RefreshCw } from "lucide-react";
import { SubmitError } from "@/components/common/SubmitError";
import { FormActions } from "@/components/form/FormActions";
import { FormField } from "@/components/form/FormField";
import { FieldRow } from "@/components/form/FieldRow";
import { NameInput } from "@/components/form/NameInput";
import { FormRow, FormRowList } from "@/components/form/FormRowList";
import { ListInput } from "@/components/form/ListInput";
import { NamespaceSelect } from "@/components/form/NamespaceSelect";
import { UnsavedChangesModal } from "@/components/form/UnsavedChangesModal";
import { useSubmitValidation } from "@/components/form/useSubmitValidation";
import { useUnsavedChangesGuard } from "@/components/form/useUnsavedChangesGuard";
import {
  RESOURCE_NAME_HINT,
  isValidResourceName,
  slugifyResourceName,
} from "@/components/common/resourceName";
import { paths } from "@/router/routes";
import {
  useProviderModels,
  useProviders,
  type CreateModelConfigRequest,
} from "@/api";
import type { OpenAIAPIFormat, OpenAIReasoningEffort } from "@/api/domain/models";
import {
  authTypeForProvider,
  supportsActorJWT,
  buildModelPayload,
  emptyModelDraft,
  modelDraftIssues,
  newHeaderRow,
  paramLabel,
  type ModelAuthType,
  type ModelDraft,
  type ModelTls,
} from "./modelDraft";
import {
  OLLAMA_DEFAULT_TAG,
  OLLAMA_PROVIDER,
  PROVIDER_INFO,
  providerDisplayName,
  supportsPassthrough,
} from "./providerInfo";
import { ProviderIconWithLabel } from "./ProviderIcon";

const { Text } = Typography;

/**
 * Provider params whose values are a fixed CRD enum (`OpenAIAPIFormat`,
 * `OpenAIReasoningEffort`), rendered as a select with help rather than free text.
 * `ListSupportedModelProviders` reports param names only, not their allowed values,
 * so the values are kept here, typed against `@/api/domain/models`.
 */
const ENUM_PARAMS: Record<string, { values: string[]; help: string }> = {
  apiFormat: {
    values: ["chatCompletions", "responses"] satisfies OpenAIAPIFormat[],
    help: "chatCompletions (default) posts to /v1/chat/completions; responses posts to /v1/responses. Codex requires responses.",
  },
  reasoningEffort: {
    values: ["none", "minimal", "low", "medium", "high", "xhigh"] satisfies OpenAIReasoningEffort[],
    help: "How many reasoning tokens the model spends before answering; none disables reasoning. Supported values vary by model.",
  },
};

interface ModelFormProps {
  onSubmit: (payload: CreateModelConfigRequest) => Promise<void>;
  /** Called after a successful save, once the guard has stood down; usually navigates. */
  onSaved: () => void;
  initial?: ModelDraft;
  outcome?: "created" | "saved";
  submitLabel?: string;
  identityLocked?: boolean;
  additionalProviderOptions?: Array<{ value: string; label: string }>;
  onProviderChange?: (provider: string) => void;
  allowProviderSelectionWhenIdentityLocked?: boolean;
}

/** The draft by value, minus the header rows' bookkeeping ids, for dirty checks. */
function draftContents(draft: ModelDraft): string {
  return JSON.stringify({
    ...draft,
    defaultHeaders: draft.defaultHeaders.map((row) => [row.key, row.value]),
  });
}

const spin = keyframes({ from: { transform: "rotate(0deg)" }, to: { transform: "rotate(360deg)" } });

/** A small "open the provider's docs" link, when there is one to open. */
function DocsLink({ href, title }: { href?: string; title: string }) {
  const theme = useTheme();
  if (!href) return null;
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      title={title}
      css={{
        display: "inline-flex",
        alignItems: "center",
        gap: 4,
        color: theme.color.textMuted,
        fontSize: 12,
      }}
    >
      <ExternalLink size={12} aria-hidden />
    </a>
  );
}

/**
 * The fields of a model configuration, for creating one or changing one.
 *
 * Shared by the create and edit pages: the form owns the fields and whether they
 * are allowed through; the page owns the request and what happens afterwards.
 *
 * Beyond the identity and the provider/model pair, three things a configuration
 * actually needs are here: its credential (typed inline and materialised into a
 * Secret, or opted out of), and the provider's own parameters — required ones like
 * an Azure endpoint or a Bedrock region, and the optional tuning knobs each
 * provider exposes.
 */
export function ModelForm({
  onSubmit,
  onSaved,
  initial,
  outcome = "created",
  submitLabel = "Create",
  identityLocked = false,
  additionalProviderOptions = [],
  onProviderChange,
  allowProviderSelectionWhenIdentityLocked = false,
}: ModelFormProps) {
  const theme = useTheme();
  const isEdit = initial !== undefined;

  const providers = useProviders();
  const providerModels = useProviderModels();

  const [draft, setDraft] = useState<ModelDraft>(initial ?? emptyModelDraft());
  const [baseline] = useState(() => draftContents(draft));
  const [optionalOpen, setOptionalOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [refreshSpins, setRefreshSpins] = useState(0);
  const [failure, setFailure] = useState<unknown>();

  const set = <K extends keyof ModelDraft>(key: K, value: ModelDraft[K]) => {
    setDraft((current) => ({ ...current, [key]: value }));
    setFailure(undefined);
  };

  const setParam = (key: string, value: string) => {
    setDraft((current) => ({
      ...current,
      params: { ...current.params, [key]: value },
    }));
    setFailure(undefined);
  };

  const setTls = <K extends keyof ModelTls>(key: K, value: ModelTls[K]) => {
    setDraft((current) => ({
      ...current,
      tls: { ...current.tls, [key]: value },
    }));
    setFailure(undefined);
  };

  const addHeader = () =>
    setDraft((current) => ({
      ...current,
      defaultHeaders: [...current.defaultHeaders, newHeaderRow()],
    }));
  const setHeader = (id: string, field: "key" | "value", value: string) =>
    setDraft((current) => ({
      ...current,
      defaultHeaders: current.defaultHeaders.map((row) =>
        row.id === id ? { ...row, [field]: value } : row,
      ),
    }));
  const removeHeader = (id: string) =>
    setDraft((current) => ({
      ...current,
      defaultHeaders: current.defaultHeaders.filter((row) => row.id !== id),
    }));

  const providerList = useMemo(
    () =>
      [...(providers.data ?? [])].sort((a, b) =>
        providerDisplayName(a.type).localeCompare(providerDisplayName(b.type)),
      ),
    [providers.data],
  );
  const providerSelectOptions = useMemo(() => {
    // One option per provider *type*, because that is what the draft stores.
    const seen = new Set<string>();
    const keepFirst = (item: { value: string }) => {
      if (seen.has(item.value)) return false;
      seen.add(item.value);
      return true;
    };

    const base = providerList
      .map((item) => ({
        value: item.type,
        label: providerDisplayName(item.type),
      }))
      .filter(keepFirst);

    return [...base, ...additionalProviderOptions.filter(keepFirst)];
  }, [providerList, additionalProviderOptions]);
  const selectedProvider = useMemo(
    () => providerList.find((item) => item.type === draft.provider),
    [providerList, draft.provider],
  );
  const requiredKeys = useMemo(
    () => selectedProvider?.requiredParams ?? [],
    [selectedProvider],
  );
  const optionalKeys = useMemo(
    () => selectedProvider?.optionalParams ?? [],
    [selectedProvider],
  );

  const modelsForProvider = useMemo(
    () => (draft.provider ? (providerModels.data?.[draft.provider] ?? []) : []),
    [draft.provider, providerModels.data],
  );
  const selectedModel = modelsForProvider.find((m) => m.name === draft.model);
  const refreshing = providers.isLoading || providerModels.isLoading;
  const isOllama = draft.provider === OLLAMA_PROVIDER;
  const info = draft.provider ? PROVIDER_INFO[draft.provider] : undefined;

  // Passthrough only where the CRD allows it; `changeProvider` resets the mode
  // when switching to a provider that forbids it, so this never strands an
  // invalid selection. Ollama gets "Existing secret" alone: it needs no
  // credential locally, but one is what authenticates an Ollama Cloud model, and
  // the chart's OLLAMA_API_KEY already lives in a Secret.
  //
  // "No credential" has to be offered alongside it: a new draft starts on
  // "apiKey" (see newDraft), so with only "Existing secret" here an Ollama draft
  // opened with nothing selected and no way back once that button was clicked.
  const authOptions: { label: string; value: ModelAuthType }[] = isOllama
    ? [
        { label: "Existing secret", value: "secret" },
        { label: "No credential", value: "none" },
      ]
    : [
        { label: "API key", value: "apiKey" },
        { label: "Existing secret", value: "secret" },
        ...(supportsPassthrough(draft.provider)
          ? [{ label: "Passthrough", value: "passthrough" as const }]
          : []),
        { label: "No credential", value: "none" },
      ];

  if (supportsActorJWT(draft.provider)) authOptions.push({ label: "Actor JWT", value: "actorJwt" });

  // An empty name falls back to the model's own, which is almost always what a new configuration is called.
  const modelName = slugifyResourceName(draft.model ?? "");
  const effectiveName = draft.name.trim() || modelName;
  const effective = useMemo<ModelDraft>(
    () => ({ ...draft, name: effectiveName }),
    [draft, effectiveName],
  );

  // The name is optional: with no model to fall back to, the model's own issue says what is missing.
  const issues = useMemo(
    () =>
      modelDraftIssues(effective, {
        isValidName: isValidResourceName,
        nameHint: RESOURCE_NAME_HINT,
        requiredParamKeys: requiredKeys,
        isEdit,
      }).filter((issue) => issue.field !== "name" || effectiveName !== ""),
    [effective, effectiveName, requiredKeys, isEdit],
  );
  const validation = useSubmitValidation(issues);
  const fieldFor = validation.field;

  const isDirty = draftContents(effective) !== baseline;
  const { blocker, release } = useUnsavedChangesGuard(isDirty);

  // The Secret name/key fields, shared by every provider that offers "Existing secret".
  const secretFields = (
    <>
      <FormField
        label="Existing secret"
        required
        {...fieldFor("apiKeySecret")}
        help="The Secret to read the API key from."
      >
        <Input
          data-testid="model-api-key-secret"
          placeholder={isOllama ? "kagent-ollama-cloud" : "kagent-openai"}
          value={draft.apiKeySecret}
          onChange={(event) => set("apiKeySecret", event.target.value)}
        />
      </FormField>
      <FormField
        label="Key in secret"
        required={false}
        help={
          isOllama
            ? "Which entry in the Secret to read. Defaults to OLLAMA_API_KEY."
            : "Which entry in the Secret to read. Defaults to the provider's usual key."
        }
      >
        <Input
          data-testid="model-api-key-secret-key"
          placeholder={isOllama ? "OLLAMA_API_KEY" : "OPENAI_API_KEY"}
          value={draft.apiKeySecretKey}
          onChange={(event) => set("apiKeySecretKey", event.target.value)}
        />
      </FormField>
    </>
  );

  const changeProvider = (type: string) => {
    // The previous model, tag and parameters belong to the old provider, so they
    // are cleared rather than carried onto one that does not offer them.
    // The auth mode is reset whenever the new provider does not offer it: a
    // passthrough selection for a provider that forbids it, and "apiKey" for
    // Ollama, which takes an existing Secret only. Leaving it would render a
    // radio group with nothing selected.
    setDraft((current) => ({
      ...current,
      provider: type,
      model: undefined,
      modelTag: "",
      params: {},
      authType: authTypeForProvider(type, current.authType),
    }));
    setFailure(undefined);
    onProviderChange?.(type);
  };

  const optionalError = optionalKeys.some((key) => fieldFor(`param:${key}`).error !== undefined);

  const submit = async () => {
    setFailure(undefined);
    if (!validation.attempt()) return;

    setSaving(true);
    try {
      await onSubmit(buildModelPayload(effective));
      release();
      onSaved();
    } catch (error) {
      setFailure(error);
    } finally {
      setSaving(false);
    }
  };

  const paramField = (key: string, required: boolean) => {
    const value = draft.params[key] ?? "";
    const label = paramLabel(key);
    return (
      <FormField
        key={key}
        label={label}
        required={required}
        {...fieldFor(`param:${key}`)}
        help={ENUM_PARAMS[key]?.help}
      >
        {ENUM_PARAMS[key] ? (
          <Select
            data-testid={`model-param-${key}`}
            placeholder={`Choose ${label.toLowerCase()}`}
            allowClear={!required}
            value={value || undefined}
            options={ENUM_PARAMS[key].values.map((option) => ({ label: option, value: option }))}
            onChange={(next?: string) => setParam(key, next ?? "")}
          />
        ) : (
          <Input
            data-testid={`model-param-${key}`}
            placeholder={`Enter ${label.toLowerCase()}`}
            value={value}
            onChange={(event) => setParam(key, event.target.value)}
          />
        )}
      </FormField>
    );
  };

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex", maxWidth: 720 }}>
      {providers.error || providerModels.error ? (
        <Alert
          type="warning"
          showIcon
          data-testid="model-providers-error"
          title="Could not load the provider list"
          description="Providers and models cannot be chosen until it loads."
          action={
            <Button
              size="small"
              onClick={() => {
                void providers.refresh();
                void providerModels.refresh();
              }}
            >
              Try again
            </Button>
          }
        />
      ) : null}

      <Form layout="vertical" component="div">
        <Row gutter={16}>
          <Col xs={24} md={12}>
            <FormField
              label={
                <Space size={6}>
                  Provider
                  <DocsLink
                    href={info?.modelDocsLink}
                    title={`${providerDisplayName(draft.provider ?? "")} model docs`}
                  />
                </Space>
              }
              required
              {...fieldFor("provider")}
              help="Choose where the model runs."
            >
              <div css={{ display: "flex", gap: theme.space(2) }}>
                <Select
                  data-testid="model-provider"
                  placeholder="Select a provider"
                  value={draft.provider}
                  loading={providers.isLoading}
                  disabled={identityLocked && !allowProviderSelectionWhenIdentityLocked}
                  css={{ flex: 1, minWidth: 0 }}
                  options={providerSelectOptions}
                  showSearch={{ optionFilterProp: "label" }}
                  onChange={changeProvider}
                  optionRender={(o) => (
                    <ProviderIconWithLabel type={String(o.value ?? "")} label={o.label} />
                  )}
                  labelRender={(l) => (
                    <ProviderIconWithLabel type={String(l.value ?? "")} label={l.label} />
                  )}
                />
                {/* A refresh, so a provider configured after this page loaded is reachable without a reload. */}
                <Tooltip title="Refresh models">
                  <Button
                    data-testid="model-fetch"
                    aria-label="Refresh models"
                    aria-busy={refreshing}
                    css={{ flexShrink: 0 }}
                    icon={
                      // Keyed by click count, so each click replays the turn; it keeps turning while loading.
                      <RefreshCw
                        key={refreshSpins}
                        size={16}
                        aria-hidden
                        css={{
                          animation: refreshing
                            ? `${spin} 800ms linear infinite`
                            : refreshSpins > 0 ? `${spin} 500ms ease-out` : undefined,
                          "@media (prefers-reduced-motion: reduce)": { animation: "none" },
                        }}
                      />
                    }
                    onClick={() => {
                      setRefreshSpins((count) => count + 1);
                      void providers.refresh();
                      void providerModels.refresh();
                    }}
                  />
                </Tooltip>
              </div>
            </FormField>
          </Col>

          <Col xs={24} md={12}>
            <FormField
              label="Provider model"
              required
              {...fieldFor("model")}
              help="Pick from the list, or type a model name."
            >
              <AutoComplete
                data-testid="model-model"
                placeholder={draft.provider ? "Select or type a model" : "Pick a provider first"}
                value={draft.model}
                disabled={
                  !draft.provider || (identityLocked && !allowProviderSelectionWhenIdentityLocked)
                }
                options={modelsForProvider.map((item) => ({ value: item.name }))}
                filterOption={(input, option) =>
                  (option?.value ?? "").toLowerCase().includes(input.toLowerCase())
                }
                onChange={(next) => set("model", next)}
              />
              {selectedModel && selectedModel.function_calling === false ? (
                <Text
                  data-testid="model-fc-warning"
                  css={{
                    display: "inline-flex",
                    alignItems: "center",
                    gap: 6,
                    marginTop: 6,
                    color: theme.color.warning,
                    fontSize: 12,
                  }}
                >
                  <AlertTriangle size={14} aria-hidden />
                  This model does not support function calling, so agents cannot call tools with it.
                </Text>
              ) : null}
            </FormField>
          </Col>
        </Row>

        {isOllama ? (
          <FormField
            label="Ollama tag"
            required={false}
            help={`Which version of the model to pull. Defaults to “${OLLAMA_DEFAULT_TAG}”.`}
          >
            <Input
              data-testid="model-ollama-tag"
              placeholder={OLLAMA_DEFAULT_TAG}
              value={draft.modelTag}
              onChange={(event) => set("modelTag", event.target.value)}
            />
          </FormField>
        ) : null}

        <FieldRow>
          <FormField
            label="Display name"
            required={false}
            {...fieldFor("name")}
            help={identityLocked ? "You can't change the name." : "Leave empty to use the model's name."}
          >
            <NameInput
              dots={false}
              data-testid="model-name"
              placeholder={modelName || "gpt-4-1"}
              value={draft.name}
              disabled={identityLocked}
              onChange={(value) => set("name", value)}
              css={{ fontFamily: theme.font.mono }}
            />
          </FormField>

          <FormField
            label="Namespace"
            required
            {...fieldFor("namespace")}
            help={identityLocked ? "Fixed once the model exists." : "Pick the namespace to create the model in."}
          >
            <NamespaceSelect
              data-testid="model-namespace"
              value={draft.namespace}
              disabled={identityLocked}
              onChange={(namespace) => set("namespace", namespace)}
            />
          </FormField>
        </FieldRow>

        {/* Ollama needs no key for a local model, but an Ollama Cloud model authenticates
            with a key, hence the Secret option. Other providers pick a mode: a key typed
            here, an existing Secret, the caller's own credential, or none. */}
        {isOllama ? (
          <>
            <FormField
              label="Authentication"
              required
              help="A local Ollama model needs no API key. To run a cloud model against api.ollama.com, name a Secret holding OLLAMA_API_KEY — an explicit host is used instead when one is set."
            >
              <Radio.Group
                data-testid="model-auth-type"
                value={draft.authType}
                onChange={(event) => set("authType", event.target.value as ModelAuthType)}
                options={authOptions}
                optionType="button"
              />
            </FormField>
            {draft.authType === "secret" ? secretFields : null}
          </>
        ) : (
          <>
            <FormField label="Authentication" required {...fieldFor("authType")}>
              <Radio.Group
                data-testid="model-auth-type"
                value={draft.authType}
                onChange={(event) => set("authType", event.target.value as ModelAuthType)}
                options={authOptions}
                optionType="button"
              />
            </FormField>

            {draft.authType === "apiKey" ? (
              <FormField
                label={
                  <Space size={6}>
                    {isEdit ? "API key (leave blank to keep existing)" : "API key"}
                    <DocsLink
                      href={info?.apiKeyLink ?? undefined}
                      title={`Get your ${providerDisplayName(draft.provider ?? "")} API key`}
                    />
                  </Space>
                }
                // Required to create and not to edit: a blank edit keeps the stored key.
                required={!isEdit}
                {...fieldFor("apiKey")}
                help="Typed once and stored in a Kubernetes Secret the controller creates and owns."
              >
                <Input.Password
                  data-testid="model-api-key"
                  placeholder={isEdit ? "Enter a new key to replace it" : "Enter API key…"}
                  value={draft.apiKey}
                  autoComplete="new-password"
                  onChange={(event) => set("apiKey", event.target.value)}
                />
              </FormField>
            ) : null}

            {draft.authType === "secret" ? secretFields : null}

            {draft.authType === "passthrough" ? (
              <Alert
                type="info"
                showIcon
                data-testid="model-passthrough-note"
                title="The credential on each incoming request is forwarded to the provider; nothing is stored in the cluster."
              />
            ) : null}
          </>
        )}

        {draft.authType === "actorJwt" ? (
          <>
            <FormField
              label="Audiences"
              required
              {...fieldFor("actorAudiences")}
              help="Services that accept this Actor token. Commas or spaces split them."
              htmlFor="model-actor-audiences"
            >
              <ListInput
                id="model-actor-audiences"
                data-testid="model-actor-audiences"
                split="commasAndSpaces"
                unique
                value={draft.actorAudiences}
                onChange={(value) => set("actorAudiences", value)}
              />
            </FormField>
            <FormField
              label="Token lifetime (seconds)"
              required
              {...fieldFor("actorExpirationSeconds")}
            >
              <InputNumber
                min={300}
                max={3600}
                precision={0}
                value={draft.actorExpirationSeconds}
                onChange={(value) => set("actorExpirationSeconds", value ?? 900)}
              />
            </FormField>
          </>
        ) : null}

        {/* Required provider parameters (an Azure endpoint, a Bedrock region) gate
            submission; the optional ones stay collapsed so the common case is short. */}
        {draft.provider && requiredKeys.length > 0 ? (
          <div data-testid="model-required-params">
            <Text css={{ fontSize: 13, fontWeight: 500 }}>Required parameters</Text>
            <div css={{ marginTop: theme.space(2) }}>
              {requiredKeys.map((key) => paramField(key, true))}
            </div>
          </div>
        ) : null}

        {draft.provider && optionalKeys.length > 0 ? (
          <Collapse
            ghost
            data-testid="model-optional-params"
            // Stays open while a parameter inside shows an error, so the error stays in sight.
            activeKey={optionalOpen || optionalError ? ["optional"] : []}
            onChange={(keys) => setOptionalOpen(keys.length > 0 || optionalError)}
            items={[
              {
                key: "optional",
                label: `Optional parameters (${optionalKeys.length})`,
                children: optionalKeys.map((key) => paramField(key, false)),
              },
            ]}
          />
        ) : null}

        {/* Headers and TLS apply to any provider, so they sit apart from the
            provider-specific parameters and stay collapsed until wanted. */}
        {draft.provider ? (
          <Collapse
            ghost
            data-testid="model-advanced"
            items={[
              {
                key: "advanced",
                label: "Advanced (headers, TLS)",
                children: (
                  <>
                    <FormField
                      label="Default headers"
                      required={false}
                      help="Sent on every request to the provider — for API versioning, tracing, or provider-specific needs."
                    >
                      <FormRowList
                        data-testid="model-headers"
                        addLabel="Add header"
                        onAdd={addHeader}
                      >
                        {draft.defaultHeaders.map((row, index) => (
                          <FormRow
                            key={row.id}
                            title={`Header ${index + 1}`}
                            onRemove={() => removeHeader(row.id)}
                            removeLabel={`Remove header ${index + 1}`}
                          >
                            <Space size={8} css={{ display: "flex" }}>
                              <Input
                                data-testid="model-header-name"
                                aria-label={`Header ${index + 1} name`}
                                placeholder="X-Environment"
                                value={row.key}
                                onChange={(event) => setHeader(row.id, "key", event.target.value)}
                              />
                              <Input
                                data-testid="model-header-value"
                                aria-label={`Header ${index + 1} value`}
                                placeholder="development"
                                value={row.value}
                                onChange={(event) => setHeader(row.id, "value", event.target.value)}
                              />
                            </Space>
                          </FormRow>
                        ))}
                      </FormRowList>
                    </FormField>

                    <Form.Item>
                      <Checkbox
                        data-testid="model-tls-disable-verify"
                        checked={draft.tls.disableVerify}
                        onChange={(event) => setTls("disableVerify", event.target.checked)}
                      >
                        Skip TLS verification
                      </Checkbox>
                    </Form.Item>
                    {draft.tls.disableVerify ? (
                      <Alert
                        type="warning"
                        showIcon
                        data-testid="model-tls-insecure-warning"
                        title="TLS verification is disabled — use only in non-production environments."
                      />
                    ) : null}
                  </>
                ),
              },
            ]}
          />
        ) : null}
      </Form>

      <FormActions
        submitLabel={submitLabel}
        onSubmit={() => void submit()}
        submitting={saving}
        cancel={{ to: paths.models }}
        testIdPrefix="model"
        blocked={!validation.valid}
        error={
          failure !== undefined ? (
            <SubmitError
              error={failure}
              what="model configuration"
              outcome={outcome}
              onRetry={() => void submit()}
              data-testid="model-submit-error"
            />
          ) : null
        }
      />

      <UnsavedChangesModal blocker={blocker} testIdPrefix="model" />
    </Space>
  );
}
