# KAgent ModelConfig 调用 LLM 完整流程详解

本文档详细说明 KAgent 如何使用 ModelConfig 中的配置来调用 LLM 模型。

## 📋 目录

1. [整体架构](#整体架构)
2. [详细流程](#详细流程)
3. [关键组件](#关键组件)
4. [SAP AI Core 示例](#sap-ai-core-示例)
5. [数据结构映射](#数据结构映射)

---

## 整体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                     KAgent 架构层次                                  │
└─────────────────────────────────────────────────────────────────────┘

用户请求
   ↓
┌──────────────────────────────────────┐
│ 1. Kubernetes 资源层                  │
│    - Agent CRD                       │
│    - ModelConfig CRD                 │
│    - Secret (API Keys)               │
└──────────────────────────────────────┘
   ↓
┌──────────────────────────────────────┐
│ 2. Controller 层 (Go)                │
│    - Agent Controller                │
│    - ModelConfig Controller          │
│    - Translator (adk_api_translator) │
└──────────────────────────────────────┘
   ↓
┌──────────────────────────────────────┐
│ 3. 配置生成层 (Go)                   │
│    - AgentConfig JSON                │
│    - AgentCard JSON                  │
│    - Kubernetes Deployment           │
│    - Secret with config files        │
└──────────────────────────────────────┘
   ↓
┌──────────────────────────────────────┐
│ 4. Runtime 层 (Python)               │
│    - kagent-adk CLI                  │
│    - AgentConfig loader              │
│    - Model instantiation             │
└──────────────────────────────────────┘
   ↓
┌──────────────────────────────────────┐
│ 5. LLM 调用层 (Python)               │
│    - Model adapters (_openai.py)    │
│    - API clients                     │
│    - LLM providers                   │
└──────────────────────────────────────┘
```

---

## 详细流程

### 阶段 1: 用户创建 Kubernetes 资源

```yaml
# 1. 用户创建 Secret (存储 API Key)
apiVersion: v1
kind: Secret
metadata:
  name: kagent-sap-aicore
  namespace: kagent
type: Opaque
stringData:
  SAP_AI_CORE_API_KEY: "your-api-key"
  CLIENT_SECRET: "your-client-secret"

---
# 2. 用户创建 ModelConfig
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: sap-aicore-gpt4
  namespace: kagent
spec:
  provider: SAPAICore
  model: "gpt-4"
  apiKeySecret: kagent-sap-aicore
  apiKeySecretKey: SAP_AI_CORE_API_KEY
  sapAICore:
    baseUrl: "https://api.ai.prod.eu-central-1.aws.ml.hana.ondemand.com"
    resourceGroup: "default"
    deploymentId: "d1234567890"
    temperature: "0.7"
    maxTokens: 2048

---
# 3. 用户创建 Agent
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
  namespace: kagent
spec:
  type: Declarative
  description: "My AI agent"
  declarative:
    modelConfig: sap-aicore-gpt4  # 引用 ModelConfig
    systemMessage: "You are a helpful assistant"
    tools: []
```

### 阶段 2: Controller 处理 (Go)

**文件**: `go/internal/controller/agent_controller.go`

```go
// Agent Controller 监听 Agent 资源的变化
func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) {
    // 1. 获取 Agent 资源
    agent := &v1alpha2.Agent{}
    r.Get(ctx, req.NamespacedName, agent)
    
    // 2. 调用 Translator 转换
    outputs, err := r.translator.TranslateAgent(ctx, agent)
    
    // 3. 应用生成的 Kubernetes 资源
    // - Deployment
    // - Service
    // - Secret (包含 config.json)
    // - ServiceAccount
}
```

### 阶段 3: Translator 转换 ModelConfig (Go)

**文件**: `go/internal/controller/translator/agent/adk_api_translator.go`

```go
// translateModel 函数负责将 ModelConfig 转换为运行时配置
func (a *adkApiTranslator) translateModel(ctx context.Context, namespace, modelConfig string) (adk.Model, *modelDeploymentData, error) {
    // 1. 读取 ModelConfig CRD
    model := &v1alpha2.ModelConfig{}
    a.kube.Get(ctx, types.NamespacedName{
        Namespace: namespace, 
        Name: modelConfig
    }, model)
    
    // 2. 根据 Provider 类型进行不同的处理
    switch model.Spec.Provider {
    case v1alpha2.ModelProviderSAPAICore:
        // 3. 创建环境变量映射 (从 Secret 中读取)
        modelDeploymentData.EnvVars = append(modelDeploymentData.EnvVars, 
            corev1.EnvVar{
                Name: "SAP_AI_CORE_API_KEY",
                ValueFrom: &corev1.EnvVarSource{
                    SecretKeyRef: &corev1.SecretKeySelector{
                        LocalObjectReference: corev1.LocalObjectReference{
                            Name: model.Spec.APIKeySecret,
                        },
                        Key: model.Spec.APIKeySecretKey,
                    },
                },
            })
        
        // 4. 构建 ADK Model 配置 (将被序列化为 JSON)
        sapAICore := &adk.SAPAICore{
            BaseModel: adk.BaseModel{
                Model:   model.Spec.Model,
                Headers: model.Spec.DefaultHeaders,
            },
            BaseUrl:          model.Spec.SAPAICore.BaseURL,
            ResourceGroup:    model.Spec.SAPAICore.ResourceGroup,
            DeploymentID:     model.Spec.SAPAICore.DeploymentID,
            AuthUrl:          model.Spec.SAPAICore.AuthURL,
            ClientID:         model.Spec.SAPAICore.ClientID,
            Temperature:      model.Spec.SAPAICore.Temperature,
            MaxTokens:        model.Spec.SAPAICore.MaxTokens,
            TopP:             model.Spec.SAPAICore.TopP,
            TopK:             model.Spec.SAPAICore.TopK,
            FrequencyPenalty: model.Spec.SAPAICore.FrequencyPenalty,
            PresencePenalty:  model.Spec.SAPAICore.PresencePenalty,
        }
        return sapAICore, modelDeploymentData, nil
    }
}
```

### 阶段 4: 生成配置文件 (Go)

**文件**: `go/internal/controller/translator/agent/adk_api_translator.go`

```go
func (a *adkApiTranslator) buildManifest(...) (*AgentOutputs, error) {
    // 1. 构建 AgentConfig (包含 Model 配置)
    cfg := &adk.AgentConfig{
        Description: agent.Spec.Description,
        Instruction: systemMessage,
        Model:       model,  // 包含 SAP AI Core 配置
        HttpTools:   [...],
        SseTools:    [...],
    }
    
    // 2. 序列化为 JSON
    bCfg, _ := json.Marshal(cfg)
    bCard, _ := json.Marshal(card)
    
    // 3. 创建 Kubernetes Secret (存储配置文件)
    outputs.Manifest = append(outputs.Manifest, &corev1.Secret{
        ObjectMeta: objMeta(),
        StringData: map[string]string{
            "config.json":     string(bCfg),      // Agent 配置
            "agent-card.json": string(bCard),     // Agent 元数据
        },
    })
    
    // 4. 创建 Deployment (挂载 Secret)
    deployment := &appsv1.Deployment{
        Spec: appsv1.DeploymentSpec{
            Template: corev1.PodTemplateSpec{
                Spec: corev1.PodSpec{
                    Containers: []corev1.Container{{
                        Name:  "kagent",
                        Image: dep.Image,
                        Env:   env,  // 包含 SAP_AI_CORE_API_KEY 等环境变量
                        VolumeMounts: []corev1.VolumeMount{{
                            Name:      "config",
                            MountPath: "/config",  // 挂载配置文件
                        }},
                        Command: []string{"kagent-adk"},
                        Args: []string{
                            "static",
                            "--filepath", "/config",  // 指向配置文件路径
                        },
                    }},
                    Volumes: []corev1.Volume{{
                        Name: "config",
                        VolumeSource: corev1.VolumeSource{
                            Secret: &corev1.SecretVolumeSource{
                                SecretName: agent.Name,  // 引用上面创建的 Secret
                            },
                        },
                    }},
                },
            },
        },
    }
}
```

**生成的 config.json 示例**:
```json
{
  "model": {
    "type": "sap_ai_core",
    "model": "gpt-4",
    "base_url": "https://api.ai.prod.eu-central-1.aws.ml.hana.ondemand.com",
    "resource_group": "default",
    "deployment_id": "d1234567890",
    "auth_url": "",
    "client_id": "",
    "temperature": "0.7",
    "max_tokens": 2048,
    "headers": {}
  },
  "description": "My AI agent",
  "instruction": "You are a helpful assistant",
  "http_tools": [],
  "sse_tools": [],
  "remote_agents": []
}
```

### 阶段 5: Python Runtime 加载配置

**文件**: `python/packages/kagent-adk/src/kagent/adk/cli.py`

```python
@app.command()
def static(
    host: str = "127.0.0.1",
    port: int = 8080,
    filepath: str = "/config",  # 配置文件路径
):
    # 1. 读取 config.json
    with open(os.path.join(filepath, "config.json"), "r") as f:
        config = json.load(f)
    
    # 2. 验证并解析配置
    agent_config = AgentConfig.model_validate(config)
    
    # 3. 读取 agent-card.json
    with open(os.path.join(filepath, "agent-card.json"), "r") as f:
        agent_card = json.load(f)
    agent_card = AgentCard.model_validate(agent_card)
    
    # 4. 转换为 Agent 实例
    root_agent = agent_config.to_agent(app_cfg.name)
    
    # 5. 启动 HTTP 服务器
    kagent_app = KAgentApp(root_agent, agent_card, app_cfg.url, app_cfg.app_name)
    server = kagent_app.build()
    uvicorn.run(server, host=host, port=port)
```

### 阶段 6: 实例化 Model (Python)

**文件**: `python/packages/kagent-adk/src/kagent/adk/types.py`

```python
class AgentConfig(BaseModel):
    model: Union[OpenAI, Anthropic, ..., SAPAICore] = Field(
        discriminator="type"
    )
    
    def to_agent(self, name: str) -> Agent:
        extra_headers = self.model.headers or {}
        
        # 根据 model.type 实例化不同的 Model
        if self.model.type == "sap_ai_core":
            # 实例化 SAP AI Core Model
            model = SAPAICoreNative(
                type="sap_ai_core",
                model=self.model.model,
                base_url=self.model.base_url,
                resource_group=self.model.resource_group,
                deployment_id=self.model.deployment_id,
                auth_url=self.model.auth_url,
                client_id=self.model.client_id,
                default_headers=extra_headers,
                temperature=self.model.temperature,
                max_tokens=self.model.max_tokens,
                top_p=self.model.top_p,
                top_k=self.model.top_k,
                frequency_penalty=self.model.frequency_penalty,
                presence_penalty=self.model.presence_penalty,
                timeout=self.model.timeout,
            )
        
        # 创建 Agent (包含 model 实例)
        return Agent(
            name=name,
            model=model,  # Model 实例
            description=self.description,
            instruction=self.instruction,
            tools=tools,
        )
```

### 阶段 7: LLM 调用 (Python)

**文件**: `python/packages/kagent-adk/src/kagent/adk/models/_sap_ai_core.py`

```python
class SAPAICore(BaseLlm):
    # 配置属性 (从 config.json 加载)
    model: str
    base_url: str
    resource_group: str
    deployment_id: str
    temperature: Optional[str] = None
    max_tokens: Optional[int] = None
    # ... 其他参数
    
    @cached_property
    def _client(self) -> httpx.AsyncClient:
        """创建 HTTP 客户端"""
        # 从环境变量读取 API Key (通过 Secret 注入)
        api_key = self.api_key or os.environ.get("SAP_AI_CORE_API_KEY")
        
        headers = self.default_headers.copy() if self.default_headers else {}
        headers["Authorization"] = f"Bearer {api_key}"
        headers["AI-Resource-Group"] = self.resource_group
        
        return httpx.AsyncClient(
            base_url=self.base_url,
            headers=headers,
            timeout=self.timeout,
        )
    
    async def generate_content_async(
        self, llm_request: LlmRequest, stream: bool = False
    ) -> AsyncGenerator[LlmResponse, None]:
        """生成内容"""
        # 1. 转换消息格式
        messages = self._convert_content_to_messages(
            llm_request.contents, 
            system_instruction
        )
        
        # 2. 构建请求 payload
        payload = {
            "messages": messages,
            "model": llm_request.model or self.model,
        }
        
        # 添加可选参数
        if self.temperature is not None:
            payload["temperature"] = float(self.temperature)
        if self.max_tokens is not None:
            payload["max_tokens"] = self.max_tokens
        
        # 3. 调用 SAP AI Core API
        endpoint = f"/v2/inference/deployments/{self.deployment_id}/chat/completions"
        response = await self._client.post(endpoint, json=payload)
        
        # 4. 转换响应
        response_data = response.json()
        yield self._convert_response_to_llm_response(response_data)
```

---

## 关键组件

### 1. **CRD 定义** (Kubernetes)

**文件**: `go/api/v1alpha2/modelconfig_types.go`

```go
// ModelProvider 枚举
type ModelProvider string

const (
    ModelProviderSAPAICore ModelProvider = "SAPAICore"
    // ... 其他 providers
)

// SAP AI Core 特定配置
type SAPAICoreConfig struct {
    BaseURL       string  `json:"baseUrl"`
    ResourceGroup string  `json:"resourceGroup"`
    DeploymentID  string  `json:"deploymentId"`
    AuthURL       string  `json:"authUrl,omitempty"`
    ClientID      string  `json:"clientId,omitempty"`
    Temperature   *string `json:"temperature,omitempty"`
    MaxTokens     *int    `json:"maxTokens,omitempty"`
    // ... 其他参数
}

// ModelConfig Spec
type ModelConfigSpec struct {
    Model           string          `json:"model"`
    Provider        ModelProvider   `json:"provider"`
    APIKeySecret    string          `json:"apiKeySecret"`
    APIKeySecretKey string          `json:"apiKeySecretKey"`
    SAPAICore       *SAPAICoreConfig `json:"sapAICore,omitempty"`
    // ... 其他 providers
}
```

### 2. **Go 数据结构** (内部表示)

**文件**: `go/internal/adk/types.go`

```go
// Go 侧的 Model 接口
type Model interface {
    GetType() string
}

// SAP AI Core 模型配置
type SAPAICore struct {
    BaseModel
    BaseUrl          string  `json:"base_url"`
    ResourceGroup    string  `json:"resource_group"`
    DeploymentID     string  `json:"deployment_id"`
    AuthUrl          string  `json:"auth_url,omitempty"`
    ClientID         string  `json:"client_id,omitempty"`
    Temperature      *string `json:"temperature,omitempty"`
    MaxTokens        *int    `json:"max_tokens,omitempty"`
    // ... 其他参数
}

// Agent 配置 (包含 Model)
type AgentConfig struct {
    Model        Model                 `json:"model"`
    Description  string                `json:"description"`
    Instruction  string                `json:"instruction"`
    HttpTools    []HttpMcpServerConfig `json:"http_tools"`
    SseTools     []SseMcpServerConfig  `json:"sse_tools"`
    RemoteAgents []RemoteAgentConfig   `json:"remote_agents"`
}
```

### 3. **Python 数据结构** (Runtime)

**文件**: `python/packages/kagent-adk/src/kagent/adk/types.py`

```python
# Python 侧的 Model 配置类
class SAPAICore(BaseLLM):
    base_url: str
    resource_group: str
    deployment_id: str
    auth_url: str | None = None
    client_id: str | None = None
    temperature: str | None = None
    max_tokens: int | None = None
    # ... 其他参数
    
    type: Literal["sap_ai_core"]

# Agent 配置
class AgentConfig(BaseModel):
    model: Union[OpenAI, Anthropic, ..., SAPAICore] = Field(
        discriminator="type"
    )
    description: str
    instruction: str
    http_tools: list[HttpMcpServerConfig] | None = None
    sse_tools: list[SseMcpServerConfig] | None = None
    remote_agents: list[RemoteAgentConfig] | None = None
```

### 4. **Python Model 实现**

**文件**: `python/packages/kagent-adk/src/kagent/adk/models/_sap_ai_core.py`

```python
class SAPAICore(BaseLlm):
    """SAP AI Core 模型适配器"""
    
    # 从 config.json 加载的配置
    model: str
    base_url: str
    resource_group: str
    deployment_id: str
    # ... 其他配置
    
    # API Key 从环境变量读取 (Secret 注入)
    api_key: Optional[str] = Field(default=None, exclude=True)
    
    @cached_property
    def _client(self) -> httpx.AsyncClient:
        """创建 HTTP 客户端 (带认证)"""
        pass
    
    async def generate_content_async(self, llm_request, stream=False):
        """调用 SAP AI Core API"""
        pass
```

---

## SAP AI Core 示例

### 完整示例：从 YAML 到 LLM 调用

**1. 用户创建资源**:
```yaml
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: sap-gpt4
spec:
  provider: SAPAICore
  model: "gpt-4"
  apiKeySecret: sap-secret
  apiKeySecretKey: API_KEY
  sapAICore:
    baseUrl: "https://api.ai.sap.com"
    resourceGroup: "default"
    deploymentId: "d123"
    temperature: "0.7"
    maxTokens: 2048
```

**2. Controller 生成 config.json**:
```json
{
  "model": {
    "type": "sap_ai_core",
    "model": "gpt-4",
    "base_url": "https://api.ai.sap.com",
    "resource_group": "default",
    "deployment_id": "d123",
    "temperature": "0.7",
    "max_tokens": 2048
  },
  "description": "My agent",
  "instruction": "You are helpful"
}
```

**3. Python 加载并调用**:
```python
# 1. 加载配置
agent_config = AgentConfig.model_validate(config)

# 2. 实例化 Model
model = SAPAICoreNative(
    type="sap_ai_core",
    model="gpt-4",
    base_url="https://api.ai.sap.com",
    resource_group="default",
    deployment_id="d123",
    temperature="0.7",
    max_tokens=2048,
)

# 3. 创建 Agent
agent = Agent(model=model, ...)

# 4. 调用 LLM
response = await model.generate_content_async(request)
```

---

## 数据结构映射

### Kubernetes → Go → JSON → Python

```
┌─────────────────────────────────────────────────────────────────┐
│ Kubernetes CRD (YAML)                                           │
├─────────────────────────────────────────────────────────────────┤
│ spec:                                                           │
│   provider: SAPAICore                                           │
│   model: "gpt-4"                                                │
│   sapAICore:                                                    │
│     baseUrl: "https://..."                                      │
│     resourceGroup: "default"                                    │
│     deploymentId: "d123"                                        │
└─────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────┐
│ Go Struct (v1alpha2.ModelConfig)                                │
├─────────────────────────────────────────────────────────────────┤
│ type ModelConfigSpec struct {                                   │
│     Provider ModelProvider                                      │
│     Model    string                                             │
│     SAPAICore *SAPAICoreConfig                                  │
│ }                                                               │
└─────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────┐
│ Go ADK Struct (adk.SAPAICore)                                   │
├─────────────────────────────────────────────────────────────────┤
│ type SAPAICore struct {                                         │
│     BaseModel                                                   │
│     BaseUrl       string                                        │
│     ResourceGroup string                                        │
│     DeploymentID  string                                        │
│ }                                                               │
└─────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────┐
│ JSON (config.json in Secret)                                    │
├─────────────────────────────────────────────────────────────────┤
│ {                                                               │
│   "model": {                                                    │
│     "type": "sap_ai_core",                                      │
│     "model": "gpt-4",                                           │
│     "base_url": "https://...",                                  │
│     "resource_group": "default",                                │
│     "deployment_id": "d123"                                     │
│   }                                                             │
│ }                                                               │
└─────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────┐
│ Python Pydantic (types.SAPAICore)                               │
├─────────────────────────────────────────────────────────────────┤
│ class SAPAICore(BaseLLM):                                       │
│     base_url: str                                               │
│     resource_group: str                                         │
│     deployment_id: str                                          │
│     type: Literal["sap_ai_core"]                                │
└─────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────┐
│ Python Model Implementation (models._sap_ai_core.SAPAICore)     │
├─────────────────────────────────────────────────────────────────┤
│ class SAPAICore(BaseLlm):                                       │
│     async def generate_content_async(...):                      │
│         # 调用 SAP AI Core API                                  │
│         response = await self._client.post(...)                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## 环境变量流转

```
┌─────────────────────────────────────────────────────────────────┐
│ Kubernetes Secret                                               │
├─────────────────────────────────────────────────────────────────┤
│ stringData:                                                     │
│   SAP_AI_CORE_API_KEY: "sk-..."                                │
│   CLIENT_SECRET: "secret-..."                                  │
└─────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────┐
│ Deployment Env (Go 生成)                                         │
├─────────────────────────────────────────────────────────────────┤
│ env:                                                            │
│ - name: SAP_AI_CORE_API_KEY                                     │
│   valueFrom:                                                    │
│     secretKeyRef:                                               │
│       name: kagent-sap-aicore                                   │
│       key: SAP_AI_CORE_API_KEY                                  │
└─────────────────────────────────────────────────────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────────────┐
│ Python Runtime (读取环境变量)                                    │
├─────────────────────────────────────────────────────────────────┤
│ api_key = os.environ.get("SAP_AI_CORE_API_KEY")                │
└─────────────────────────────────────────────────────────────────┘
```

---

## 总结

KAgent 的 ModelConfig 调用 LLM 的完整流程涉及：

1. **用户定义** Kubernetes 资源 (Agent + ModelConfig + Secret)
2. **Controller** 监听资源变化并调用 Translator
3. **Translator** 将 ModelConfig 转换为 ADK 格式并生成配置文件
4. **Controller** 创建 Deployment 和 Secret (包含 config.json)
5. **Python Runtime** 启动时加载 config.json
6. **AgentConfig** 根据 model.type 实例化对应的 Model
7. **Model 实例** 在运行时调用对应的 LLM Provider API

整个流程实现了 **声明式配置 → 运行时实例化 → API 调用** 的完整链路。
