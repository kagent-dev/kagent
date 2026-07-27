import {
  formRowsToGitRepos,
  gitRepoToFormRow,
  newEmptyGitSkillRow,
  validateDeclarativeAgentSkills,
  type GitSkillFormRow,
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
  Agent,
  AgentSpec,
  ContextConfig,
  DeclarativeRuntime,
  EnvVar,
  GitRepo,
  ModelConfig,
  PromptSource,
  SandboxAgent,
  SkillForAgent,
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
  serviceAccountName?: string;
  promptSources?: string;
  byoCmd?: string;
  agentHarness?: AgentHarnessFormValidationError;
}

export interface AgentFormData {
  name: string;
  namespace: string;
  description: string;
  type?: AgentType;
  runInSandbox?: boolean;
  declarativeRuntime?: DeclarativeRuntime;
  systemPrompt?: string;
  modelName?: string;
  tools: Tool[];
  stream?: boolean;
  skillRefs?: string[];
  skillGitRepos?: GitRepo[];
  skillsGitAuthSecretName?: string;
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
  replicas?: number;
  imagePullSecrets?: Array<{ name: string }>;
  volumes?: unknown[];
  volumeMounts?: unknown[];
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  env?: EnvVar[];
  imagePullPolicy?: string;
  serviceAccountName?: string;
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
  runInSandbox: boolean;
  systemPrompt: string;
  selectedModel: ModelConfig | null;
  selectedMemoryModel: ModelConfig | null;
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
  envPairs: AgentFormEnvRow[];
  stream: boolean;
  shareTools: boolean;
  declarativeRuntime: DeclarativeRuntime;
  contextConfig: ContextConfig | undefined;
  serviceAccountName: string;
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
    runInSandbox: false,
    systemPrompt: isEditMode ? "" : defaultSystemPrompt,
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
    shareTools: false,
    declarativeRuntime: "go",
    contextConfig: undefined,
    serviceAccountName: "",
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

  if (data.serviceAccountName !== undefined) {
    const serviceAccountName = data.serviceAccountName.trim();
    if (serviceAccountName && !isResourceNameValid(serviceAccountName)) {
      errors.serviceAccountName = `Service account name can only contain lowercase alphanumeric characters, "-" or ".", and must start and end with an alphanumeric character`;
    }
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
    runInSandbox: agentResponse.workloadMode === "sandbox",
    ...(agentResponse.workloadMode === "sandbox"
      ? sandboxFieldsFromApiSpec(agent.spec.substrate)
      : {}),
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
      stream: declarative?.stream ?? false,
      shareTools: declarative?.shareTools ?? false,
      declarativeRuntime: declarative?.runtime === "go" ? "go" : "python",
      selectedMemoryModel: memoryModelRef
        ? {
            ref: memoryModelRef,
            spec: { model: memory?.modelConfig || "", provider: "" },
          }
        : null,
      memoryTtlDays: memory?.ttlDays ? String(memory.ttlDays) : "",
      contextConfig: declarative?.context,
      serviceAccountName: declarative?.deployment?.serviceAccountName || "",
      byoImage: "",
      byoCmd: "",
      byoArgs: "",
    };
  }

  const deployment = agent.spec.byo?.deployment;
  const imagePullSecrets =
    deployment?.imagePullSecrets?.map((secret) => secret.name) ?? [];
  const envPairs =
    deployment?.env?.map<AgentFormEnvRow>((env) =>
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
    byoImage: deployment?.image || "",
    byoCmd: deployment?.cmd || "",
    byoArgs: (deployment?.args || []).join(" "),
    replicas:
      deployment?.replicas !== undefined ? String(deployment.replicas) : "",
    imagePullPolicy: deployment?.imagePullPolicy || "",
    imagePullSecrets: imagePullSecrets.length > 0 ? imagePullSecrets : [""],
    envPairs:
      envPairs.length > 0
        ? envPairs
        : [{ name: "", value: "", isSecret: false }],
    serviceAccountName: deployment?.serviceAccountName || "",
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
  const skillsEnabled = declarative && !state.runInSandbox;
  const memoryEnabled = !!(
    state.selectedMemoryModel?.ref || state.memoryTtlDays
  );

  return {
    name: state.name,
    namespace: state.namespace,
    description: state.description,
    type: state.agentType,
    runInSandbox: state.runInSandbox,
    systemPrompt: state.systemPrompt,
    promptSources: state.promptSourceRows.map(({ name, alias }) => ({
      name,
      alias,
    })),
    modelName: state.selectedModel?.ref || "",
    stream: state.stream,
    shareTools: declarative ? state.shareTools : undefined,
    tools: state.selectedTools,
    skillRefs: skillsEnabled
      ? state.skillRefs.filter((ref) => ref.trim())
      : undefined,
    skillGitRepos: skillsEnabled
      ? formRowsToGitRepos(state.skillGitRepos)
      : undefined,
    skillsGitAuthSecretName:
      skillsEnabled && state.skillsGitAuthSecretName.trim()
        ? state.skillsGitAuthSecretName.trim()
        : undefined,
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
    declarativeRuntime: declarative ? state.declarativeRuntime : undefined,
    byoImage: state.byoImage,
    byoCmd: state.byoCmd || undefined,
    byoArgs: state.byoArgs
      ? state.byoArgs.split(/\s+/).filter(Boolean)
      : undefined,
    replicas: state.replicas ? Number.parseInt(state.replicas, 10) : undefined,
    imagePullPolicy: state.imagePullPolicy || undefined,
    imagePullSecrets: state.imagePullSecrets
      .filter((name) => name.trim())
      .map((name) => ({ name: name.trim() })),
    env: formEnvRowsToEnvVars(state.envPairs),
    serviceAccountName: state.serviceAccountName.trim() || undefined,
    ...(state.runInSandbox
      ? {
          substrateWorkerPoolRefName: state.substrateWorkerPoolRefName,
          substrateSnapshotsLocation: state.substrateSnapshotsLocation,
        }
      : {}),
  };
}

export function validateAgentFormState(
  state: AgentFormFields,
): AgentFormValidationErrors {
  const data = agentFormStateToData(state);
  const errors = validateAgentFormData(data);

  if (state.agentType === "BYO" && state.runInSandbox && !state.byoCmd.trim()) {
    errors.byoCmd = "Command is required for BYO agents on Agent Substrate";
  }
  if (formUsesDeclarativeSections(state.agentType) && !state.runInSandbox) {
    const skillsError = validateDeclarativeAgentSkills({
      skillRefs: state.skillRefs,
      skillGitRepos: state.skillGitRepos,
      skillsGitAuthSecretName: state.skillsGitAuthSecretName,
    });
    if (skillsError) {
      errors.skills = skillsError;
    }
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

function declarativeRuntimeFromForm(
  data: AgentWorkloadFormData,
): DeclarativeRuntime {
  return data.declarativeRuntime === "python" ? "python" : "go";
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
          kind: tool.agent.kind || "Agent",
          apiGroup: tool.agent.apiGroup || "kagent.dev",
        },
      };
    }

    return tool;
  });
}

function skillsFromForm(
  data: AgentWorkloadFormData,
): SkillForAgent | undefined {
  const refs = (data.skillRefs || []).map((ref) => ref.trim()).filter(Boolean);
  const gitRefs = formRowsToGitRepos(
    (data.skillGitRepos || []).map((repo) => ({
      url: repo.url ?? "",
      ref: repo.ref ?? "",
      path: repo.path ?? "",
      name: repo.name ?? "",
    })),
  );
  if (refs.length === 0 && gitRefs.length === 0) {
    return undefined;
  }

  const skills: SkillForAgent = {};
  if (refs.length > 0) {
    skills.refs = refs;
  }
  if (gitRefs.length > 0) {
    skills.gitRefs = gitRefs;
    const secretName = data.skillsGitAuthSecretName?.trim();
    if (secretName) {
      skills.gitAuthSecretRef = { name: secretName };
    }
  }
  return skills;
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
    runtime: declarativeRuntimeFromForm(data),
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
  const serviceAccountName = data.serviceAccountName?.trim();
  if (serviceAccountName) {
    declarative.deployment = { serviceAccountName };
  }
  attachPromptTemplate(declarative, data);
  return declarative;
}

function byoSpecFromForm(data: AgentWorkloadFormData) {
  return {
    deployment: {
      image: data.byoImage || "",
      cmd: data.byoCmd,
      args: data.byoArgs,
      replicas: data.replicas,
      imagePullSecrets: data.imagePullSecrets,
      volumes: data.volumes,
      volumeMounts: data.volumeMounts,
      labels: data.labels,
      annotations: data.annotations,
      env: data.env,
      imagePullPolicy: data.imagePullPolicy,
      serviceAccountName: data.serviceAccountName,
    },
  };
}

function agentSpecFromForm(
  data: AgentWorkloadFormData,
  sandbox: boolean,
): AgentSpec {
  const type = data.type || "Declarative";
  const spec: AgentSpec = {
    type,
    description: data.description,
  };

  switch (type) {
    case "Declarative":
      spec.declarative = declarativeSpecFromForm(data);
      if (!sandbox) {
        spec.skills = skillsFromForm(data);
      }
      break;
    case "BYO":
      spec.byo = byoSpecFromForm(data);
      break;
  }

  if (sandbox) {
    spec.substrate = buildSandboxSubstrateFromForm({
      ...data,
      runInSandbox: true,
    });
  }

  return spec;
}

export function agentFormDataToAgent(data: AgentWorkloadFormData): Agent {
  const spec = agentSpecFromForm(data, false);

  return {
    metadata: { name: data.name, namespace: data.namespace || "" },
    spec,
  };
}

export function agentFormDataToSandboxAgent(
  data: AgentWorkloadFormData,
): SandboxAgent {
  const spec = agentSpecFromForm(data, true);

  return {
    apiVersion: "kagent.dev/v1alpha2",
    kind: "SandboxAgent",
    metadata: { name: data.name, namespace: data.namespace || "" },
    spec,
  };
}
