# SAP AI Core 快速入门指南

本指南将帮助您在 5 分钟内开始使用 SAP AI Core 与 KAgent。

## 前提条件

✅ 已安装 KAgent  
✅ 拥有 SAP AI Core 访问权限  
✅ 已部署至少一个生成式 AI 模型  

## 第一步：获取 SAP AI Core 凭证

从 SAP BTP Cockpit 获取以下信息：

```bash
# 必需信息
SAP_AI_CORE_API_KEY="your-api-key"
SAP_AI_CORE_BASE_URL="https://api.ai.prod.eu-central-1.aws.ml.hana.ondemand.com"
SAP_AI_CORE_DEPLOYMENT_ID="d1234567890abcdef"
SAP_AI_CORE_RESOURCE_GROUP="default"

# 可选（如使用 OAuth）
SAP_AI_CORE_AUTH_URL="https://oauth.authentication.eu10.hana.ondemand.com/oauth/token"
SAP_AI_CORE_CLIENT_ID="your-client-id"
SAP_AI_CORE_CLIENT_SECRET="your-client-secret"
```

## 第二步：创建 Secret

```bash
kubectl create secret generic kagent-sap-aicore \
  --from-literal=SAP_AI_CORE_API_KEY="${SAP_AI_CORE_API_KEY}" \
  --namespace=kagent
```

## 第三步：应用配置

保存以下内容为 `sap-aicore-config.yaml`：

```yaml
---
# ModelConfig
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: sap-aicore-model
  namespace: kagent
spec:
  provider: SAPAICore
  model: "gpt-4"
  apiKeySecret: kagent-sap-aicore
  apiKeySecretKey: SAP_AI_CORE_API_KEY
  sapAICore:
    baseUrl: "https://api.ai.prod.eu-central-1.aws.ml.hana.ondemand.com"
    resourceGroup: "default"
    deploymentId: "d1234567890abcdef"  # 替换为您的部署 ID
    temperature: 0.7
    maxTokens: 2048

---
# Agent
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-sap-agent
  namespace: kagent
spec:
  type: Declarative
  description: "我的 SAP AI Core 智能助手"
  declarative:
    modelConfig: sap-aicore-model
    systemMessage: "你是一个专业的 AI 助手。"
```

应用配置：

```bash
kubectl apply -f sap-aicore-config.yaml
```

## 第四步：等待就绪

```bash
# 检查 ModelConfig 状态
kubectl get modelconfig sap-aicore-model -n kagent

# 检查 Agent 状态
kubectl get agent my-sap-agent -n kagent

# 等待 Pod 运行
kubectl wait --for=condition=Available deployment/my-sap-agent -n kagent --timeout=120s
```

## 第五步：测试 Agent

### 方式 1: 使用 kubectl port-forward

```bash
# Port forward
kubectl port-forward -n kagent svc/kagent-controller 8083:8083

# 在另一个终端测试
curl -X POST http://localhost:8083/api/agents/kagent/my-sap-agent/invoke \
  -H "Content-Type: application/json" \
  -d '{
    "message": {
      "role": "user",
      "parts": [{"text": "你好，请介绍一下你自己"}]
    }
  }'
```

### 方式 2: 使用 KAgent CLI

```bash
# 安装 CLI（如果尚未安装）
curl -sSL https://kagent.dev/install.sh | bash

# 创建会话
kagent session create --agent kagent/my-sap-agent

# 发送消息
kagent session invoke --session <session-id> --task "你好，请介绍一下你自己"
```

## 成功！🎉

如果您看到了响应，说明集成成功！

## 下一步

- 📖 阅读[完整文档](../docs/SAP_AI_CORE_INTEGRATION.md)
- 🔧 调整[模型参数](../docs/SAP_AI_CORE_INTEGRATION.md#配置参数说明)
- 🛠️ 添加[工具和技能](https://kagent.dev/docs/tools)
- 📊 设置[监控](https://kagent.dev/docs/monitoring)

## 常见问题

### Q: 如何找到我的 Deployment ID？

A: 在 SAP AI Core Launchpad 中：
1. 进入 "Deployments"
2. 选择您的部署
3. 复制 "Deployment ID"

### Q: 支持哪些模型？

A: SAP AI Core 支持多种模型，包括：
- OpenAI (GPT-4, GPT-3.5)
- Anthropic (Claude)
- 开源模型 (Llama, Mistral 等)

具体取决于您的部署配置。

### Q: 如何启用流式响应？

A: 流式响应会自动启用（如果 SAP AI Core 支持）。在调用时添加 `stream=true` 参数。

### Q: 遇到错误怎么办？

A: 查看故障排查指南：
```bash
# 查看 Agent 日志
kubectl logs -n kagent deployment/my-sap-agent --tail=50

# 查看 Controller 日志
kubectl logs -n kagent deployment/kagent-controller --tail=50
```

更多帮助请参考[故障排查文档](../docs/SAP_AI_CORE_INTEGRATION.md#故障排查)。

## 清理

```bash
kubectl delete agent my-sap-agent -n kagent
kubectl delete modelconfig sap-aicore-model -n kagent
kubectl delete secret kagent-sap-aicore -n kagent
```

---

需要帮助？访问 [KAgent 文档](https://kagent.dev) 或提交 [GitHub Issue](https://github.com/kagent-dev/kagent/issues)。



