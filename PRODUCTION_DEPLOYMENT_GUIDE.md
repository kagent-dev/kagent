# KAgent SAP AI Core 生产环境部署指南

## 概述

本指南详细说明如何将集成了SAP AI Core的KAgent部署到生产环境中。

## 前置条件

### 1. 环境要求
- Kubernetes 集群 (v1.24+)
- Helm 3.x
- Docker
- kubectl
- 访问SAP AI Core实例的权限

### 2. SAP AI Core 配置
在部署前，确保您有以下SAP AI Core信息：
- Base URL (例如: `https://api.ai.prod.eu-central-1.aws.ml.hana.ondemand.com`)
- Resource Group
- Deployment ID
- API Key 或 OAuth2 凭据

## 部署步骤

### 步骤 1: 构建镜像

#### 1.1 构建Go控制器镜像
```bash
cd /Users/I756374/Documents/myProjects/kagent/go
make docker-build-controller
```

#### 1.2 构建Python ADK镜像
```bash
cd /Users/I756374/Documents/myProjects/kagent/python
make docker-build
```

#### 1.3 构建UI镜像
```bash
cd /Users/I756374/Documents/myProjects/kagent/ui
make docker-build
```

### 步骤 2: 准备Kubernetes Secrets

#### 2.1 创建SAP AI Core API Key Secret
```bash
kubectl create secret generic kagent-sap-aicore \
  --from-literal=SAP_AI_CORE_API_KEY="your-api-key-here" \
  --from-literal=CLIENT_SECRET="your-client-secret-here" \
  -n kagent-system
```

#### 2.2 验证Secret创建
```bash
kubectl get secret kagent-sap-aicore -n kagent-system
```

### 步骤 3: 配置Helm Values

#### 3.1 更新values.yaml
确保在 `helm/kagent/values.yaml` 中已配置SAP AI Core：

```yaml
modelConfigs:
  sapAICore:
    provider: SAPAICore
    model: "gpt-4"  # 根据您的SAP AI Core部署调整
    apiKeySecretRef: kagent-sap-aicore
    apiKeySecretKey: SAP_AI_CORE_API_KEY
    config:
      baseUrl: "https://api.ai.prod.eu-central-1.aws.ml.hana.ondemand.com"
      resourceGroup: "default"
      deploymentId: "your-deployment-id"
      authUrl: "https://your-tenant.authentication.eu10.hana.ondemand.com/oauth/token"
      clientId: "your-client-id"
      temperature: 0.7
      maxTokens: 2048
```

### 步骤 4: 部署到Kubernetes

#### 4.1 安装CRDs
```bash
cd /Users/I756374/Documents/myProjects/kagent
helm install kagent-crds helm/kagent-crds/ \
  --namespace kagent-system \
  --create-namespace
```

#### 4.2 安装KAgent
```bash
helm install kagent helm/kagent/ \
  --namespace kagent-system \
  --set controller.image.repository=your-registry/kagent-controller \
  --set controller.image.tag=latest \
  --set ui.image.repository=your-registry/kagent-ui \
  --set ui.image.tag=latest \
  --set app.image.repository=your-registry/kagent-app \
  --set app.image.tag=latest
```

#### 4.3 验证部署
```bash
# 检查Pod状态
kubectl get pods -n kagent-system

# 检查服务状态
kubectl get svc -n kagent-system

# 检查CRDs
kubectl get crd | grep kagent
```

### 步骤 5: 创建SAP AI Core ModelConfig

#### 5.1 创建ModelConfig资源
```bash
kubectl apply -f examples/sap-aicore-modelconfig.yaml
```

#### 5.2 验证ModelConfig
```bash
kubectl get modelconfig sap-ai-core-model -o yaml
```

### 步骤 6: 测试集成

#### 6.1 创建测试Agent
```yaml
apiVersion: v1alpha2
kind: Agent
metadata:
  name: sap-test-agent
spec:
  modelConfigRef: sap-ai-core-model
  instruction: "你是一个SAP AI Core测试助手"
  tools: []
```

#### 6.2 测试Agent功能
```bash
# 通过CLI测试
kagent agent invoke sap-test-agent --message "Hello, SAP AI Core!"

# 通过API测试
curl -X POST http://kagent-ui.kagent-system.svc.cluster.local/api/v1/agents/sap-test-agent/invoke \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello, SAP AI Core!"}'
```

## 监控和故障排除

### 1. 查看日志
```bash
# 控制器日志
kubectl logs -f deployment/kagent-controller -n kagent-system

# UI日志
kubectl logs -f deployment/kagent-ui -n kagent-system

# Agent Pod日志
kubectl logs -f deployment/sap-test-agent -n kagent-system
```

### 2. 常见问题

#### 2.1 SAP AI Core连接问题
- 检查API Key是否正确
- 验证Base URL和Deployment ID
- 确认网络连接

#### 2.2 OAuth2认证问题
- 验证Client ID和Client Secret
- 检查Auth URL是否正确
- 确认Token权限

#### 2.3 模型调用失败
- 检查模型名称是否正确
- 验证Resource Group配置
- 查看SAP AI Core服务状态

### 3. 性能优化

#### 3.1 资源配置
```yaml
resources:
  requests:
    memory: "512Mi"
    cpu: "250m"
  limits:
    memory: "1Gi"
    cpu: "500m"
```

#### 3.2 扩缩容配置
```yaml
autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
```

## 安全考虑

### 1. Secret管理
- 使用Kubernetes Secrets存储敏感信息
- 定期轮换API Keys
- 启用RBAC权限控制

### 2. 网络安全
- 配置Network Policies
- 使用TLS加密通信
- 限制API访问

### 3. 审计日志
- 启用审计日志记录
- 监控API调用
- 设置告警规则

## 升级和维护

### 1. 版本升级
```bash
# 升级Helm Chart
helm upgrade kagent helm/kagent/ \
  --namespace kagent-system \
  --set controller.image.tag=v1.1.0
```

### 2. 备份和恢复
- 备份ModelConfig资源
- 备份Secret配置
- 制定灾难恢复计划

## 生产环境最佳实践

1. **高可用性**: 部署多个副本
2. **监控**: 集成Prometheus和Grafana
3. **日志**: 使用ELK或类似方案
4. **安全**: 定期安全扫描和更新
5. **性能**: 监控资源使用和响应时间

## 支持

如遇到问题，请：
1. 查看日志和事件
2. 检查SAP AI Core服务状态
3. 参考故障排除指南
4. 联系技术支持团队


