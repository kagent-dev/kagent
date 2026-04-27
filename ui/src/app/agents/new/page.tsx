"use client";
import React, { useState, useEffect, Suspense, useCallback, useMemo } from "react";
import { Loader2 } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { formAgentTypeFromApi, formUsesByoSections, formUsesDeclarativeSections } from "@/lib/agentFormLayout";
import { ModelConfig, AgentType, ContextConfig } from "@/types";
import { SystemPromptSection } from "@/components/create/SystemPromptSection";
import { newPromptSourceRow, type PromptSourceRow } from "@/lib/promptSourceRow";
import { ModelSelectionSection } from "@/components/create/ModelSelectionSection";
import { ToolsSection } from "@/components/create/ToolsSection";
import { MemorySection } from "@/components/create/MemorySection";
import { ContextSection } from "@/components/create/ContextSection";
import { useRouter, useSearchParams } from "next/navigation";
import { useAgents } from "@/components/AgentsProvider";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { AgentFormData } from "@/components/AgentsProvider";
import { Tool, EnvVar } from "@/types";
import { toast } from "sonner";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import {
  MAX_SKILLS_PER_SOURCE,
  formRowsToGitRepos,
  gitRepoToFormRow,
  newEmptyGitSkillRow,
  validateDeclarativeAgentSkills,
  type GitSkillFormRow,
} from "@/lib/agentSkillsForm";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { FormSection, FieldRoot, FieldLabel, FieldHint, FieldError } from "@/components/agent-form/form-primitives";
import { ByoDeploymentFields } from "@/components/agent-form/ByoDeploymentFields";
import { AgentSkillsFormSection } from "@/components/agent-form/AgentSkillsFormSection";
import { ServiceAccountNameField } from "@/components/agent-form/ServiceAccountNameField";
import { AgentFormValidationErrors } from "@/components/agent-form/agent-form-types";
import { focusFirstFormError } from "@/components/agent-form/focusFirstFormError";
import { PageHeader } from "@/components/layout/PageHeader";

interface AgentPageContentProps {
  isEditMode: boolean;
  agentName: string | null;
  agentNamespace: string | null;
}

const DEFAULT_SYSTEM_PROMPT = `You're a helpful agent, made by the kagent team.

# Instructions
    - If user question is unclear, ask for clarification before running any tools
    - Always be helpful and friendly
    - If you don't know how to answer the question DO NOT make things up, tell the user "Sorry, I don't know how to answer that" and ask them to clarify the question further
    - If you are unable to help, or something goes wrong, refer the user to https://kagent.dev for more information or support.

# Response format:
    - ALWAYS format your response as Markdown
    - Your response will include a summary of actions you took and an explanation of the result
    - If you created any artifacts such as files or resources, you will include those in your response as well`

function AgentPageContent({ isEditMode, agentName, agentNamespace }: AgentPageContentProps) {
  const router = useRouter();
  const { models, loading, error, createNewAgent, updateAgent, getAgent, validateAgentData } = useAgents();

  type SelectedModelType = ModelConfig;

  interface FormState {
    name: string;
    namespace: string;
    description: string;
    agentType: AgentType;
    systemPrompt: string;
    selectedModel: SelectedModelType | null;
    selectedMemoryModel: SelectedModelType | null;
    memoryTtlDays: string;
    selectedTools: Tool[];
    skillRefs: string[];
    skillGitRepos: GitSkillFormRow[];
    skillsGitAuthSecretName: string;
    byoImage: string;
    byoCmd: string;
    byoArgs: string;
    replicas: string;
    imagePullPolicy: string;
    imagePullSecrets: string[];
    envPairs: { name: string; value?: string; isSecret?: boolean; secretName?: string; secretKey?: string; optional?: boolean }[];
    stream: boolean;
    contextConfig: ContextConfig | undefined;
    serviceAccountName: string;
    promptSourceRows: PromptSourceRow[];
    isSubmitting: boolean;
    isLoading: boolean;
    errors: AgentFormValidationErrors;
  }

  const [formDirty, setFormDirty] = useState(false);

  const [state, setState] = useState<FormState>({
    name: "",
    namespace: "default",
    description: "",
    agentType: "Declarative",
    systemPrompt: isEditMode ? "" : DEFAULT_SYSTEM_PROMPT,
    selectedModel: null,
    selectedMemoryModel: null,
    memoryTtlDays: "",
    selectedTools: [],
    skillRefs: [""],
    skillGitRepos: [newEmptyGitSkillRow()],
    skillsGitAuthSecretName: "",
    byoImage: "",
    byoCmd: "",
    byoArgs: "",
    replicas: "",
    imagePullPolicy: "",
    imagePullSecrets: [""],
    envPairs: [{ name: "", value: "", isSecret: false }],
    stream: false,
    contextConfig: undefined,
    serviceAccountName: "",
    promptSourceRows: [newPromptSourceRow()],
    isSubmitting: false,
    isLoading: isEditMode,
    errors: {},
  });

  const useDeclarativeAgentFields = formUsesDeclarativeSections(state.agentType, state.byoImage);
  const showByoFields = formUsesByoSections(state.agentType, state.byoImage);
  const disabled = state.isSubmitting || state.isLoading;

  useEffect(() => {
    if (!formDirty) {
      return;
    }
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [formDirty]);

  const resolvedGitSkillRepos = useMemo(
    () => formRowsToGitRepos(state.skillGitRepos || []),
    [state.skillGitRepos],
  );

  const ensureConfigMapSource = useCallback((cmName: string) => {
    const t = cmName.trim();
    if (!t) {
      return;
    }
    setState((prev) => {
      if (prev.promptSourceRows.some((r) => r.name.trim() === t)) {
        return { ...prev, errors: { ...prev.errors, promptSources: undefined } };
      }
      const nonEmpty = prev.promptSourceRows.filter((r) => r.name.trim() !== "");
      return {
        ...prev,
        errors: { ...prev.errors, promptSources: undefined },
        promptSourceRows: [...nonEmpty, { id: crypto.randomUUID(), name: t, alias: "" }],
      };
    });
  }, []);

  const includeSourceIdForConfigMap = useCallback(
    (cmName: string) => {
      const row = state.promptSourceRows.find((r) => r.name.trim() === cmName);
      const a = row?.alias?.trim();
      return a || cmName;
    },
    [state.promptSourceRows],
  );

  useEffect(() => {
    const fetchAgentData = async () => {
      if (isEditMode && agentName && agentNamespace) {
        try {
          setState((prev) => ({ ...prev, isLoading: true }));
          const agentResponse = await getAgent(agentName, agentNamespace);

          if (!agentResponse) {
            toast.error("Agent not found");
            setState((prev) => ({ ...prev, isLoading: false }));
            return;
          }
          const agent = agentResponse.agent;
          if (agent) {
            try {
              const baseUpdates: Partial<FormState> = {
                name: agent.metadata.name || "",
                namespace: agent.metadata.namespace || "",
                description: agent.spec?.description || "",
                agentType: formAgentTypeFromApi(agent.spec.type, agentResponse.workloadMode),
              };
              const useDeclarativeForm = agent.spec.type === "Declarative";
              if (useDeclarativeForm) {
                const decl = agent.spec?.declarative;
                const memorySpec = decl?.memory ?? (agent.spec as { memory?: { modelConfig: string; ttlDays?: number } })?.memory;
                const memoryModelConfig = memorySpec?.modelConfig
                  ? `${agent.metadata.namespace}/${memorySpec.modelConfig}`
                  : "";
                const pt = decl?.promptTemplate;
                const srcRows: PromptSourceRow[] =
                  pt?.dataSources?.map((ds) => ({
                    id: crypto.randomUUID(),
                    name: ds.name || "",
                    alias: ds.alias || "",
                  })) ?? [newPromptSourceRow()];
                setState((prev) => ({
                  ...prev,
                  ...baseUpdates,
                  systemPrompt: decl?.systemMessage || "",
                  promptSourceRows: srcRows.length > 0 ? srcRows : [newPromptSourceRow()],
                  selectedTools: decl?.tools && agentResponse.tools ? agentResponse.tools : [],
                  selectedModel: agentResponse.modelConfigRef
                    ? { ref: agentResponse.modelConfigRef, spec: { model: agentResponse.model || "", provider: "" } }
                    : null,
                  skillRefs: agent.spec?.skills?.refs && agent.spec.skills.refs.length > 0 ? agent.spec.skills.refs : [""],
                  skillGitRepos:
                    agent.spec?.skills?.gitRefs && agent.spec.skills.gitRefs.length > 0
                      ? agent.spec.skills.gitRefs.map(gitRepoToFormRow)
                      : [newEmptyGitSkillRow()],
                  skillsGitAuthSecretName: agent.spec?.skills?.gitAuthSecretRef?.name || "",
                  stream: decl?.stream ?? false,
                  selectedMemoryModel: memoryModelConfig
                    ? { ref: memoryModelConfig, spec: { model: memorySpec?.modelConfig || "", provider: "" } }
                    : null,
                  memoryTtlDays: memorySpec?.ttlDays ? String(memorySpec.ttlDays) : "",
                  contextConfig: decl?.context,
                  serviceAccountName: decl?.deployment?.serviceAccountName || "",
                  byoImage: "",
                  byoCmd: "",
                  byoArgs: "",
                }));
              } else {
                setState((prev) => ({
                  ...prev,
                  ...baseUpdates,
                  systemPrompt: "",
                  selectedModel: null,
                  selectedTools: [],
                  selectedMemoryModel: null,
                  memoryTtlDays: "",
                  byoImage: agent.spec?.byo?.deployment?.image || "",
                  byoCmd: agent.spec?.byo?.deployment?.cmd || "",
                  byoArgs: (agent.spec?.byo?.deployment?.args || []).join(" "),
                  replicas: agent.spec?.byo?.deployment?.replicas !== undefined ? String(agent.spec?.byo?.deployment?.replicas) : "",
                  imagePullPolicy: agent.spec?.byo?.deployment?.imagePullPolicy || "",
                  imagePullSecrets: (agent.spec?.byo?.deployment?.imagePullSecrets || [])
                    .map((s: { name: string }) => s.name)
                    .concat((agent.spec?.byo?.deployment?.imagePullSecrets || []).length === 0 ? [""] : []),
                  envPairs: (agent.spec?.byo?.deployment?.env || [])
                    .map((e: EnvVar) =>
                      e?.valueFrom?.secretKeyRef
                        ? {
                            name: e.name || "",
                            isSecret: true,
                            secretName: e.valueFrom.secretKeyRef.name || "",
                            secretKey: e.valueFrom.secretKeyRef.key || "",
                            optional: e.valueFrom.secretKeyRef.optional,
                          }
                        : { name: e.name || "", value: e.value || "", isSecret: false },
                    )
                    .concat((agent.spec?.byo?.deployment?.env || []).length === 0
                      ? [{ name: "", value: "", isSecret: false }]
                      : []),
                  serviceAccountName: agent.spec?.byo?.deployment?.serviceAccountName || "",
                }));
              }
            } catch (extractError) {
              console.error("Error extracting assistant data:", extractError);
              toast.error("Failed to extract agent data");
            }
          } else {
            toast.error("Agent not found");
          }
        } catch (e) {
          console.error("Error fetching agent:", e);
          toast.error("Failed to load agent data");
        } finally {
          setState((prev) => ({ ...prev, isLoading: false }));
        }
      }
    };

    void fetchAgentData();
  }, [isEditMode, agentName, agentNamespace, getAgent]);

  const validateForm = () => {
    const memoryEnabled = !!(state.selectedMemoryModel?.ref || state.memoryTtlDays);
    const formData = {
      name: state.name,
      namespace: state.namespace,
      description: state.description,
      type: state.agentType,
      systemPrompt: state.systemPrompt,
      promptSources: state.promptSourceRows.map(({ name, alias }) => ({ name, alias })),
      modelName: state.selectedModel?.ref || "",
      tools: state.selectedTools,
      byoImage: state.byoImage,
      memory: memoryEnabled
        ? {
            modelConfig: state.selectedMemoryModel?.ref || "",
            ttlDays: state.memoryTtlDays ? parseInt(state.memoryTtlDays, 10) : undefined,
          }
        : undefined,
      context: state.contextConfig,
      serviceAccountName: state.serviceAccountName,
    };

    const newErrors = validateAgentData(formData);

    if (useDeclarativeAgentFields) {
      const skillsError = validateDeclarativeAgentSkills({
        skillRefs: state.skillRefs || [],
        skillGitRepos: state.skillGitRepos || [],
        skillsGitAuthSecretName: state.skillsGitAuthSecretName || "",
      });
      if (skillsError) {
        newErrors.skills = skillsError;
      }
    }

    setState((prev) => ({ ...prev, errors: newErrors }));
    const valid = Object.keys(newErrors).length === 0;
    if (!valid) {
      requestAnimationFrame(() => {
        focusFirstFormError(newErrors, { byoSectionsActive: showByoFields });
      });
    }
    return valid;
  };

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const validateField = (fieldName: keyof AgentFormValidationErrors, value: any) => {
    const formData: Partial<AgentFormData> = {};
    const memoryEnabled = !!(state.selectedMemoryModel?.ref || state.memoryTtlDays);

    switch (fieldName) {
      case "name":
        formData.name = value;
        break;
      case "namespace":
        formData.namespace = value;
        break;
      case "description":
        formData.description = value;
        break;
      case "type":
        formData.type = value;
        break;
      case "systemPrompt":
        formData.systemPrompt = value;
        break;
      case "model":
        formData.modelName = value;
        break;
      case "tools":
        formData.tools = value;
        break;
      case "memoryModel":
        if (memoryEnabled || value) {
          formData.memory = {
            modelConfig: value,
            ttlDays: state.memoryTtlDays ? parseInt(state.memoryTtlDays, 10) : undefined,
          };
        }
        break;
      case "memoryTtl":
        if (memoryEnabled || value) {
          formData.memory = {
            modelConfig: state.selectedMemoryModel?.ref || "",
            ttlDays: value ? parseInt(value, 10) : undefined,
          };
        }
        break;
      case "serviceAccountName":
        formData.serviceAccountName = value;
        break;
    }

    const fieldErrors = validateAgentData(formData);
    const valueForField = (fieldErrors as Record<string, string | undefined>)[fieldName as string];
    setState((prev) => ({
      ...prev,
      errors: {
        ...prev.errors,
        [fieldName]: valueForField,
      },
    }));
  };

  const handleSaveAgent = async () => {
    if (!validateForm()) {
      return;
    }

    try {
      setState((prev) => ({ ...prev, isSubmitting: true }));
      if (useDeclarativeAgentFields && !state.selectedModel) {
        throw new Error("Model is required for this agent type.");
      }

      const memoryEnabled = !!(state.selectedMemoryModel?.ref || state.memoryTtlDays);

      const agentData = {
        name: state.name,
        namespace: state.namespace,
        description: state.description,
        type: state.agentType,
        systemPrompt: state.systemPrompt,
        promptSources: state.promptSourceRows.map(({ name, alias }) => ({ name, alias })),
        modelName: state.selectedModel?.ref || "",
        stream: state.stream,
        tools: state.selectedTools,
        skillRefs: useDeclarativeAgentFields ? (state.skillRefs || []).filter((ref) => ref.trim()) : undefined,
        skillGitRepos: useDeclarativeAgentFields ? formRowsToGitRepos(state.skillGitRepos || []) : undefined,
        skillsGitAuthSecretName:
          useDeclarativeAgentFields && (state.skillsGitAuthSecretName || "").trim()
            ? (state.skillsGitAuthSecretName || "").trim()
            : undefined,
        memory:
          useDeclarativeAgentFields && memoryEnabled
            ? {
                modelConfig: state.selectedMemoryModel?.ref || "",
                ttlDays: state.memoryTtlDays ? parseInt(state.memoryTtlDays, 10) : undefined,
              }
            : undefined,
        context: useDeclarativeAgentFields ? state.contextConfig : undefined,
        byoImage: state.byoImage,
        byoCmd: state.byoCmd || undefined,
        byoArgs: state.byoArgs ? state.byoArgs.split(/\s+/).filter(Boolean) : undefined,
        replicas: state.replicas ? parseInt(state.replicas, 10) : undefined,
        imagePullPolicy: state.imagePullPolicy || undefined,
        imagePullSecrets: (state.imagePullSecrets || [])
          .filter((n) => n.trim())
          .map((n) => ({ name: n.trim() })),
        env: (state.envPairs || [])
          .map<EnvVar | null>((ev) => {
            const name = (ev.name || "").trim();
            if (!name) {
              return null;
            }
            if (ev.isSecret) {
              const secName = (ev.secretName || "").trim();
              const secKey = (ev.secretKey || "").trim();
              if (!secName || !secKey) {
                return null;
              }
              return {
                name,
                valueFrom: {
                  secretKeyRef: {
                    name: secName,
                    key: secKey,
                    optional: ev.optional,
                  },
                },
              } as EnvVar;
            }
            return { name, value: ev.value ?? "" } as EnvVar;
          })
          .filter((e): e is EnvVar => e !== null),
        serviceAccountName: state.serviceAccountName.trim() || undefined,
      };

      let result;

      if (isEditMode && agentName && agentNamespace) {
        result = await updateAgent(agentData);
      } else {
        result = await createNewAgent(agentData);
      }

      if (result.error) {
        throw new Error(result.error);
      }

      setFormDirty(false);
      router.push(`/agents`);
    } catch (e) {
      console.error(`Error ${isEditMode ? "updating" : "creating"} agent:`, e);
      const errorMessage =
        e instanceof Error ? e.message : `Failed to ${isEditMode ? "update" : "create"} agent. Please try again.`;
      toast.error(errorMessage);
      setState((prev) => ({ ...prev, isSubmitting: false }));
    }
  };

  const clearSkillsError = useCallback(() => {
    setState((prev) => ({ ...prev, errors: { ...prev.errors, skills: undefined } }));
  }, []);

  const renderPageContent = () => {
    if (error) {
      return <ErrorState message={error} />;
    }

    return (
      <div className="relative min-h-screen touch-manipulation bg-gradient-to-b from-background via-background to-muted/15">
        <a
          href="#agent-form-main"
          className="absolute -left-full top-0 z-[100] whitespace-nowrap p-2 text-sm text-primary focus:left-4 focus:top-4 focus:rounded-md focus:bg-primary focus:px-3 focus:py-2 focus:text-primary-foreground"
        >
          Skip to form
        </a>
        <div className="mx-auto max-w-3xl px-4 py-10 sm:px-6">
          <PageHeader
            titleId="agent-form-page-title"
            title={isEditMode ? "Edit Agent" : "New Agent"}
          />

          <main
            id="agent-form-main"
            className="scroll-mt-8 outline-none"
            tabIndex={-1}
            aria-labelledby="agent-form-page-title"
          >
            <form
              className="space-y-8"
              noValidate
              onInput={() => {
                if (!formDirty) {
                  setFormDirty(true);
                }
              }}
              onSubmit={(e) => {
                e.preventDefault();
                void handleSaveAgent();
              }}
            >
            <p className="sr-only" role="status" aria-live="polite" aria-atomic>
              {state.isSubmitting
                ? isEditMode
                  ? "Saving…"
                  : "Creating…"
                : ""}
            </p>
            <FormSection
              title="Identity"
              description="Name, where it lives in the cluster, and a short working note."
            >
              <FieldRoot>
                <FieldLabel htmlFor="agent-field-name">Agent name</FieldLabel>
                <FieldHint>Resource name in the cluster (shown in the UI and in refs).</FieldHint>
                <Input
                  id="agent-field-name"
                  name="agentName"
                  value={state.name}
                  onChange={(e) => setState((prev) => ({ ...prev, name: e.target.value }))}
                  onBlur={() => validateField("name", state.name)}
                  className={state.errors.name ? "border-destructive" : ""}
                  placeholder="e.g. my-assistant"
                  autoComplete="off"
                  spellCheck={false}
                  translate="no"
                  disabled={disabled || isEditMode}
                  aria-invalid={!!state.errors.name}
                />
                <FieldError>{state.errors.name}</FieldError>
              </FieldRoot>

              <FieldRoot>
                <FieldLabel htmlFor="agent-field-namespace">Namespace</FieldLabel>
                <FieldHint>Must exist and match where ModelConfigs and tools are resolved.</FieldHint>
                <NamespaceCombobox
                  id="agent-field-namespace"
                  value={state.namespace}
                  onValueChange={(value) => {
                    setState((prev) => ({ ...prev, selectedModel: null, namespace: value }));
                    validateField("namespace", value);
                  }}
                  disabled={disabled || isEditMode}
                />
              </FieldRoot>

              <FieldRoot>
                <FieldLabel>Agent type</FieldLabel>
                <FieldHint>Declarative and sandbox (without a custom image) use the built-in model runtime. BYO or sandbox with a custom image adds container settings.</FieldHint>
                <Select
                  value={state.agentType}
                  onValueChange={(val) => {
                    const next = val as AgentType;
                    setState((prev) => ({ ...prev, agentType: next }));
                    validateField("type", val);
                  }}
                  disabled={disabled}
                >
                  <SelectTrigger id="agent-field-type" className="w-full">
                    <SelectValue placeholder="Select type…" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="Declarative">Declarative</SelectItem>
                    <SelectItem value="Sandbox">Sandbox</SelectItem>
                    <SelectItem value="BYO">BYO</SelectItem>
                  </SelectContent>
                </Select>
              </FieldRoot>

              <FieldRoot>
                <FieldLabel htmlFor="agent-desc">Description (optional)</FieldLabel>
                <FieldHint>Internal note only; not sent to the model as instructions.</FieldHint>
                <Textarea
                  id="agent-desc"
                  name="description"
                  value={state.description}
                  onChange={(e) => setState((prev) => ({ ...prev, description: e.target.value }))}
                  onBlur={() => validateField("description", state.description)}
                  className={`min-h-[96px] ${state.errors.description ? "border-destructive" : ""}`}
                  placeholder="What this agent is for…"
                  autoComplete="off"
                  disabled={disabled}
                  aria-invalid={!!state.errors.description}
                />
                <FieldError>{state.errors.description}</FieldError>
              </FieldRoot>
            </FormSection>

            {useDeclarativeAgentFields && (
              <FormSection
                title="Model & behavior"
                description="Instructions, main model, streaming, and optional pod service account for this declarative or sandbox agent."
              >
                <SystemPromptSection
                  value={state.systemPrompt}
                  onChange={(e) => setState((prev) => ({ ...prev, systemPrompt: e.target.value }))}
                  onBlur={() => validateField("systemPrompt", state.systemPrompt)}
                  error={state.errors.systemPrompt}
                  disabled={disabled}
                  mentionNamespace={state.namespace}
                  onPickInclude={(pick) => ensureConfigMapSource(pick.configMapName)}
                  includeSourceIdForConfigMap={includeSourceIdForConfigMap}
                />

                <ModelSelectionSection
                  allModels={models}
                  selectedModel={state.selectedModel}
                  setSelectedModel={(model) => {
                    setState((prev) => ({ ...prev, selectedModel: model as ModelConfig | null }));
                  }}
                  error={state.errors.model}
                  isSubmitting={disabled}
                  onChange={(modelRef) => validateField("model", modelRef)}
                  agentNamespace={state.namespace}
                />

                <div className="flex gap-3 rounded-md border border-border/60 bg-muted/20 p-3">
                  <div className="flex h-5 shrink-0 items-center self-start">
                    <Checkbox
                      id="stream-toggle"
                      checked={state.stream}
                      onCheckedChange={(checked) => setState((prev) => ({ ...prev, stream: !!checked }))}
                      disabled={disabled}
                    />
                  </div>
                  <div className="min-w-0 space-y-1.5">
                    <Label
                      htmlFor="stream-toggle"
                      className="block cursor-pointer text-sm font-medium leading-5 text-foreground"
                    >
                      Stream model output
                    </Label>
                    <p className="text-xs leading-snug text-muted-foreground">
                      Token-by-token responses where the provider supports it
                    </p>
                  </div>
                </div>

                <ServiceAccountNameField
                  value={state.serviceAccountName}
                  onChange={(v) => setState((prev) => ({ ...prev, serviceAccountName: v }))}
                  onBlur={() => validateField("serviceAccountName", state.serviceAccountName)}
                  error={state.errors.serviceAccountName}
                  disabled={disabled}
                />
              </FormSection>
            )}

            {showByoFields && (
              <FormSection
                title="Container"
                description="Image and process for BYO, or a custom image on sandbox. Open the lower panel for pull secrets, scheduling, and environment."
              >
                <ByoDeploymentFields
                  byoImage={state.byoImage}
                  byoCmd={state.byoCmd}
                  byoArgs={state.byoArgs}
                  replicas={state.replicas}
                  imagePullPolicy={state.imagePullPolicy}
                  imagePullSecrets={state.imagePullSecrets}
                  envPairs={state.envPairs}
                  serviceAccountName={state.serviceAccountName}
                  errors={{ model: state.errors.model, serviceAccountName: state.errors.serviceAccountName }}
                  disabled={disabled}
                  onByoImageChange={(v) => setState((prev) => ({ ...prev, byoImage: v }))}
                  onByoCmdChange={(v) => setState((prev) => ({ ...prev, byoCmd: v }))}
                  onByoArgsChange={(v) => setState((prev) => ({ ...prev, byoArgs: v }))}
                  onReplicasChange={(v) => setState((prev) => ({ ...prev, replicas: v }))}
                  onImagePullPolicyChange={(v) => setState((prev) => ({ ...prev, imagePullPolicy: v }))}
                  onImagePullSecretsUpdate={(s) => setState((prev) => ({ ...prev, imagePullSecrets: s }))}
                  onAddImagePullSecret={() => setState((prev) => ({ ...prev, imagePullSecrets: [...prev.imagePullSecrets, ""] }))}
                  onRemoveImagePullSecret={(idx) =>
                    setState((prev) => ({ ...prev, imagePullSecrets: prev.imagePullSecrets.filter((_, i) => i !== idx) }))
                  }
                  onEnvPairChange={(index, next) => {
                    const u = [...state.envPairs];
                    u[index] = next;
                    setState((prev) => ({ ...prev, envPairs: u }));
                  }}
                  onAddEnvPair={() =>
                    setState((prev) => ({
                      ...prev,
                      envPairs: [...prev.envPairs, { name: "", value: "", isSecret: false }],
                    }))
                  }
                  onRemoveEnvPair={(index) => setState((prev) => ({ ...prev, envPairs: prev.envPairs.filter((_, i) => i !== index) }))}
                  onServiceAccountChange={(v) => setState((prev) => ({ ...prev, serviceAccountName: v }))}
                  onServiceAccountBlur={() => validateField("serviceAccountName", state.serviceAccountName)}
                  onValidateByoImage={() => validateField("model", state.byoImage)}
                />
              </FormSection>
            )}

            {useDeclarativeAgentFields && (
              <>
                <FormSection id="section-tools" title="Tools">
                  <ToolsSection
                    selectedTools={state.selectedTools}
                    setSelectedTools={(tools) => setState((prev) => ({ ...prev, selectedTools: tools }))}
                    isSubmitting={disabled}
                    onBlur={() => validateField("tools", state.selectedTools)}
                    currentAgentName={state.name}
                    currentAgentNamespace={state.namespace}
                  />
                </FormSection>

                <FormSection
                  title="Long-term memory"
                  description="Optional: embed and recall information across sessions using a dedicated model config for embeddings."
                >
                  <MemorySection
                    allModels={models}
                    selectedModel={state.selectedMemoryModel}
                    setSelectedModel={(model) => {
                      setState((prev) => ({ ...prev, selectedMemoryModel: model as ModelConfig | null }));
                      validateField("memoryModel", (model as ModelConfig | null)?.ref || "");
                    }}
                    agentNamespace={state.namespace}
                    ttlDays={state.memoryTtlDays}
                    onTtlChange={(value) => setState((prev) => ({ ...prev, memoryTtlDays: value }))}
                    onTtlBlur={() => validateField("memoryTtl", state.memoryTtlDays)}
                    modelError={state.errors.memoryModel}
                    ttlError={state.errors.memoryTtl}
                    isSubmitting={disabled}
                  />
                </FormSection>

                <FormSection
                  title="Context"
                  description="Compaction and summarization to keep long runs within model limits. Leave off for the default context behavior."
                >
                  <ContextSection
                    context={state.contextConfig}
                    onChange={(ctx) => setState((prev) => ({ ...prev, contextConfig: ctx }))}
                    isSubmitting={disabled}
                  />
                </FormSection>

                <AgentSkillsFormSection
                  skillRefs={state.skillRefs}
                  skillGitRepos={state.skillGitRepos}
                  skillsGitAuthSecretName={state.skillsGitAuthSecretName}
                  skillsError={state.errors.skills}
                  disabled={disabled}
                  resolvedGitSkillRepos={resolvedGitSkillRepos}
                  onSkillRefChange={(index, value) => {
                    const copy = [...state.skillRefs];
                    copy[index] = value;
                    setState((prev) => ({ ...prev, skillRefs: copy, errors: { ...prev.errors, skills: undefined } }));
                  }}
                  onAddSkillRef={() => {
                    if (state.skillRefs.length < MAX_SKILLS_PER_SOURCE) {
                      setState((prev) => ({ ...prev, skillRefs: [...prev.skillRefs, ""] }));
                    }
                  }}
                  onRemoveSkillRef={(index) =>
                    setState((prev) => ({ ...prev, skillRefs: prev.skillRefs.filter((_, i) => i !== index) }))
                  }
                  onGitRowChange={(index, next) => {
                    const copy = [...state.skillGitRepos];
                    copy[index] = next;
                    setState((prev) => ({ ...prev, skillGitRepos: copy, errors: { ...prev.errors, skills: undefined } }));
                  }}
                  onAddGitRow={() => {
                    if (state.skillGitRepos.length < MAX_SKILLS_PER_SOURCE) {
                      setState((prev) => ({ ...prev, skillGitRepos: [...prev.skillGitRepos, newEmptyGitSkillRow()] }));
                    }
                  }}
                  onRemoveGitRow={(index) =>
                    setState((prev) => ({
                      ...prev,
                      skillGitRepos:
                        prev.skillGitRepos.length <= 1
                          ? [newEmptyGitSkillRow()]
                          : prev.skillGitRepos.filter((_, i) => i !== index),
                    }))
                  }
                  onGitAuthSecretChange={(value) => setState((prev) => ({ ...prev, skillsGitAuthSecretName: value }))}
                  onClearSkillsError={clearSkillsError}
                />
              </>
            )}

            <div className="flex justify-end border-t border-border/50 pt-6">
              <Button
                type="submit"
                size="lg"
                disabled={disabled}
                className="min-w-[10rem]"
                aria-busy={state.isSubmitting}
              >
                {state.isSubmitting ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 shrink-0 animate-spin" aria-hidden />
                    {isEditMode ? "Saving…" : "Creating…"}
                  </>
                ) : isEditMode ? (
                  "Save Changes"
                ) : (
                  "Create Agent"
                )}
              </Button>
            </div>
            </form>
          </main>
        </div>
      </div>
    );
  };

  return (
    <>
      {(loading || state.isLoading) && <LoadingState />}
      {renderPageContent()}
    </>
  );
}

export default function AgentPage() {
  const searchParams = useSearchParams();
  const isEditMode = searchParams.get("edit") === "true";
  const agentName = searchParams.get("name");
  const agentNamespace = searchParams.get("namespace");
  const formKey = isEditMode ? `edit-${agentName}-${agentNamespace}` : "create";

  return (
    <Suspense fallback={<LoadingState />}>
      <AgentPageContent key={formKey} isEditMode={isEditMode} agentName={agentName} agentNamespace={agentNamespace} />
    </Suspense>
  );
}
