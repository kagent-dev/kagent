# Wake-Cycle Agent Pattern

A pattern for building autonomous AI agents that operate on a schedule in Kubernetes, maintaining state across wake cycles and performing value-generating work independently.

## Overview

Traditional AI agents are **reactive** - they respond to user prompts. Wake-Cycle Agents are **proactive** - they wake on a schedule, check their state and environment, and take autonomous action.

This pattern is inspired by real production autonomous agents that:
- Wake every N minutes via cron
- Check for pending tasks and messages
- Perform autonomous work
- Update state and log activity
- Sleep until next wake

## Use Cases

- **Monitoring & Alerting**: Periodic health checks, anomaly detection
- **Content Generation**: Scheduled blog posts, social media content
- **Market Analysis**: Regular scans for trading opportunities
- **Task Processing**: Working through backlogs autonomously
- **Revenue Operations**: Freelance marketplace monitoring, bounty hunting

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Kubernetes Cluster                       │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌──────────────────┐    ┌──────────────────┐               │
│  │   CronJob        │    │   ConfigMap      │               │
│  │   (Wake Timer)   │───▶│   (Constitution) │               │
│  └────────┬─────────┘    └──────────────────┘               │
│           │                                                  │
│           ▼                                                  │
│  ┌──────────────────┐    ┌──────────────────┐               │
│  │   kagent Agent   │───▶│   PVC            │               │
│  │   (Wake-Cycle)   │    │   (State Store)  │               │
│  └────────┬─────────┘    └──────────────────┘               │
│           │                                                  │
│           ▼                                                  │
│  ┌──────────────────┐    ┌──────────────────┐               │
│  │   MCP ToolServer │    │   Secret         │               │
│  │   (Capabilities) │    │   (API Keys)     │               │
│  └──────────────────┘    └──────────────────┘               │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

## Components

### 1. Constitution (ConfigMap)

The constitution defines the agent's values, permissions, and behavioral boundaries. It's mounted as a ConfigMap so it can be version-controlled and audited.

```yaml
# constitution.yaml
data:
  CONSTITUTION.md: |
    # Agent Constitution

    ## Core Values
    1. Create value, don't just monitor
    2. Log everything for transparency
    3. Bias toward action
    4. External actions require appropriate gates

    ## Permissions
    - AUTONOMOUS: Local operations, research, planning
    - NOTIFY-AND-PROCEED: Low-risk external actions
    - APPROVAL-REQUIRED: Spending, external communications
    - FORBIDDEN: Impersonation, credential exposure
```

### 2. State Persistence (PVC)

The agent maintains state across wake cycles via a PersistentVolumeClaim:

```yaml
state.json:
  last_wake: 930
  current_focus: "Processing backlog items"
  blocked_on: []
  active_tasks: ["content_generation", "monitoring"]

backlog.md:
  - [ ] High-priority task A
  - [ ] Medium-priority task B
  - [x] Completed task C
```

### 3. Wake Trigger (CronJob)

A CronJob triggers the agent on schedule:

```yaml
schedule: "*/15 * * * *"  # Every 15 minutes
```

The CronJob invokes the kagent with a wake prompt containing:
- Current timestamp
- Any external signals (messages, alerts)
- Context about the current state

### 4. MCP Tools (ToolServer)

The agent's capabilities are defined via MCP ToolServers:

- **File operations**: Read/write state, logs
- **External APIs**: Web requests, notifications
- **Specialized tools**: Market scanners, content generators

## Getting Started

### Prerequisites

- Kind cluster with kagent installed
- Anthropic or OpenAI API key

### Quick Start

1. Apply the manifests using kustomize:
```bash
kubectl apply -k manifests/
```

2. Verify deployment:
```bash
kubectl -n wake-cycle-demo get agent,toolserver,pvc
```

3. Watch the agent wake:
```bash
kubectl logs -f -n wake-cycle-demo -l app.kubernetes.io/name=wake-cycle-agent
```

### ModelConfig Prerequisite

The agent references a `ModelConfig` named `anthropic-claude` that must exist in the `kagent` namespace. Create one before deploying:

```yaml
# modelconfig.yaml
apiVersion: kagent.dev/v1alpha1
kind: ModelConfig
metadata:
  name: anthropic-claude
  namespace: kagent
spec:
  provider: anthropic
  # Use the latest Claude model available
  # Check Anthropic's documentation for current model IDs
  model: claude-sonnet-4-20250514  # or claude-3-5-sonnet-20241022
  apiKeyFrom:
    secretKeyRef:
      name: anthropic-api-key
      key: api-key
```

Apply the ModelConfig:
```bash
kubectl apply -f modelconfig.yaml
```

### Configuration

Customize the pattern via kustomize patches:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `wake.schedule` | Cron schedule for wake cycles | `*/15 * * * *` |
| `agent.modelConfig` | Reference to ModelConfig | `anthropic-claude` |
| `state.storageClass` | Storage class for PVC | `standard` |
| `state.size` | PVC size | `1Gi` |

## Best Practices

### State Management

1. **Structured state**: Use JSON for programmatic access
2. **Human-readable backlog**: Markdown for task lists
3. **Journal logging**: Daily logs for transparency
4. **Checkpoint frequently**: Save state after each significant action

### Wake Cycle Design

1. **Priority order**: Messages > Urgent items > Active tasks > Autonomous work
2. **Avoid busywork**: Every wake should produce tangible value
3. **Long-running task chunking**: Break batch operations across multiple wakes
4. **Graceful degradation**: Handle failures without losing progress

### Security

1. **Principle of least privilege**: Only mount necessary secrets
2. **Constitution enforcement**: Clearly define permission boundaries
3. **Audit logging**: Log all external actions
4. **Rate limiting**: Prevent runaway resource consumption

## Example: Content Generator Agent

See `examples/content-generator/` for a complete example that:
- Wakes every 4 hours
- Checks a content queue in Google Sheets
- Generates and posts content to social media
- Logs accomplishments to state

## Contributing

Contributions welcome! Please:
1. Follow the kagent contribution guidelines
2. Include tests for new functionality
3. Document any new configuration options

## License

Apache 2.0 - See LICENSE in the kagent repository root.
