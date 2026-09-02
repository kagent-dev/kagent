import { useMemo, useState } from "react";
import {
  Alert,
  AutoComplete,
  Button,
  Col,
  Collapse,
  Form,
  Input,
  Radio,
  Row,
  Select,
  Space,
  Typography,
} from "antd";
import { useTheme } from "@emotion/react";
import { Link } from "react-router-dom";
import {
  AlertTriangle,
  ExternalLink,
  Plus,
  RefreshCw,
  Trash2,
} from "lucide-react";
import { SubmitError } from "@/components/common/SubmitError";
import {
  RESOURCE_NAME_HINT,
  isValidResourceName,
  slugifyResourceName,
} from "@/components/common/resourceName";
import { paths } from "@/router/routes";
import {
  useNamespaces,
  useProviderModels,
  useProviders,
  type CreateModelConfigRequest,
} from "@/api";
import {
  buildModelPayload,
  emptyModelDraft,
  isBadNumber,
  modelDraftIssues,
  newHeaderRow,
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

interface ModelFormProps {
  onSubmit: (payload: CreateModelConfigRequest) => Promise<void>;
  initial?: ModelDraft;
  outcome?: "created" | "saved";
  submitLabel?: string;
  identityLocked?: boolean;
  additionalProviderOptions?: Array<{ value: string; label: string }>;
  onProviderChange?: (provider: string) => void;
  allowProviderSelectionWhenIdentityLocked?: boolean;
}

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
  initial,
  outcome = "created",
  submitLabel = "Create model",
  identityLocked = false,
  additionalProviderOptions = [],
  onProviderChange,
  allowProviderSelectionWhenIdentityLocked = false,
}: ModelFormProps) {
  const theme = useTheme();
  const isEdit = initial !== undefined;

  const providers = useProviders();
  const providerModels = useProviderModels();
  const namespaces = useNamespaces();

  const [draft, setDraft] = useState<ModelDraft>(initial ?? emptyModelDraft());
  // Whether the name has been typed into. True when editing: an existing name is
  // its own and must not be rewritten by the model picker.
  const [nameTouched, setNameTouched] = useState(isEdit);
  const [submitted, setSubmitted] = useState(false);
  const [saving, setSaving] = useState(false);
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
    const base = providerList.map((item) => ({
      value: item.type,
      label: providerDisplayName(item.type),
    }));

    const seen = new Set(base.map((item) => item.value));
    const extras = additionalProviderOptions.filter((item) => {
      if (seen.has(item.value)) return false;
      seen.add(item.value);
      return true;
    });

    return [...base, ...extras];
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
  const isOllama = draft.provider === OLLAMA_PROVIDER;
  const info = draft.provider ? PROVIDER_INFO[draft.provider] : undefined;

  // Passthrough only where the CRD allows it; `changeProvider` resets the mode
  // when switching to a provider that forbids it, so this never strands an
  // invalid selection.
  const authOptions: { label: string; value: ModelAuthType }[] = [
    { label: "API key", value: "apiKey" },
    { label: "Existing secret", value: "secret" },
    ...(supportsPassthrough(draft.provider)
      ? [{ label: "Passthrough", value: "passthrough" as const }]
      : []),
    { label: "No credential", value: "none" },
  ];

  // The name follows the chosen model until the field is edited, since the model's
  // own name is almost always what a new configuration should be called.
  const effectiveName = nameTouched
    ? draft.name
    : slugifyResourceName(draft.model ?? "");
  const effective = useMemo<ModelDraft>(
    () => ({ ...draft, name: effectiveName }),
    [draft, effectiveName],
  );

  const issues = useMemo(
    () =>
      modelDraftIssues(effective, {
        isValidName: isValidResourceName,
        nameHint: RESOURCE_NAME_HINT,
        requiredParamKeys: requiredKeys,
        isEdit,
      }),
    [effective, requiredKeys, isEdit],
  );

  const changeProvider = (type: string) => {
    // The previous model, tag and parameters belong to the old provider, so they
    // are cleared rather than carried onto one that does not offer them. A
    // passthrough selection is reset too when the new provider forbids it.
    setDraft((current) => ({
      ...current,
      provider: type,
      model: undefined,
      modelTag: "",
      params: {},
      authType:
        current.authType === "passthrough" && !supportsPassthrough(type)
          ? "apiKey"
          : current.authType,
    }));
    setFailure(undefined);
    onProviderChange?.(type);
  };

  const submit = async () => {
    setSubmitted(true);
    setFailure(undefined);
    if (issues.length > 0) return;

    setSaving(true);
    try {
      await onSubmit(buildModelPayload(effective));
    } catch (error) {
      setFailure(error);
    } finally {
      setSaving(false);
    }
  };

  const paramField = (key: string, required: boolean) => {
    const value = draft.params[key] ?? "";
    const missing = required && submitted && !value.trim();
    const badNumber = submitted && isBadNumber(key, value);
    return (
      <Form.Item
        key={key}
        label={key}
        required={required}
        validateStatus={missing || badNumber ? "error" : undefined}
        help={
          missing
            ? `${key} is required.`
            : badNumber
              ? `${key} must be a number.`
              : undefined
        }
      >
        <Input
          data-testid={`model-param-${key}`}
          placeholder={required ? `Enter ${key}` : `(optional) ${key}`}
          value={value}
          onChange={(event) => setParam(key, event.target.value)}
        />
      </Form.Item>
    );
  };

  return (
    <Space
      orientation="vertical"
      size="middle"
      css={{ display: "flex", maxWidth: 720 }}
    >
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
            <Form.Item
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
              validateStatus={
                submitted && !draft.provider ? "error" : undefined
              }
              help={
                submitted && !draft.provider ? "Choose a provider." : undefined
              }
            >
              <Space.Compact css={{ display: "flex" }}>
                <Select
                  data-testid="model-provider"
                  placeholder="Select a provider"
                  value={draft.provider}
                  loading={providers.isLoading}
                  disabled={
                    identityLocked && !allowProviderSelectionWhenIdentityLocked
                  }
                  css={{ flex: 1 }}
                  options={providerSelectOptions}
                  onChange={changeProvider}
                  optionRender={(o) => (
                    <ProviderIconWithLabel
                      type={String(o.value ?? "")}
                      label={o.label}
                    />
                  )}
                  labelRender={(l) => (
                    <ProviderIconWithLabel
                      type={String(l.value ?? "")}
                      label={l.label}
                    />
                  )}
                />
                {/* A refresh, because a provider configured after this page loaded — or
                    its newly pulled models — should be reachable without a reload. */}
                <Button
                  data-testid="model-fetch"
                  icon={<RefreshCw size={14} aria-hidden />}
                  loading={providers.isLoading || providerModels.isLoading}
                  onClick={() => {
                    void providers.refresh();
                    void providerModels.refresh();
                  }}
                >
                  Refresh models
                </Button>
              </Space.Compact>
            </Form.Item>
          </Col>

          <Col xs={24} md={12}>
            <Form.Item
              label="Model"
              required
              validateStatus={submitted && !draft.model ? "error" : undefined}
              help={
                submitted && !draft.model
                  ? "Choose a model."
                  : draft.provider
                    ? "Pick model from the list, or type it if it isn't listed."
                    : "Pick a provider first."
              }
            >
              <AutoComplete
                data-testid="model-model"
                placeholder={
                  draft.provider
                    ? "Select or type a model"
                    : "Pick a provider first"
                }
                value={draft.model}
                disabled={
                  !draft.provider ||
                  (identityLocked && !allowProviderSelectionWhenIdentityLocked)
                }
                options={modelsForProvider.map((item) => ({
                  value: item.name,
                }))}
                filterOption={(input, option) =>
                  (option?.value ?? "")
                    .toLowerCase()
                    .includes(input.toLowerCase())
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
                  This model does not support function calling, so agents cannot
                  call tools with it.
                </Text>
              ) : null}
            </Form.Item>
          </Col>
        </Row>

        {isOllama ? (
          <Form.Item
            label="Model tag"
            help={`The Ollama version tag. Defaults to “${OLLAMA_DEFAULT_TAG}”.`}
          >
            <Input
              data-testid="model-ollama-tag"
              placeholder={OLLAMA_DEFAULT_TAG}
              value={draft.modelTag}
              onChange={(event) => set("modelTag", event.target.value)}
            />
          </Form.Item>
        ) : null}

        <Form.Item
          label="Name"
          required
          validateStatus={
            submitted &&
            (!effectiveName.trim() ||
              !isValidResourceName(effectiveName.trim()))
              ? "error"
              : undefined
          }
          help={
            submitted && !effectiveName.trim()
              ? "A name is required."
              : submitted && !isValidResourceName(effectiveName.trim())
                ? RESOURCE_NAME_HINT
                : identityLocked
                  ? "A configuration cannot be renamed; create a new one instead."
                  : "Defaults to the model's own name."
          }
        >
          <Input
            data-testid="model-name"
            placeholder="gpt-4-1"
            value={effectiveName}
            disabled={identityLocked}
            onChange={(event) => {
              setNameTouched(true);
              set("name", event.target.value);
            }}
            css={{ fontFamily: theme.font.mono }}
          />
        </Form.Item>

        <Form.Item
          label="Namespace"
          required
          validateStatus={
            submitted && !draft.namespace.trim() ? "error" : undefined
          }
          help={
            submitted && !draft.namespace.trim()
              ? "A namespace is required."
              : undefined
          }
        >
          <Select
            data-testid="model-namespace"
            value={draft.namespace}
            loading={namespaces.isLoading}
            showSearch
            disabled={identityLocked}
            options={(namespaces.data ?? []).map((item) => ({
              value: item.name,
              label: item.name,
            }))}
            onChange={(next) => set("namespace", next)}
          />
        </Form.Item>

        {/* Authentication. Ollama runs locally and needs none; every other provider
            picks a mode: a key typed here (stored in a Secret the backend creates),
            an existing Secret referenced by name, forwarding the caller's own
            credential (passthrough), or no credential at all. */}
        {isOllama ? (
          <Alert
            type="info"
            showIcon
            data-testid="model-ollama-auth-note"
            title="Ollama models run locally and need no API key."
          />
        ) : (
          <>
            <Form.Item label="Authentication">
              <Radio.Group
                data-testid="model-auth-type"
                value={draft.authType}
                onChange={(event) =>
                  set("authType", event.target.value as ModelAuthType)
                }
                options={authOptions}
                optionType="button"
              />
            </Form.Item>

            {draft.authType === "apiKey" ? (
              <Form.Item
                label={
                  <Space size={6}>
                    {isEdit
                      ? "API key (leave blank to keep existing)"
                      : "API key"}
                    <DocsLink
                      href={info?.apiKeyLink ?? undefined}
                      title={`Get your ${providerDisplayName(draft.provider ?? "")} API key`}
                    />
                  </Space>
                }
                validateStatus={
                  submitted &&
                  !isEdit &&
                  !!draft.provider &&
                  !draft.apiKey.trim()
                    ? "error"
                    : undefined
                }
                help={
                  submitted &&
                  !isEdit &&
                  !!draft.provider &&
                  !draft.apiKey.trim()
                    ? "An API key is required, or choose another authentication type."
                    : "Typed once and stored in a Kubernetes Secret the controller creates and owns."
                }
              >
                <Input.Password
                  data-testid="model-api-key"
                  placeholder={
                    isEdit ? "Enter a new key to replace it" : "Enter API key…"
                  }
                  value={draft.apiKey}
                  autoComplete="new-password"
                  onChange={(event) => set("apiKey", event.target.value)}
                />
              </Form.Item>
            ) : null}

            {draft.authType === "secret" ? (
              <>
                <Form.Item
                  label="API key secret"
                  help="The name of an existing Kubernetes Secret holding the credential."
                >
                  <Input
                    data-testid="model-api-key-secret"
                    placeholder="kagent-openai"
                    value={draft.apiKeySecret}
                    onChange={(event) =>
                      set("apiKeySecret", event.target.value)
                    }
                  />
                </Form.Item>
                <Form.Item
                  label="Secret key"
                  validateStatus={
                    submitted &&
                    draft.apiKeySecretKey.trim() &&
                    !draft.apiKeySecret.trim()
                      ? "error"
                      : undefined
                  }
                  help={
                    submitted &&
                    draft.apiKeySecretKey.trim() &&
                    !draft.apiKeySecret.trim()
                      ? "A secret key needs the secret that holds it."
                      : "Which key inside that Secret. Defaults to the provider's usual key."
                  }
                >
                  <Input
                    data-testid="model-api-key-secret-key"
                    placeholder="OPENAI_API_KEY"
                    value={draft.apiKeySecretKey}
                    onChange={(event) =>
                      set("apiKeySecretKey", event.target.value)
                    }
                  />
                </Form.Item>
              </>
            ) : null}

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

        {/* Provider parameters. Required ones (an Azure endpoint, a Bedrock region)
            gate submission; the optional tuning knobs are tucked away until asked
            for so the common case stays short. */}
        {draft.provider && requiredKeys.length > 0 ? (
          <div data-testid="model-required-params">
            <Text css={{ fontSize: 13, fontWeight: 500 }}>
              Required parameters
            </Text>
            <div css={{ marginTop: theme.space(2) }}>
              {requiredKeys.map((key) => paramField(key, true))}
            </div>
          </div>
        ) : null}

        {draft.provider && optionalKeys.length > 0 ? (
          <Collapse
            ghost
            data-testid="model-optional-params"
            items={[
              {
                key: "optional",
                label: `Optional parameters (${optionalKeys.length})`,
                children: optionalKeys.map((key) => paramField(key, false)),
              },
            ]}
          />
        ) : null}

        {/* Advanced: headers sent on every request, and TLS for the connection to
            the provider. Both apply to any provider, so they sit apart from the
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
                    <Form.Item
                      label="Default headers"
                      help="Sent on every request to the provider — for API versioning, tracing, or provider-specific needs."
                    >
                      <Space
                        orientation="vertical"
                        size={8}
                        css={{ display: "flex" }}
                      >
                        {draft.defaultHeaders.map((row, index) => (
                          <Space
                            key={row.id}
                            size={8}
                            css={{ display: "flex" }}
                          >
                            <Input
                              data-testid="model-header-name"
                              aria-label={`Header ${index + 1} name`}
                              placeholder="Header-Name"
                              value={row.key}
                              onChange={(event) =>
                                setHeader(row.id, "key", event.target.value)
                              }
                            />
                            <Input
                              data-testid="model-header-value"
                              aria-label={`Header ${index + 1} value`}
                              placeholder="value"
                              value={row.value}
                              onChange={(event) =>
                                setHeader(row.id, "value", event.target.value)
                              }
                            />
                            <Button
                              aria-label={`Remove header ${index + 1}`}
                              icon={<Trash2 size={14} />}
                              onClick={() => removeHeader(row.id)}
                            />
                          </Space>
                        ))}
                        <Button
                          data-testid="model-add-header"
                          icon={<Plus size={14} />}
                          onClick={addHeader}
                        >
                          Add header
                        </Button>
                      </Space>
                    </Form.Item>

                    <Form.Item
                      label="CA certificate secret"
                      help="Name of a Kubernetes Secret holding a custom CA. Leave blank to use the system trust store."
                    >
                      <Input
                        data-testid="model-tls-ca-ref"
                        placeholder="my-ca-secret"
                        value={draft.tls.caCertSecretRef}
                        onChange={(event) =>
                          setTls("caCertSecretRef", event.target.value)
                        }
                      />
                    </Form.Item>
                    <Form.Item label="CA certificate secret key">
                      <Input
                        data-testid="model-tls-ca-key"
                        placeholder="ca.crt"
                        value={draft.tls.caCertSecretKey}
                        onChange={(event) =>
                          setTls("caCertSecretKey", event.target.value)
                        }
                      />
                    </Form.Item>
                    <Form.Item>
                      <Space size={16} wrap>
                        <label
                          css={{
                            display: "inline-flex",
                            gap: 6,
                            alignItems: "center",
                          }}
                        >
                          <input
                            type="checkbox"
                            data-testid="model-tls-disable-system-cas"
                            checked={draft.tls.disableSystemCAs}
                            onChange={(event) =>
                              setTls("disableSystemCAs", event.target.checked)
                            }
                          />
                          Trust only the CA above (disable system CAs)
                        </label>
                        <label
                          css={{
                            display: "inline-flex",
                            gap: 6,
                            alignItems: "center",
                          }}
                        >
                          <input
                            type="checkbox"
                            data-testid="model-tls-disable-verify"
                            checked={draft.tls.disableVerify}
                            onChange={(event) =>
                              setTls("disableVerify", event.target.checked)
                            }
                          />
                          Skip TLS verification
                        </label>
                      </Space>
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

      {submitted && issues.length > 0 ? (
        <Alert
          type="error"
          showIcon
          title={
            identityLocked
              ? "This model cannot be saved yet"
              : "This model cannot be created yet"
          }
          data-testid="model-form-errors"
          description={
            <ul css={{ margin: 0, paddingLeft: theme.space(5) }}>
              {issues.map((issue) => (
                <li key={issue}>{issue}</li>
              ))}
            </ul>
          }
        />
      ) : null}

      {failure !== undefined ? (
        <SubmitError
          error={failure}
          what="model configuration"
          outcome={outcome}
          onRetry={() => void submit()}
          data-testid="model-submit-error"
        />
      ) : null}

      <Space size={8}>
        <Button
          type="primary"
          data-testid="model-submit"
          loading={saving}
          onClick={() => void submit()}
        >
          {submitLabel}
        </Button>
        <Link to={paths.models}>
          <Button>Cancel</Button>
        </Link>
      </Space>
    </Space>
  );
}
