import {
  gitRepoToFormRow,
  newEmptyGitSkillRow,
  newEmptyS3SkillRow,
  s3RefToFormRow,
  type GitSkillFormRow,
  type S3SkillFormRow,
} from "@/lib/agentSkillsForm";
import {
  formUsesByoSections,
  formUsesDeclarativeSections,
  formWorkloadKindFromApi,
  type AgentFormWorkloadKind,
} from "@/lib/agentFormLayout";
import {
  validateAgentHarnessForm,
  type AgentHarnessFormSlice,
  type AgentHarnessFormValidationError,
} from "@/lib/agentHarnessForm";
import {
  newPromptSourceRow,
  type PromptSourceRow,
} from "@/lib/promptSourceRow";
import {
  buildSandboxSubstrateFromForm,
  sandboxFieldsFromApiSpec,
} from "@/lib/sandboxAgentForm";
import { isMcpTool } from "@/lib/toolUtils";
import { k8sRefUtils } from "@/lib/k8sUtils";
import { isResourceNameValid } from "@/lib/utils";
import type {
  AgentType,
  AgentResponse,
  AgentSpec,
  ContextConfig,
  EnvVar,
  GitRepo,
  ModelConfig,
  PromptSource,
  S3SkillRef,
  SandboxAgent,
  DeclarativeAgentSpec,
  Tool,
} from "@/types";

export interface AgentFormValidationErrors {
  name?: string;
  namespace?: string;
  description?: string;
  type?: string;
  systemPrompt?: string;
  model?: string;
  knowledgeSources?: string;
  tools?: string;
  skills?: string;
  memoryModel?: string;
  memoryTtl?: string;
  promptSources?: string;
  byoCmd?: string;
  agentHarness?: AgentHarnessFormValidationError;
}

export interface AgentFormData {
  name: string;
  namespace: string;
  description: string;
  type?: AgentType;
  systemPrompt?: string;
  modelName?: string;
  tools: Tool[];
  stream?: boolean;
  skillRefs?: string[];
  skillGitRepos?: GitRepo[];
  skillsGitAuthSecretName?: string;
  skillS3Repos?: S3SkillRef[];
  memory?: {
    modelConfig?: string;
    ttlDays?: number;
  };
  context?: ContextConfig;
  shareTools?: boolean;
  promptSources?: Array<{ name: string; alias: string }>;
  agentHarness?: AgentHarnessFormSlice;
  byoImage?: string;
  byoCmd?: string;
  byoArgs?: string[];
  env?: EnvVar[];
  substrateWorkerPoolRefName?: string;
  substrateSnapshotsLocation?: string;
}

export type AgentWorkloadFormData = Omit<
  AgentFormData,
  "type" | "agentHarness"
> & {
  type?: AgentFormWorkloadKind;
};

export interface AgentFormEnvRow {
  name: string;
  value?: string;
  isSecret?: boolean;
  secretName?: string;
  secretKey?: string;
  optional?: boolean;
}

export interface AgentFormFields {
  name: string;
  namespace: string;
  description: string;
  agentType: AgentFormWorkloadKind;
  systemPrompt: string;
  selectedModel: ModelConfig | null;
  selectedMemoryModel: ModelConfig | null;
  memoryTtlDays: string;
  selectedTools: Tool[];
  skillRefs: string[];
  skillGitRepos: GitSkillFormRow[];
  skillsGitAuthSecretName: string;
  skillS3Repos: S3SkillFormRow[];
  byoImage: string;
  byoCmd: string;
  byoArgs: string;
  envPairs: AgentFormEnvRow[];
  stream: boolean;
  shareTools: boolean;
  contextConfig: ContextConfig | undefined;
  promptSourceRows: PromptSourceRow[];
  substrateWorkerPoolRefName: string;
  substrateSnapshotsLocation: string;
}

export interface AgentFormState extends AgentFormFields {
  isSubmitting: boolean;
  isLoading: boolean;
  errors: AgentFormValidationErrors;
}

interface CreateInitialAgentFormStateOptions {
  namespace: string;
  isEditMode: boolean;
  defaultSystemPrompt: string;
}

export function createInitialAgentFormState({
  namespace,
  isEditMode,
  defaultSystemPrompt,
}: CreateInitialAgentFormStateOptions): AgentFormState {
  return {
    name: "",
    namespace,
    description: "",
    agentType: "Declarative",
    systemPrompt: isEditMode ? "" : defaultSystemPrompt,
    selectedModel: null,
    selectedMemoryModel: null,
    memoryTtlDays: "",
    selectedTools: [],
    skillRefs: [""],
    skillGitRepos: [newEmptyGitSkillRow()],
    skillsGitAuthSecretName: "",
    skillS3Repos: [newEmptyS3SkillRow()],
    byoImage: "",
    byoCmd: "",
    byoArgs: "",
    envPairs: [{ name: "", value: "", isSecret: false }],
    stream: false,
    shareTools: false,
    contextConfig: undefined,
    promptSourceRows: [newPromptSourceRow()],
    isSubmitting: false,
    isLoading: isEditMode,
    errors: {},
    substrateWorkerPoolRefName: "",
    substrateSnapshotsLocation: "",
  };
}

export function validateAgentFormData(
  data: Partial<AgentFormData>,
): AgentFormValidationErrors {
  const errors: AgentFormValidationErrors = {};

  if (data.name !== undefined) {
    if (!data.name.trim()) {
      errors.name = "Agent name is required";
    } else if (!isResourceNameValid(data.name)) {
      errors.name = `Agent name can only contain lowercase alphanumeric characters, "-" or ".", and must start and end with an alphanumeric character`;
    }
  }
  if (
    data.namespace !== undefined &&
    data.namespace.trim() &&
    !isResourceNameValid(data.namespace)
  ) {
    errors.namespace = `Agent namespace can only contain lowercase alphanumeric characters, "-" or ".", and must start and end with an alphanumeric character`;
  }

  const type = data.type || "Declarative";
  if (
    data.description !== undefined &&
    !data.description.trim() &&
    type !== "AgentHarness"
  ) {
    errors.description = "Description is required";
  }

  if (type === "AgentHarness") {
    if (!data.modelName?.trim()) {
      errors.model = "Please select a model config";
    }
    if (data.agentHarness !== undefined && data.modelName?.trim()) {
      const harnessError = validateAgentHarnessForm({
        harness: data.agentHarness,
        modelRef: data.modelName,
      });
      if (harnessError) {
        errors.agentHarness = harnessError;
      }
    }
    return errors;
  }

  if (formUsesDeclarativeSections(type)) {
    if (data.systemPrompt !== undefined && !data.systemPrompt.trim()) {
      errors.systemPrompt = "Agent instructions are required";
    }
    if (!data.modelName?.trim()) {
      errors.model = "Please select a model";
    }
    if (data.memory) {
      if (!data.memory.modelConfig?.trim()) {
        errors.memoryModel = "Please select an embedding model";
      }
      if (data.memory.ttlDays !== undefined && data.memory.ttlDays < 1) {
        errors.memoryTtl = "TTL must be at least 1 day";
      }
    }
  } else if (formUsesByoSections(type) && !data.byoImage?.trim()) {
    errors.model = "Container image is required";
  }

  if (formUsesDeclarativeSections(type)) {
    const sources = (data.promptSources || []).filter((source) =>
      source.name.trim(),
    );
    for (const source of sources) {
      if (!isResourceNameValid(source.name.trim())) {
        errors.promptSources = `Prompt library name is invalid: ${source.name}`;
        break;
      }
      const alias = source.alias.trim();
      if (alias && !isResourceNameValid(alias)) {
        errors.promptSources = `Alias is invalid: ${source.alias}`;
        break;
      }
    }
  }

  return errors;
}

export function agentResponseToFormState(
  agentResponse: AgentResponse,
): Partial<AgentFormFields> {
  const agent = agentResponse.agent;
  const base: Partial<AgentFormFields> = {
    name: agent.metadata.name || "",
    namespace: agent.metadata.namespace || "",
    description: agent.spec.description || "",
    agentType: formWorkloadKindFromApi(agent.spec.type),
    ...sandboxFieldsFromApiSpec(agent.spec.substrate),
  };

  if (agent.spec.type === "Declarative") {
    const declarative = agent.spec.declarative;
    const memory = declarative?.memory;
    const memoryModelRef = memory?.modelConfig
      ? qualifiedResourceRef(agent.metadata.namespace, memory.modelConfig)
      : "";
    const promptSourceRows = declarative?.promptTemplate?.dataSources?.map(
      (source) => ({
        ...newPromptSourceRow(),
        name: source.name || "",
        alias: source.alias || "",
      }),
    ) ?? [newPromptSourceRow()];

    return {
      ...base,
      systemPrompt: declarative?.systemMessage || "",
      promptSourceRows:
        promptSourceRows.length > 0 ? promptSourceRows : [newPromptSourceRow()],
      selectedTools:
        declarative?.tools && agentResponse.tools ? agentResponse.tools : [],
      selectedModel: agentResponse.modelConfigRef
        ? {
            ref: agentResponse.modelConfigRef,
            spec: {
              model: agentResponse.model || "",
              provider: agentResponse.modelProvider || "",
            },
          }
        : null,
      skillRefs: agent.spec.skills?.refs?.length
        ? agent.spec.skills.refs
        : [""],
      skillGitRepos: agent.spec.skills?.gitRefs?.length
        ? agent.spec.skills.gitRefs.map(gitRepoToFormRow)
        : [newEmptyGitSkillRow()],
      skillsGitAuthSecretName: agent.spec.skills?.gitAuthSecretRef?.name || "",
      skillS3Repos: agent.spec.skills?.s3Refs?.length
        ? agent.spec.skills.s3Refs.map(s3RefToFormRow)
        : [newEmptyS3SkillRow()],
      stream: declarative?.stream ?? false,
      shareTools: declarative?.shareTools ?? false,
      selectedMemoryModel: memoryModelRef
        ? {
            ref: memoryModelRef,
            spec: { model: memory?.modelConfig || "", provider: "" },
          }
        : null,
      memoryTtlDays: memory?.ttlDays ? String(memory.ttlDays) : "",
      contextConfig: declarative?.context,
      byoImage: "",
      byoCmd: "",
      byoArgs: "",
    };
  }

  const byo = agent.spec.byo;
  const envPairs =
    byo?.env?.map<AgentFormEnvRow>((env) =>
      env.valueFrom?.secretKeyRef
        ? {
            name: env.name || "",
            isSecret: true,
            secretName: env.valueFrom.secretKeyRef.name || "",
            secretKey: env.valueFrom.secretKeyRef.key || "",
            optional: env.valueFrom.secretKeyRef.optional,
          }
        : { name: env.name || "", value: env.value || "", isSecret: false },
    ) ?? [];

  return {
    ...base,
    systemPrompt: "",
    selectedModel: null,
    selectedTools: [],
    selectedMemoryModel: null,
    memoryTtlDays: "",
    byoImage: byo?.image || "",
    byoCmd: byo?.cmd || "",
    byoArgs: (byo?.args || []).join(" "),
    envPairs:
      envPairs.length > 0
        ? envPairs
        : [{ name: "", value: "", isSecret: false }],
  };
}

function formEnvRowsToEnvVars(rows: AgentFormEnvRow[]): EnvVar[] {
  return rows.flatMap<EnvVar>((row) => {
    const name = row.name.trim();
    if (!name) {
      return [];
    }
    if (!row.isSecret) {
      return [{ name, value: row.value ?? "" }];
    }
    const secretName = row.secretName?.trim();
    const secretKey = row.secretKey?.trim();
    if (!secretName || !secretKey) {
      return [];
    }
    return [
      {
        name,
        valueFrom: {
          secretKeyRef: {
            name: secretName,
            key: secretKey,
            optional: row.optional,
          },
        },
      },
    ];
  });
}

export function agentFormStateToData(
  state: AgentFormFields,
): AgentWorkloadFormData {
  const declarative = formUsesDeclarativeSections(state.agentType);
  const memoryEnabled = !!(
    state.selectedMemoryModel?.ref || state.memoryTtlDays
  );

  return {
    name: state.name,
    namespace: state.namespace,
    description: state.description,
    type: state.agentType,
    systemPrompt: state.systemPrompt,
    promptSources: state.promptSourceRows.map(({ name, alias }) => ({
      name,
      alias,
    })),
    modelName: state.selectedModel?.ref || "",
    stream: state.stream,
    shareTools: declarative ? state.shareTools : undefined,
    tools: state.selectedTools,
    memory:
      declarative && memoryEnabled
        ? {
            modelConfig: state.selectedMemoryModel?.ref || "",
            ttlDays: state.memoryTtlDays
              ? Number.parseInt(state.memoryTtlDays, 10)
              : undefined,
          }
        : undefined,
    context: declarative ? state.contextConfig : undefined,
    byoImage: state.byoImage,
    byoCmd: state.byoCmd || undefined,
    byoArgs: state.byoArgs
      ? state.byoArgs.split(/\s+/).filter(Boolean)
      : undefined,
    env: formEnvRowsToEnvVars(state.envPairs),
    substrateWorkerPoolRefName: state.substrateWorkerPoolRefName,
    substrateSnapshotsLocation: state.substrateSnapshotsLocation,
  };
}

export function validateAgentFormState(
  state: AgentFormFields,
): AgentFormValidationErrors {
  const data = agentFormStateToData(state);
  const errors = validateAgentFormData(data);

  if (state.agentType === "BYO" && !state.byoCmd.trim()) {
    errors.byoCmd = "Command is required for BYO agents on Agent Substrate";
  }
  return errors;
}

function resourceNameFromRef(ref: string | undefined): string {
  if (!ref) {
    return "";
  }
  return k8sRefUtils.isValidRef(ref) ? k8sRefUtils.fromRef(ref).name : ref;
}

function qualifiedResourceRef(namespace: string | undefined, ref: string): string {
  if (k8sRefUtils.isValidRef(ref)) {
    return ref;
  }
  return k8sRefUtils.toRef(namespace || "default", ref);
}

function resolveNamespacedRef(
  ref: string,
  explicitNamespace: string | undefined,
  fallbackNamespace: string,
): { name: string; namespace: string } {
  const parsed = k8sRefUtils.isValidRef(ref)
    ? k8sRefUtils.fromRef(ref)
    : { name: ref, namespace: "" };
  return {
    name: parsed.name,
    namespace: explicitNamespace || parsed.namespace || fallbackNamespace,
  };
}

function toolsFromForm(tools: Tool[], namespace: string): Tool[] {
  return tools.map((tool) => {
    if (isMcpTool(tool)) {
      if (!tool.mcpServer) {
        throw new Error("MCP server not found");
      }
      const server = tool.mcpServer;
      const serverRef = resolveNamespacedRef(
        server.name,
        server.namespace,
        namespace,
      );
      const requireApproval = server.requireApproval?.length
        ? server.requireApproval
        : undefined;
      return {
        type: "McpServer",
        mcpServer: {
          name: serverRef.name,
          namespace: serverRef.namespace,
          kind: server.kind,
          apiGroup: server.apiGroup,
          toolNames: server.toolNames,
          ...(requireApproval ? { requireApproval } : {}),
        },
      };
    }

    if (tool.type === "Agent") {
      if (!tool.agent) {
        throw new Error("Agent not found");
      }
      const agentRef = resolveNamespacedRef(
        tool.agent.name,
        tool.agent.namespace,
        namespace,
      );
      return {
        type: "Agent",
        agent: {
          name: agentRef.name,
          namespace: agentRef.namespace,
          kind: tool.agent.kind || "SandboxAgent",
          apiGroup: tool.agent.apiGroup || "kagent.dev",
        },
        ...(tool.isolateSessions ? { isolateSessions: true } : {}),
      };
    }

    return tool;
  });
}

function attachPromptTemplate(
  declarative: DeclarativeAgentSpec,
  data: AgentWorkloadFormData,
): void {
  const dataSources: PromptSource[] = (data.promptSources || []).flatMap(
    (source) => {
      const name = source.name.trim();
      if (!name) {
        return [];
      }
      const alias = source.alias.trim();
      return [
        {
          kind: "ConfigMap",
          name,
          apiGroup: "",
          ...(alias ? { alias } : {}),
        },
      ];
    },
  );
  if (dataSources.length > 0) {
    declarative.promptTemplate = { dataSources };
  }
}

function declarativeSpecFromForm(
  data: AgentWorkloadFormData,
): DeclarativeAgentSpec {
  const declarative: DeclarativeAgentSpec = {
    systemMessage: data.systemPrompt || "",
    modelConfig: resourceNameFromRef(data.modelName),
    stream: data.stream ?? true,
    tools: toolsFromForm(data.tools || [], data.namespace || ""),
  };

  if (data.memory?.modelConfig) {
    declarative.memory = {
      modelConfig: resourceNameFromRef(data.memory.modelConfig),
      ttlDays: data.memory.ttlDays,
    };
  }
  if (data.context) {
    declarative.context = data.context;
  }
  if (data.shareTools) {
    declarative.shareTools = true;
  }
  attachPromptTemplate(declarative, data);
  return declarative;
}

function byoSpecFromForm(data: AgentWorkloadFormData) {
  return {
    image: data.byoImage || "",
    cmd: data.byoCmd,
    args: data.byoArgs,
    env: data.env,
  };
}

function agentSpecFromForm(data: AgentWorkloadFormData): AgentSpec {
  const type = data.type || "Declarative";
  const spec: AgentSpec = {
    type,
    description: data.description,
  };

  switch (type) {
    case "Declarative":
      spec.declarative = declarativeSpecFromForm(data);
      break;
    case "BYO":
      spec.byo = byoSpecFromForm(data);
      break;
  }

  spec.substrate = buildSandboxSubstrateFromForm(data);

  return spec;
}

export function agentFormDataToSandboxAgent(
  data: AgentWorkloadFormData,
): SandboxAgent {
  const spec = agentSpecFromForm(data);

  return {
    apiVersion: "kagent.dev/v1alpha3",
    kind: "SandboxAgent",
    metadata: { name: data.name, namespace: data.namespace || "" },
    spec,
  };
}
