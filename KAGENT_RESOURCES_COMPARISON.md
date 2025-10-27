# KAgent 自定义资源对比详解

本文档详细对比 KAgent 中各种自定义资源的用途、区别和使用场景。

## 📋 资源概览

| 资源名称 | API 版本 | 短名称 | 用途 | 部署方式 |
|---------|----------|--------|------|----------|
| **Agent** | v1alpha2 | - | AI 智能体 | K8s Deployment |
| **MCPServer** | v1alpha1 | mcps, mcp | MCP 服务器 | K8s Deployment |
| **RemoteMCPServer** | v1alpha2 | rmcps | 远程 MCP 服务器 | 外部服务 |
| **ToolServer** | v1alpha1 | ts | 工具服务器 | 外部服务 |

---

## 🤖 Agent

### **定义**
AI 智能体，是 KAgent 的核心资源，负责执行 AI 任务。

### **特点**
- **两种类型**：`Declarative`（声明式）和 `BYO`（自带）
- **自动部署**：Controller 会创建 Deployment、Service 等
- **工具集成**：可以引用 MCPServer、RemoteMCPServer、ToolServer
- **模型配置**：通过 ModelConfig 指定使用的 LLM

### **示例**
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
spec:
  type: Declarative
  description: "My AI assistant"
  declarative:
    modelConfig: gpt4-model
    systemMessage: "You are a helpful assistant"
    tools:
      - type: McpServer
        mcpServer:
          name: file-tools
          toolNames: ["read_file", "write_file"]
```

### **生成的资源**
- Deployment（运行 Agent）
- Service（暴露 Agent 服务）
- Secret（存储配置）
- ServiceAccount（权限管理）

---

## 🔧 MCPServer

### **定义**
MCP（Model Context Protocol）服务器，提供标准化的工具接口。

### **特点**
- **本地部署**：在 Kubernetes 集群内运行
- **两种传输方式**：`stdio` 和 `http`
- **自动发现**：Controller 自动发现可用工具
- **生命周期管理**：完整的 K8s 资源管理

### **传输方式对比**

#### **Stdio 传输**
```yaml
apiVersion: kagent.dev/v1alpha1
kind: MCPServer
metadata:
  name: file-server
spec:
  transportType: stdio
  deployment:
    image: my-mcp-server:latest
    cmd: ["python", "server.py"]
    env:
      API_KEY: "secret"
```

#### **HTTP 传输**
```yaml
apiVersion: kagent.dev/v1alpha1
kind: MCPServer
metadata:
  name: web-server
spec:
  transportType: http
  httpTransport:
    targetPort: 8080
    path: "/mcp"
  deployment:
    image: my-http-server:latest
    port: 8080
```

### **生成的资源**
- Deployment（运行 MCP 服务器）
- Service（暴露 MCP 服务）
- ConfigMap（存储配置）

---

## 🌐 RemoteMCPServer

### **定义**
远程 MCP 服务器，连接集群外部的 MCP 服务。

### **特点**
- **外部服务**：不部署在 K8s 集群内
- **两种协议**：`SSE` 和 `STREAMABLE_HTTP`
- **连接管理**：处理网络连接、超时、重试
- **工具发现**：自动发现远程服务器的工具

### **协议对比**

#### **SSE 协议**
```yaml
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: external-sse-server
spec:
  description: "External SSE MCP server"
  protocol: SSE
  url: "https://api.example.com/mcp"
  headersFrom:
    - name: "Authorization"
      valueFrom:
        type: Secret
        valueRef: "auth-secret"
        key: "token"
```

#### **Streamable HTTP 协议**
```yaml
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: external-http-server
spec:
  description: "External HTTP MCP server"
  protocol: STREAMABLE_HTTP
  url: "https://api.example.com/mcp"
  timeout: "30s"
  terminateOnClose: true
```

---

## 🛠️ ToolServer

### **定义**
工具服务器，提供各种工具和服务的接口。

### **特点**
- **多种类型**：`stdio`、`sse`、`streamableHttp`
- **外部服务**：通常连接外部 API
- **工具发现**：自动发现可用工具
- **配置灵活**：支持复杂的配置选项

### **类型对比**

#### **Stdio 类型**
```yaml
apiVersion: kagent.dev/v1alpha1
kind: ToolServer
metadata:
  name: local-tools
spec:
  description: "Local command-line tools"
  config:
    type: stdio
    stdio:
      command: "python"
      args: ["tools.py"]
      env:
        API_KEY: "secret"
      readTimeoutSeconds: 30
```

#### **SSE 类型**
```yaml
apiVersion: kagent.dev/v1alpha1
kind: ToolServer
metadata:
  name: sse-tools
spec:
  description: "SSE-based tools"
  config:
    type: sse
    sse:
      url: "https://api.example.com/sse"
      headers:
        Authorization: "Bearer token"
      timeout: "30s"
```

#### **Streamable HTTP 类型**
```yaml
apiVersion: kagent.dev/v1alpha1
kind: ToolServer
metadata:
  name: http-tools
spec:
  description: "HTTP-based tools"
  config:
    type: streamableHttp
    streamableHttp:
      url: "https://api.example.com/tools"
      headers:
        Content-Type: "application/json"
      terminateOnClose: true
```

---

## 🔄 资源关系图

```
┌─────────────────────────────────────────────────────────────────┐
│                        KAgent 资源关系                          │
└─────────────────────────────────────────────────────────────────┘

┌─────────────┐
│   Agent     │  ←── 核心资源，AI 智能体
│ (v1alpha2)  │
└─────┬───────┘
      │
      ├─── 引用 ───┐
      │            │
      ▼            ▼
┌─────────────┐  ┌─────────────────┐
│ MCPServer   │  │ RemoteMCPServer │
│(v1alpha1)   │  │  (v1alpha2)     │
│             │  │                 │
│ 本地部署     │  │ 远程连接        │
│ K8s 资源    │  │ 外部服务        │
└─────────────┘  └─────────────────┘
      │
      ▼
┌─────────────┐
│ ToolServer  │
│(v1alpha1)   │
│             │
│ 工具服务     │
│ 外部 API    │
└─────────────┘
```

---

## 📊 详细对比表

| 特性 | Agent | MCPServer | RemoteMCPServer | ToolServer |
|------|-------|-----------|-----------------|------------|
| **部署位置** | K8s 集群内 | K8s 集群内 | 集群外部 | 集群外部 |
| **资源管理** | 自动创建 Deployment | 自动创建 Deployment | 仅连接管理 | 仅连接管理 |
| **传输协议** | HTTP (Agent API) | stdio/http | SSE/HTTP | stdio/SSE/HTTP |
| **工具发现** | 通过引用其他资源 | 自动发现 | 自动发现 | 自动发现 |
| **生命周期** | 完整 K8s 管理 | 完整 K8s 管理 | 连接管理 | 连接管理 |
| **配置复杂度** | 中等 | 高 | 低 | 高 |
| **使用场景** | AI 任务执行 | 标准化工具 | 远程 MCP 服务 | 通用工具服务 |

---

## 🎯 使用场景

### **Agent 使用场景**
```yaml
# 场景 1: 简单的 AI 助手
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: simple-assistant
spec:
  type: Declarative
  declarative:
    modelConfig: gpt4-model
    systemMessage: "You are a helpful assistant"

---
# 场景 2: 带工具的 AI 助手
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: tooled-assistant
spec:
  type: Declarative
  declarative:
    modelConfig: gpt4-model
    systemMessage: "You are a helpful assistant with tools"
    tools:
      - type: McpServer
        mcpServer:
          name: file-tools
          toolNames: ["read_file", "write_file"]
      - type: McpServer
        mcpServer:
          name: web-tools
          toolNames: ["search_web", "get_weather"]
```

### **MCPServer 使用场景**
```yaml
# 场景 1: 文件操作工具
apiVersion: kagent.dev/v1alpha1
kind: MCPServer
metadata:
  name: file-tools
spec:
  transportType: stdio
  deployment:
    image: file-mcp-server:latest
    cmd: ["python", "file_server.py"]
    env:
      WORK_DIR: "/workspace"

---
# 场景 2: Web API 工具
apiVersion: kagent.dev/v1alpha1
kind: MCPServer
metadata:
  name: web-tools
spec:
  transportType: http
  httpTransport:
    targetPort: 8080
  deployment:
    image: web-mcp-server:latest
    port: 8080
    env:
      API_BASE_URL: "https://api.example.com"
```

### **RemoteMCPServer 使用场景**
```yaml
# 场景 1: 连接外部 MCP 服务
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: external-mcp
spec:
  description: "External MCP service"
  protocol: STREAMABLE_HTTP
  url: "https://mcp.example.com"
  headersFrom:
    - name: "Authorization"
      valueFrom:
        type: Secret
        valueRef: "mcp-auth"
        key: "token"

---
# 场景 2: 连接 SSE 服务
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: sse-mcp
spec:
  description: "SSE MCP service"
  protocol: SSE
  url: "https://sse.example.com/mcp"
  sseReadTimeout: "60s"
```

### **ToolServer 使用场景**
```yaml
# 场景 1: 命令行工具
apiVersion: kagent.dev/v1alpha1
kind: ToolServer
metadata:
  name: cli-tools
spec:
  description: "Command line tools"
  config:
    type: stdio
    stdio:
      command: "python"
      args: ["cli_tools.py"]
      env:
        TOOL_CONFIG: "/config/tools.yaml"

---
# 场景 2: HTTP API 工具
apiVersion: kagent.dev/v1alpha1
kind: ToolServer
metadata:
  name: api-tools
spec:
  description: "HTTP API tools"
  config:
    type: streamableHttp
    streamableHttp:
      url: "https://api.example.com/tools"
      headers:
        Authorization: "Bearer token"
        Content-Type: "application/json"
```

---

## 🔧 最佳实践

### **1. 资源选择指南**

| 需求 | 推荐资源 | 原因 |
|------|----------|------|
| AI 智能体 | Agent | 核心功能 |
| 集群内工具服务 | MCPServer | 标准化、自动管理 |
| 外部 MCP 服务 | RemoteMCPServer | 简单连接 |
| 复杂外部工具 | ToolServer | 灵活配置 |

### **2. 部署策略**

```yaml
# 推荐：使用 Agent + MCPServer 组合
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: production-agent
spec:
  type: Declarative
  declarative:
    modelConfig: production-model
    tools:
      - type: McpServer
        mcpServer:
          name: production-tools
          toolNames: ["*"]  # 使用所有工具
```

### **3. 监控和调试**

```bash
# 查看所有资源状态
kubectl get agents,mcpservers,remotemcpservers,toolservers

# 查看 Agent 详细信息
kubectl describe agent my-agent

# 查看 MCPServer 日志
kubectl logs -l app=mcp-server

# 查看发现的工具
kubectl get mcpserver file-tools -o jsonpath='{.status.discoveredTools}'
```

---

## 📝 总结

### **核心区别**

1. **Agent**：AI 智能体，核心资源，自动部署
2. **MCPServer**：本地 MCP 服务，K8s 部署，标准化
3. **RemoteMCPServer**：远程 MCP 服务，外部连接，简单
4. **ToolServer**：通用工具服务，外部连接，灵活

### **选择建议**

- **新项目**：从 Agent + MCPServer 开始
- **现有服务**：使用 RemoteMCPServer 或 ToolServer
- **复杂需求**：组合使用多种资源
- **标准化**：优先选择 MCPServer

### **版本说明**

- **v1alpha1**：早期版本，功能稳定
- **v1alpha2**：新版本，功能更丰富
- **迁移建议**：逐步迁移到 v1alpha2

这就是 KAgent 中各种自定义资源的详细对比！🎉
