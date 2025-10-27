# SAP AI Core Integration for KAgent

本文档介绍如何在KAgent中集成和使用SAP AI Core作为LLM提供商。

## 概述

SAP AI Core是SAP Business Technology Platform (BTP)的一部分，提供企业级的AI模型部署和管理能力。KAgent现已支持将SAP AI Core作为LLM提供商，允许您在Kubernetes环境中调用部署在SAP AI Core上的生成式AI模型。

## 架构

SAP AI Core集成遵循KAgent的标准架构模式：

```
用户请求 → Agent → ModelConfig → SAP AI Core适配器 → SAP AI Core API → 模型推理
```

## 前提条件

1. **SAP BTP账户**：拥有SAP Business Technology Platform账户
2. **SAP AI Core服务实例**：已创建SAP AI Core服务实例
3. **模型部署**：在SAP AI Core中已部署生成式AI模型（如GPT-4、Claude等）
4. **服务密钥**：获取SAP AI Core服务密钥，包含API Key和其他凭证

## 配置步骤

### 1. 创建Kubernetes Secret

首先，创建包含SAP AI Core凭证的Secret：

```bash
kubectl create secret generic kagent-sap-aicore \
  --from-literal=SAP_AI_CORE_API_KEY='your-api-key-here' \
  --from-literal=CLIENT_SECRET='your-client-secret-here' \
  -n kagent
```

或使用YAML文件：

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kagent-sap-aicore
  namespace: kagent
type: Opaque
stringData:
  SAP_AI_CORE_API_KEY: "your-sap-ai-core-api-key"
  CLIENT_SECRET: "your-oauth-client-secret"  # 可选：用于OAuth认证
```

### 2. 创建ModelConfig

创建SAP AI Core的ModelConfig资源：

```yaml
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: sap-aicore-gpt4
  namespace: kagent
spec:
  provider: SAPAICore
  model: "gpt-4"  # 根据您的部署调整
  apiKeySecret: kagent-sap-aicore
  apiKeySecretKey: SAP_AI_CORE_API_KEY
  sapAICore:
    # SAP AI Core API基础URL
    baseUrl: "https://api.ai.prod.eu-central-1.aws.ml.hana.ondemand.com"
    # 资源组
    resourceGroup: "default"
    # 部署ID（必填）
    deploymentId: "d1234567890abcdef"
    # OAuth配置（可选）
    authUrl: "https://your-oauth-server.authentication.eu10.hana.ondemand.com/oauth/token"
    clientId: "your-client-id"
    # 模型参数（可选）
    temperature: 0.7
    maxTokens: 2048
    topP: 0.9
    frequencyPenalty: 0.0
    presencePenalty: 0.0
```

### 3. 创建Agent

创建使用SAP AI Core的Agent：

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: sap-aicore-agent
  namespace: kagent
spec:
  type: Declarative
  description: "使用SAP AI Core的智能助手"
  declarative:
    modelConfig: sap-aicore-gpt4
    systemMessage: "你是一个由SAP AI Core驱动的智能助手。"
    tools: []
```

## 配置参数说明

### 必需参数

| 参数 | 说明 | 示例 |
|------|------|------|
| `baseUrl` | SAP AI Core API基础URL | `https://api.ai.prod.eu-central-1.aws.ml.hana.ondemand.com` |
| `resourceGroup` | SAP AI Core资源组 | `default` |
| `deploymentId` | 模型部署ID | `d1234567890abcdef` |

### 可选参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `authUrl` | OAuth令牌端点URL | - |
| `clientId` | OAuth客户端ID | - |
| `temperature` | 采样温度（0.0-2.0） | - |
| `maxTokens` | 最大生成token数 | - |
| `topP` | Top-p采样参数 | - |
| `topK` | Top-k采样参数 | - |
| `frequencyPenalty` | 频率惩罚 | - |
| `presencePenalty` | 存在惩罚 | - |

## 认证方式

SAP AI Core支持两种认证方式：

### 1. API Key认证（推荐）

直接使用API Key进行认证：

```yaml
apiKeySecret: kagent-sap-aicore
apiKeySecretKey: SAP_AI_CORE_API_KEY
```

### 2. OAuth2认证

使用OAuth2客户端凭证流：

```yaml
sapAICore:
  authUrl: "https://oauth-server.authentication.eu10.hana.ondemand.com/oauth/token"
  clientId: "your-client-id"
```

Secret中需包含`CLIENT_SECRET`字段。

## 使用示例

### 通过CLI调用

```bash
# 创建会话
kagent session create --agent kagent/sap-aicore-agent

# 发送消息
kagent session invoke --session <session-id> --task "解释什么是SAP AI Core"
```

### 通过API调用

```bash
curl -X POST http://kagent-controller:8083/api/agents/kagent/sap-aicore-agent/invoke \
  -H "Content-Type: application/json" \
  -d '{
    "message": {
      "role": "user",
      "parts": [{"text": "解释什么是SAP AI Core"}]
    }
  }'
```

## 支持的模型

SAP AI Core支持多种生成式AI模型，包括但不限于：

- **OpenAI模型**：GPT-4, GPT-3.5-turbo等
- **Anthropic模型**：Claude-3系列
- **开源模型**：Llama 2, Mistral等

具体支持的模型取决于您在SAP AI Core中的部署配置。

## 故障排查

### 1. 认证失败

**错误**：`HTTP 401: Unauthorized`

**解决方案**：
- 检查API Key是否正确
- 确认Secret已正确创建并挂载
- 验证OAuth配置（如使用OAuth认证）

### 2. 部署ID无效

**错误**：`HTTP 404: Deployment not found`

**解决方案**：
- 确认deploymentId正确
- 在SAP AI Core控制台验证部署状态
- 检查resourceGroup是否正确

### 3. 配额限制

**错误**：`HTTP 429: Too Many Requests`

**解决方案**：
- 检查SAP AI Core配额限制
- 实施请求限流
- 升级SAP AI Core服务计划

### 4. 超时错误

**错误**：`Timeout waiting for response`

**解决方案**：
- 增加timeout参数值
- 检查网络连接
- 验证SAP AI Core服务状态

## 最佳实践

### 1. 安全性

- ✅ 使用Kubernetes Secret存储敏感信息
- ✅ 定期轮换API Key和Client Secret
- ✅ 使用RBAC限制对Secret的访问
- ✅ 启用网络策略限制出站流量

### 2. 性能优化

- 合理设置`maxTokens`避免不必要的长响应
- 使用适当的`temperature`值平衡创意性和确定性
- 考虑使用流式响应以改善用户体验

### 3. 成本控制

- 监控token使用量
- 设置合理的请求限制
- 使用缓存机制避免重复请求
- 选择合适的模型大小

### 4. 高可用性

- 配置多个ModelConfig作为备份
- 实现故障转移机制
- 监控API响应时间和成功率

## 监控和日志

### 查看Agent日志

```bash
kubectl logs -n kagent deployment/sap-aicore-agent -f
```

### 查看Controller日志

```bash
kubectl logs -n kagent deployment/kagent-controller -f | grep "SAP AI Core"
```

### 监控指标

KAgent会自动收集以下指标：

- LLM调用次数
- 响应时间
- Token使用量
- 错误率

通过Prometheus和Grafana可视化这些指标。

## 与其他提供商的比较

| 特性 | SAP AI Core | OpenAI | Anthropic |
|------|-------------|--------|-----------|
| 部署位置 | 企业云/私有云 | 公有云 | 公有云 |
| 数据主权 | ✅ 完全控制 | ❌ 限制 | ❌ 限制 |
| 模型选择 | 多种 | OpenAI模型 | Claude模型 |
| 企业集成 | ✅ SAP生态系统 | 有限 | 有限 |
| 合规性 | ✅ 高 | 中 | 中 |

## 参考资源

- [SAP AI Core官方文档](https://help.sap.com/docs/ai-core)
- [SAP BTP文档](https://help.sap.com/docs/btp)
- [KAgent文档](https://kagent.dev)
- [示例配置文件](../examples/sap-aicore-modelconfig.yaml)

## 支持

如遇到问题，请通过以下渠道获取支持：

1. 查看[故障排查指南](#故障排查)
2. 搜索[GitHub Issues](https://github.com/kagent-dev/kagent/issues)
3. 提交新的Issue
4. 加入KAgent社区讨论

## 版本兼容性

| KAgent版本 | SAP AI Core API版本 | 状态 |
|-----------|-------------------|------|
| >= 0.5.0 | v2 | ✅ 支持 |
| < 0.5.0 | - | ❌ 不支持 |

---

**更新时间**：2025-01-21  
**文档版本**：1.0



