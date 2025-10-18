# Kagent Slackbot

Production-ready Slack bot for the Kagent multi-agent platform. This bot provides a unified interface to interact with multiple AI agents through Slack, featuring dynamic agent discovery, intelligent routing, and rich Block Kit formatting.

## Features

- **Dynamic Agent Discovery**: Automatically discovers agents from Kagent via `/api/agents`
- **Intelligent Routing**: Keyword-based matching to route messages to appropriate agents
- **Streaming Responses**: Real-time updates for declarative agents with human-in-the-loop approval
- **RBAC**: Slack user group integration with agent-level permissions
- **Rich Formatting**: Professional Slack Block Kit responses with metadata
- **Session Management**: Maintains conversation context across multiple turns
- **Async Architecture**: Built with modern slack-bolt AsyncApp for high performance
- **Production Ready**: Prometheus metrics, health checks, structured logging
- **Kubernetes Native**: Complete K8s manifests with HPA, PDB, and security contexts

## Architecture

```
User in Slack
    ↓
@mention / slash command
    ↓
Kagent Slackbot (AsyncApp)
    ├── Agent Discovery (cache agents from /api/agents)
    ├── Agent Router (keyword matching)
    └── A2A Client (JSON-RPC 2.0)
        ↓
Kagent Controller (/api/a2a/{namespace}/{name})
    ↓
    ┌─────────┬─────────┬──────────┐
    │ k8s     │ security│ research │
    │ agent   │ agent   │ agent    │
    └─────────┴─────────┴──────────┘
```

## Quick Start

### Prerequisites

- Python 3.11+
- Slack workspace with bot app configured
- Kagent instance running and accessible

### Installation

1. Navigate to the slackbot directory:
```bash
cd /path/to/kagent/slackbot
```

2. Create virtual environment and install dependencies:
```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -e ".[dev]"
```

3. Configure environment variables:
```bash
cp .env.example .env
# Edit .env with your Slack tokens and Kagent URL
```

Required environment variables:
- `SLACK_BOT_TOKEN`: Bot user OAuth token (xoxb-*)
- `SLACK_APP_TOKEN`: App-level token for Socket Mode (xapp-*)
- `SLACK_SIGNING_SECRET`: Signing secret for request verification
- `KAGENT_BASE_URL`: Kagent API base URL (e.g., http://localhost:8083)

### Running Locally

```bash
source .venv/bin/activate
python -m kagent_slackbot.main
```

The bot will:
1. Connect to Slack via Socket Mode (WebSocket)
2. Start health server on port 8080
3. Discover available agents from Kagent
4. Begin processing messages

### Slack App Configuration

Your Slack app needs these OAuth scopes:
- `app_mentions:read` - Receive @mentions
- `chat:write` - Send messages
- `commands` - Handle slash commands
- `reactions:write` - Add emoji reactions

Required features:
- **Socket Mode**: Enabled (no public HTTP endpoint needed)
- **Event Subscriptions**: `app_mention`
- **Slash Commands**: `/agents`, `/agent-switch`

## Usage

### Interacting with Agents

**@mention the bot** with your question:
```
@kagent show me failing pods
```

The bot will:
1. Extract keywords from your message ("pods" → k8s-agent)
2. Route to the appropriate agent
3. Respond with formatted blocks showing:
   - Which agent responded
   - Why that agent was selected
   - Response time and session ID

### Slash Commands

**List available agents**:
```
/agents
```

Shows all agents with:
- Namespace and name
- Description
- Ready status

**Switch to specific agent**:
```
/agent-switch kagent/security-agent
```

All subsequent @mentions will use this agent until you reset.

**Reset to automatic routing**:
```
/agent-switch reset
```

Returns to keyword-based agent selection.

### Human-in-the-Loop (HITL) Approvals

When agents request approval for sensitive operations (like deleting pods), the bot displays an interactive approval UI:

```
@kagent delete pod my-app-xyz in namespace prod
```

The bot shows:
- Tool name and arguments requiring approval
- **Approve** and **Deny** buttons
- Session and task context

**Workflow**:
1. Agent detects sensitive operation and requests approval
2. Slackbot displays approval buttons in Slack
3. User clicks Approve or Deny
4. Slackbot sends structured decision (DataPart + TextPart) to agent
5. Agent resumes execution with user's decision
6. Completion message shown to user

**Technical Details**:
- Uses A2A protocol `input_required` state for interrupts
- Sends DataPart with `decision_type: tool_approval` for reliable parsing
- Tracks contextId and taskId for proper task resumption
- Streams completion responses in real-time

## Development

### Project Structure

```
src/kagent_slackbot/
├── main.py                 # Entry point, AsyncApp initialization
├── config.py               # Configuration management
├── constants.py            # Application constants
├── handlers/               # Slack event handlers
│   ├── mentions.py        # @mention handling
│   ├── commands.py        # Slash command handling
│   ├── actions.py         # Button action handling (HITL approvals)
│   └── middleware.py      # Prometheus metrics
├── services/               # Business logic
│   ├── a2a_client.py      # Kagent A2A protocol client (JSON-RPC 2.0)
│   ├── agent_discovery.py # Agent discovery from API
│   └── agent_router.py    # Agent routing logic
├── auth/                   # Authorization
│   ├── permissions.py     # RBAC permissions checker
│   └── slack_groups.py    # Slack user group integration
└── slack/                  # Slack utilities
    ├── formatters.py      # Block Kit formatting
    └── validators.py      # Input validation
```

### Type Checking

```bash
.venv/bin/mypy src/kagent_slackbot/
```

### Linting

```bash
.venv/bin/ruff check src/
```

Auto-fix issues:
```bash
.venv/bin/ruff check --fix src/
```

## Deployment

The Slackbot is deployed via the kagent Helm chart.

### Prerequisites

1. **Kubernetes cluster** with Kagent installed (or installing for the first time)
2. **Helm 3.x** installed
3. **Slack App configured** with Socket Mode (see [SLACK_SETUP.md](./SLACK_SETUP.md))
4. **Slack API tokens** obtained (Bot Token, App Token, Signing Secret)

### Install with Helm

```bash
helm upgrade --install kagent ./helm/kagent \
  --namespace kagent \
  --create-namespace \
  --set slackbot.enabled=true \
  --set slackbot.secrets.slackBotToken="xoxb-your-token" \
  --set slackbot.secrets.slackAppToken="xapp-your-token" \
  --set slackbot.secrets.slackSigningSecret="your-secret"
```

Or use a values file:

```yaml
# slackbot-values.yaml
slackbot:
  enabled: true
  secrets:
    slackBotToken: "xoxb-..."
    slackAppToken: "xapp-..."
    slackSigningSecret: "..."
  permissions:
    agent_permissions:
      kagent/k8s-agent:
        user_groups: ["S0T8FCWSB"]
```

```bash
helm upgrade --install kagent ./helm/kagent -f slackbot-values.yaml
```

### Verify Deployment

```bash
kubectl get pods -n kagent -l app.kubernetes.io/component=slackbot
kubectl logs -f -n kagent -l app.kubernetes.io/component=slackbot
```

### Configuration Options

See `helm/kagent/values.yaml` for all configuration options including:
- Autoscaling (HPA)
- Pod Disruption Budget
- RBAC permissions for agents
- Resource limits
- Node selectors and tolerations

### Monitoring

**Prometheus Metrics** available at `/metrics`:
- `slack_messages_total{event_type, status}` - Total messages processed
- `slack_message_duration_seconds{event_type}` - Message processing time
- `slack_commands_total{command, status}` - Slash command counts
- `agent_invocations_total{agent, status}` - Agent invocation counts

**Health Endpoints**:
- `/health` - Liveness probe
- `/ready` - Readiness probe

**Structured Logs**: JSON format with fields:
- `event`: Log message
- `level`: Log level (INFO, ERROR, etc.)
- `timestamp`: ISO 8601 timestamp
- `user`, `agent`, `session`: Contextual fields

## Troubleshooting

### Bot doesn't respond to @mentions

1. Check bot is online: `kubectl logs -n kagent deployment/slackbot`
2. Verify Socket Mode connection is established (look for "Connecting to Slack via Socket Mode")
3. Ensure Slack app has `app_mentions:read` scope
4. Check event subscription for `app_mention` is configured

### Agent discovery fails

1. Verify Kagent is accessible: `curl http://kagent-controller.kagent.svc.cluster.local:8083/api/agents`
2. Check logs for "Agent discovery failed" messages
3. Ensure `KAGENT_BASE_URL` is configured correctly

### Type errors during development

Run type checking:
```bash
.venv/bin/mypy src/kagent_slackbot/
```

Common issues:
- Missing type annotations - add explicit types
- Untyped external libraries - use `# type: ignore[no-untyped-call]`

## References

- **Slack Bolt Docs**: https://slack.dev/bolt-python/
- **Kagent A2A Protocol**: `go/internal/a2a/`
- **Agent CRD Spec**: `go/api/v1alpha2/agent_types.go`
