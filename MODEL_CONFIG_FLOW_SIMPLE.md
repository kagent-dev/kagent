# KAgent ModelConfig 调用流程简化版

## 🎯 核心流程（5步）

```
用户创建 YAML → Controller 转换 → 生成 config.json → Python 加载 → 调用 LLM
```

---

## 📝 详细步骤

### 步骤 1: 用户创建 Kubernetes 资源

```yaml
# ModelConfig: 定义使用哪个 LLM 以及如何配置
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: sap-gpt4
spec:
  provider: SAPAICore          # 选择 Provider
  model: "gpt-4"               # 模型名称
  apiKeySecret: sap-secret     # API Key 存储位置
  sapAICore:
    baseUrl: "https://..."     # SAP AI Core URL
    deploymentId: "d123"       # 部署 ID
    temperature: "0.7"         # 参数配置

---
# Agent: 使用上面定义的 ModelConfig
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
spec:
  declarative:
    modelConfig: sap-gpt4      # 引用 ModelConfig
    systemMessage: "You are helpful"
```

### 步骤 2: Go Controller 读取并转换

**位置**: `go/internal/controller/translator/agent/adk_api_translator.go` (第 787-836 行)

```go
// 读取 ModelConfig
model := &v1alpha2.ModelConfig{}
kube.Get(ctx, "sap-gpt4", model)

// 转换为 Go 结构
sapAICore := &adk.SAPAICore{
    Model:         "gpt-4",
    BaseUrl:       "https://...",
    DeploymentID:  "d123",
    Temperature:   "0.7",
    // ... 其他参数
}

// 返回用于生成配置
return sapAICore, nil
```

### 步骤 3: Controller 生成配置文件

**位置**: `go/internal/controller/translator/agent/adk_api_translator.go` (第 230-299 行)

```go
// 序列化为 JSON
cfg := &adk.AgentConfig{
    Model:       sapAICore,  // 包含 SAP AI Core 配置
    Description: "My agent",
    Instruction: "You are helpful",
}
configJson, _ := json.Marshal(cfg)

// 创建 Kubernetes Secret (存储配置文件)
secret := &corev1.Secret{
    StringData: map[string]string{
        "config.json": string(configJson),
    },
}

// 创建 Deployment (挂载 Secret 并设置环境变量)
deployment := &appsv1.Deployment{
    Spec: DeploymentSpec{
        Template: PodTemplateSpec{
            Spec: PodSpec{
                Env: []EnvVar{{
                    Name: "SAP_AI_CORE_API_KEY",
                    ValueFrom: &EnvVarSource{
                        SecretKeyRef: &SecretKeySelector{
                            Name: "sap-secret",
                            Key:  "API_KEY",
                        },
                    },
                }},
                VolumeMounts: []VolumeMount{{
                    Name:      "config",
                    MountPath: "/config",  // 挂载到 /config
                }},
                Command: []string{"kagent-adk"},
                Args:    []string{"static", "--filepath", "/config"},
            },
        },
    },
}
```

**生成的 config.json**:
```json
{
  "model": {
    "type": "sap_ai_core",
    "model": "gpt-4",
    "base_url": "https://...",
    "deployment_id": "d123",
    "temperature": "0.7"
  },
  "description": "My agent",
  "instruction": "You are helpful"
}
```

### 步骤 4: Python Runtime 加载配置

**位置**: `python/packages/kagent-adk/src/kagent/adk/cli.py` (第 22-50 行)

```python
def static(filepath: str = "/config"):
    # 1. 读取配置文件 (从挂载的 Secret)
    with open(f"{filepath}/config.json", "r") as f:
        config = json.load(f)
    
    # 2. 解析并验证配置
    agent_config = AgentConfig.model_validate(config)
    
    # 3. 转换为 Agent 实例
    agent = agent_config.to_agent("my-agent")
    
    # 4. 启动服务器
    uvicorn.run(server, host="0.0.0.0", port=8080)
```

**位置**: `python/packages/kagent-adk/src/kagent/adk/types.py` (第 174-191 行)

```python
class AgentConfig(BaseModel):
    model: Union[..., SAPAICore]  # 支持多种 Provider
    
    def to_agent(self, name: str) -> Agent:
        # 根据 type 实例化对应的 Model
        if self.model.type == "sap_ai_core":
            model = SAPAICoreNative(
                model=self.model.model,
                base_url=self.model.base_url,
                deployment_id=self.model.deployment_id,
                temperature=self.model.temperature,
                # ... API Key 从环境变量读取
            )
        
        # 创建 Agent (包含 model 实例)
        return Agent(name=name, model=model, ...)
```

### 步骤 5: Model 调用 LLM API

**位置**: `python/packages/kagent-adk/src/kagent/adk/models/_sap_ai_core.py` (第 180-287 行)

```python
class SAPAICore(BaseLlm):
    # 配置 (从 config.json 加载)
    model: str
    base_url: str
    deployment_id: str
    temperature: Optional[str]
    
    async def generate_content_async(self, request):
        # 1. 从环境变量获取 API Key (Secret 注入)
        api_key = os.environ.get("SAP_AI_CORE_API_KEY")
        
        # 2. 准备请求
        payload = {
            "messages": [...],
            "model": self.model,
            "temperature": float(self.temperature),
        }
        
        # 3. 调用 SAP AI Core API
        endpoint = f"/v2/inference/deployments/{self.deployment_id}/chat/completions"
        response = await self._client.post(
            endpoint, 
            json=payload,
            headers={"Authorization": f"Bearer {api_key}"}
        )
        
        # 4. 返回响应
        return self._convert_response(response.json())
```

---

## 🔄 完整数据流

```
┌──────────────────────┐
│ 1. ModelConfig YAML  │  用户创建
│   provider: SAPAICore│
│   model: gpt-4       │
│   sapAICore: {...}   │
└──────────┬───────────┘
           │
           ↓
┌──────────────────────┐
│ 2. Go Controller     │  监听并转换
│   ModelConfig CRD    │
│   → adk.SAPAICore    │
└──────────┬───────────┘
           │
           ↓
┌──────────────────────┐
│ 3. config.json       │  生成配置文件
│   {                  │
│     "model": {       │
│       "type": "sap"  │
│       "model": "gpt4"│
│     }                │
│   }                  │
└──────────┬───────────┘
           │
           ↓
┌──────────────────────┐
│ 4. Python Runtime    │  加载并实例化
│   AgentConfig        │
│   → SAPAICore Model  │
└──────────┬───────────┘
           │
           ↓
┌──────────────────────┐
│ 5. LLM API Call      │  调用 API
│   POST /chat/...     │
│   → SAP AI Core      │
└──────────────────────┘
```

---

## 🔑 关键点

### 1. **配置传递**
- **Kubernetes YAML** → **Go Struct** → **JSON** → **Python Class** → **API Call**

### 2. **认证信息**
- 存储在 Kubernetes **Secret**
- 通过**环境变量**注入到 Pod
- Python 代码从**环境变量**读取

### 3. **文件挂载**
- `config.json` 存储在 **Secret**
- 挂载到 Pod 的 **/config** 目录
- Python CLI 从 **/config/config.json** 读取

### 4. **类型安全**
- Go 使用 **struct** + JSON tags
- Python 使用 **Pydantic** + discriminator
- 确保类型一致性

---

## 📊 关键文件

| 层次 | 文件 | 作用 |
|------|------|------|
| **CRD** | `go/api/v1alpha2/modelconfig_types.go` | 定义 ModelConfig 结构 |
| **Translator** | `go/internal/controller/translator/agent/adk_api_translator.go` | 转换 ModelConfig |
| **Go Types** | `go/internal/adk/types.go` | Go 侧数据结构 |
| **Python Types** | `python/packages/kagent-adk/src/kagent/adk/types.py` | Python 侧数据结构 |
| **CLI** | `python/packages/kagent-adk/src/kagent/adk/cli.py` | 加载配置 |
| **Model** | `python/packages/kagent-adk/src/kagent/adk/models/_sap_ai_core.py` | LLM 实现 |

---

## 💡 示例：从 YAML 到 API 调用

### Input (YAML)
```yaml
spec:
  provider: SAPAICore
  model: "gpt-4"
  sapAICore:
    baseUrl: "https://api.ai.sap.com"
    deploymentId: "d123"
    temperature: "0.7"
```

### Output (API Request)
```http
POST https://api.ai.sap.com/v2/inference/deployments/d123/chat/completions
Authorization: Bearer sk-...
AI-Resource-Group: default
Content-Type: application/json

{
  "model": "gpt-4",
  "messages": [...],
  "temperature": 0.7
}
```

这就是 KAgent 如何使用 ModelConfig 来调用 LLM 的完整流程！🎉
