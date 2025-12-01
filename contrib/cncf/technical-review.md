# General Technical Review - kagent / Incubation

_This document provides a General Technical Review of the kagent project. This is a living document that demonstrates to the Technical Advisory Group (TAG) that the project satisfies the Engineering Principle requirements for moving levels. This document follows the template outlined [in the TOC subproject review](https://github.com/cncf/toc/blob/main/toc_subprojects/project-reviews-subproject/general-technical-questions.md)_

- **Project:** kagent
- **Project Version:** v0.7.5
- **Website:** [https://kagent.dev](https://kagent.dev)
- **Date Updated:** 2025-12-01
- **Template Version:** v1.0
- **Description:** kagent is a Kubernetes native framework for building AI agents. Kubernetes is the most popular orchestration platform for running workloads, and **kagent** makes it easy to build, deploy and manage AI agents in Kubernetes. The **kagent** framework is designed to be easy to understand and use, and to provide a flexible and powerful way to build and manage AI agents.

## Day 0 - Planning Phase

### Scope

**Roadmap Process:**
Kagent's roadmap is managed through a public GitHub project board at [https://github.com/orgs/kagent-dev/projects/3](https://github.com/orgs/kagent-dev/projects/3). The roadmap process includes:

- Features are proposed through GitHub issues and design documents (see [design template](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/design/template.md))
- Significant features require design documents following the enhancement proposal process (e.g., [EP-685-kmcp](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/design/EP-685-kmcp.md))
- Community input is gathered through Discord (https://discord.gg/Fu3k65f2k3), Slack (#kagent-dev on CNCF Slack), and community meetings
- The maintainer ladder is defined in [CONTRIBUTION.md](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/CONTRIBUTION.md), with clear paths from contributor to maintainer based on sustained contributions

**Target Personas:**

1. **Platform Engineers**: Building and maintaining internal developer platforms with AI-powered automation
2. **DevOps/SRE Teams**: Automating operational tasks, troubleshooting, and incident response in Kubernetes environments
3. **Kubernetes Administrators**: Managing complex multi-cluster environments with intelligent agents
4. **Application Developers**: Building AI-powered applications that need to interact with Kubernetes infrastructure

**Primary Use Case:**
The primary use case is enabling AI-powered automation and intelligent operations for Kubernetes clusters. This includes:

- **Kubernetes-Native AI Agents**: Provide a framework for building AI agents that operate naturally within Kubernetes environments with full integration of Kubernetes security models.
- **Secure Multi-Tenancy**: Enable multiple users and teams to deploy and manage their own agents with proper isolation and access controls. This is not yet implemented, but is on the project roadmap.
- **Extensible Tool Ecosystem**: Offer a secure and extensible system for agents to access various tools and services while maintaining proper authorization boundaries.
- **Declarative Configuration**: Enable infrastructure-as-code practices for agent deployment and management with version control and review processes.

**Additional Supported Use Cases:**

- Multi-agent coordination for complex operational workflows (via A2A protocol)
- Integration with service mesh (Istio), observability (Prometheus/Grafana), and deployment tools (Helm, Argo Rollouts)
- Custom agent development using multiple frameworks (ADK, CrewAI, LangGraph)

**Unsupported Use Cases:**

- **Direct Cluster Administration**: kagent does not replace Kubernetes RBAC or cluster security policies; it operates within existing security boundaries.
- **LLM Model Hosting**: kagent does not host or provide LLM models; it integrates with external model providers.

**Target Organizations:**

- **Telecommunications**: Companies like Amdocs managing complex infrastructure requiring intelligent monitoring and malicious user detection
- **Financial Services**: Organizations requiring secure, auditable AI-powered operations with strict compliance requirements
- **Identity Verification**: Companies like Au10tix needing reliable, secure automation for critical verification platforms
- **Platform Engineering Teams**: Organizations like Krateo providing cloud-native platform solutions to internal teams and customers
- **Any organization** running production Kubernetes workloads seeking to reduce operational overhead through intelligent automation

**End User Research:**
Current case studies are documented in the [security self-assessment](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/contrib/cncf/security-self-assessment.md#case-studies), including deployments at Amdocs, Au10tix, and Krateo. Formal user research reports are planned as the project matures toward v1.0.

### Usability

**Interaction Methods:**
Target personas interact with kagent through multiple interfaces:

1. **Web UI**: A modern Next.js-based dashboard providing:
   - Visual agent management and configuration
   - Real-time conversation monitoring and session history
   - Tool and model configuration interfaces
   - Observability dashboards with metrics and traces

2. **CLI (`kagent`)**: Command-line tool for:
   - Agent deployment and management (`kagent agent deploy`)
   - MCP server configuration (`kagent mcp`)
   - Local development workflows
   - CI/CD integration
   - Installation: `curl -fsSL https://kagent.dev/install.sh | sh`

3. **Kubernetes API**: Direct interaction via `kubectl` and Kubernetes manifests:
   ```yaml
   apiVersion: kagent.dev/v1alpha2
   kind: Agent
   metadata:
     name: my-agent
   spec:
     type: Declarative
     declarative:
       systemMessage: "You are a helpful Kubernetes assistant"
       tools: [...]
   ```

4. **HTTP REST API**: Programmatic access at `http://kagent-controller:8083/api` for:
   - Agent invocation and management
   - Session and task tracking
   - Model configuration
   - Integration with external systems

5. **A2A Protocol**: Agent-to-agent communication following the [Google A2A specification](https://github.com/google/A2A) for multi-agent workflows

**User Experience:**

- **Declarative Configuration**: Infrastructure-as-code approach using Kubernetes CRDs
- **Quick Start**: Get running in minutes with Helm: `helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent`
- **Progressive Disclosure**: Start simple with default configurations, customize as needed
- **Observability First**: Built-in OpenTelemetry tracing and metrics from day one
- **Documentation**: Comprehensive docs at [https://kagent.dev/docs/kagent](https://kagent.dev/docs/kagent) with tutorials, API references, and examples

**Integration with Other Projects:**
Kagent integrates seamlessly with cloud-native ecosystem projects:

- **Kubernetes**: Native CRDs, RBAC integration, standard deployment patterns
- **Helm**: Official Helm charts for installation and upgrades
- **OpenTelemetry**: Distributed tracing for agent operations and tool invocations
- **Prometheus**: Metrics exposure for monitoring agent health and performance
- **Grafana**: Pre-built dashboards and MCP tools for visualization
- **Istio**: Service mesh integration for traffic management and security
- **Argo Rollouts**: Progressive delivery integration for agent deployments
- **Cilium**: eBPF-based networking and security policy management
- **LLM Providers**: OpenAI, Anthropic, Azure OpenAI, Google Vertex AI, Ollama, and custom models via AI gateways
- **MCP Ecosystem**: Extensible tool system compatible with Model Context Protocol servers

### Design

**Design Principles:**
Kagent follows these core design principles (documented in [README.md](https://github.com/kagent-dev/kagent#core-principles)):

- **Kubernetes Native**: Kagent is designed to be easy to understand and use, and to provide a flexible and powerful way to build and manage AI agents.
- **Extensible**: Kagent is designed to be extensible, so you can add your own agents and tools.
- **Flexible**: Kagent is designed to be flexible, to suit any AI agent use case.
- **Observable**: Kagent is designed to be observable, so you can monitor the agents and tools using all common monitoring frameworks.
- **Declarative**: Kagent is designed to be declarative, so you can define the agents and tools in a YAML file.
- **Testable**: Kagent is designed to be tested and debugged easily. This is especially important for AI agent applications.

**Architecture:**

Core components:

- **Controller**: The controller is a Kubernetes controller that watches the kagent custom resources and creates the necessary resources to run the agents.
- **UI**: The UI is a web UI that allows you to manage the agents and tools.
- **Engine**: The engine runs your agents using [ADK](https://google.github.io/adk-docs/).
- **CLI**: The CLI is a command-line tool that allows you to manage the agents and tools.

<div align="center">
  <img src="img/arch.png" alt="kagent" width="500">
</div>

**Environment Differences:**

- **Development**: 
  - Kind cluster with local registry
  - SQLite database
  - Single replica deployments
  - Debug logging enabled
  - See [DEVELOPMENT.md](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/DEVELOPMENT.md)

- **Test/CI**:
  - Automated Kind cluster creation
  - Mock LLM servers for deterministic testing
  - Ephemeral resources cleaned up after tests
  - See [.github/workflows/ci.yaml](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/.github/workflows/ci.yaml)

- **Production**:
  - PostgreSQL database recommended for persistence
  - Multi-replica controller deployments for HA
  - Resource limits enforced
  - TLS for external LLM connections
  - Network policies for pod-to-pod communication
  - See [helm/kagent/values.yaml](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/helm/kagent/values.yaml)

**Service Dependencies:**
Required in-cluster:

- **Kubernetes API Server**: Core dependency for all operations
- **etcd**: Via Kubernetes for state storage
- **DNS**: Kubernetes CoreDNS for service discovery

Optional in-cluster:

- **PostgreSQL**: For production database (SQLite default for development)
- **Qdrant**: For vector memory storage (optional feature)
- **KMCP**: For building and managing MCP servers (enabled by default)
- **Prometheus**: For metrics collection (optional)
- **Jaeger/OTLP Collector**: For distributed tracing (optional)

External dependencies:

- **LLM Providers**: OpenAI, Anthropic, Azure OpenAI, Google Vertex AI, or Ollama (user-configured)

**Identity and Access Management:**
Kagent implements a multi-layered IAM approach:

1. **Kubernetes RBAC**:
   - Controller uses ServiceAccount with ClusterRole for CRD management
   - Agents receive individual ServiceAccounts with configurable RBAC permissions
   - Example roles in [go/config/rbac/role.yaml](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/go/config/rbac/role.yaml)
   - Per-agent RBAC templates in [helm/agents/*/templates/rbac.yaml](https://github.com/kagent-dev/kagent/tree/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/helm/agents)

2. **API Authentication** (planned enhancement - [Issue #476](https://github.com/kagent-dev/kagent/issues/476)):
   - Current: UnsecureAuthenticator for development, A2AAuthenticator for agent-to-agent
   - Planned: Extensible authentication system with support for API keys, OAuth, and service accounts
   - Framework in [go/pkg/auth/auth.go](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/go/pkg/auth/auth.go)

3. **Secret Management**:
   - LLM API keys stored in Kubernetes Secrets
   - Secrets mounted as environment variables or files
   - No cross-namespace secret access (potential future enhancement via ReferenceGrant)
   - Secrets managed via [go/internal/utils/secret.go](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/go/internal/utils/secret.go)

4. **Session Isolation** (roadmap - [Issue #476](https://github.com/kagent-dev/kagent/issues/476)):
   - Database-backed session management
   - Per-user and per-agent session tracking
   - Planned: Full multi-tenancy with namespace-based isolation

**Sovereignty:**
Kagent addresses data sovereignty through:

- **On-Premises Deployment**: Full support for air-gapped and on-premises Kubernetes clusters
- **LLM Provider Choice**: Support for self-hosted models via Ollama or custom endpoints
- **Data Residency**: All operational data stored in user-controlled databases (SQLite/PostgreSQL)
- **No Phone-Home**: No telemetry or data sent to kagent maintainers
- **Regional LLM Endpoints**: Support for region-specific LLM endpoints (e.g., Azure OpenAI regional deployments)

**Compliance:**

- **Apache 2.0 License**: Clear open-source [licensing](LICENSE.md)
- **OpenSSF Best Practices**: Badge at [https://www.bestpractices.dev/projects/10723](https://www.bestpractices.dev/projects/10723)
- **Dependency Scanning**: Automated [CVE scanning via Trivy in CI/CD](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/.github/workflows/image-scan.yaml)
- **SBOM Generation**: Part of the [future state of the project](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/contrib/cncf/security-self-assessment.md#future-state)
- **Audit Logging**: Comprehensive logging of all agent operations and API calls
- **Security Self-Assessment**: Available at [contrib/cncf/security-self-assessment.md](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/contrib/cncf/security-self-assessment.md)

**High Availability:**

- **Controller**: Supports multi-replica deployments with leader election (via controller-runtime)
- **Agents**: Configurable replica counts per agent (default: 1, can scale horizontally)
- **Database**: Supports PostgreSQL with HA configurations (replication, failover)
- **Stateless Design**: Controllers and agents are stateless, state in Kubernetes API and database
- **Rolling Updates**: Zero-downtime upgrades via Kubernetes rolling deployment strategy
- **Health Checks**: Liveness and readiness probes on all components

**Resource Requirements:**

Default per-component requirements (from [helm/kagent/values.yaml](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/helm/kagent/values.yaml)):

Controller:

- CPU: 100m request, 2000m limit
- Memory: 128Mi request, 512Mi limit
- Network: ClusterIP service on port 8083

UI:

- CPU: 100m request, 1000m limit
- Memory: 256Mi request, 1Gi limit
- Network: ClusterIP/LoadBalancer on port 8080

Per Agent (default):

- CPU: 100m request, 1000m limit
- Memory: 256Mi request, 1Gi limit (384Mi-1Gi depending on agent type)
- Network: ClusterIP service on port 8080

**Storage Requirements:**

Ephemeral Storage:

- Container images: ~500MB per component (controller, UI, agent base images)
- Temporary files: Minimal, used for skill loading and code execution sandboxes
- Logs: Configurable retention

Persistent Storage:

- **Database** (optional, SQLite vs PostgreSQL):
  - SQLite: 10MB-1GB depending on session history (ephemeral, lost on pod restart)
  - PostgreSQL: 1GB-100GB+ depending on retention policies and usage
  - Stores: sessions, tasks, events, feedback, agent memory, checkpoints
- **Vector Memory** (optional, Qdrant):
  - 100MB-10GB+ depending on document corpus size
  - Used for RAG and long-term agent memory

**API Design:**

**API Topology:**
Kagent exposes multiple API surfaces:

1. **Kubernetes API** (CRDs):
   - `agents.kagent.dev/v1alpha2` - Agent definitions
   - `modelconfigs.kagent.dev/v1alpha2` - LLM model configurations
   - `toolservers.kagent.dev/v1alpha1` - MCP tool server definitions
   - `remotemcpservers.kagent.dev/v1alpha2` - Remote MCP servers
   - `memories.kagent.dev/v1alpha1` - Memory/vector store configurations
   - `mcpservers.kagent.dev` (inherited via KMCP dependency)

2. **HTTP REST API** (controller):
   - Base path: `/api`
   - Endpoints: `/agents`, `/sessions`, `/modelconfigs`, `/tools`, `/feedback`, etc.
   - Format: JSON request/response
   - Authentication: Pluggable (currently development mode)
   - See [go/internal/httpserver/server.go](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/go/internal/httpserver/server.go)

3. **A2A Protocol** (per-agent):
   - Path: `/api/a2a/{namespace}/{agent-name}`
   - Spec: https://github.com/google/A2A
   - Supports streaming and synchronous invocations

**API Conventions:**

- RESTful resource naming (plural nouns)
- Standard HTTP methods (GET, POST, PUT, DELETE)
- JSON for all request/response bodies
- Kubernetes-style metadata (namespace, name, labels, annotations)
- Status subresources for CRDs following Kubernetes conventions

**Defaults:**

Default values can be found in [helm/kagent/values.yaml](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/helm/kagent/values.yaml):

- Default model provider: OpenAI (configurable via `providers.default` in Helm)
- Default model: `gpt-4.1-mini` for OpenAI
- Default namespace: `kagent`
- Default database: SQLite (ephemeral)
- Default agent type: `Declarative`
- Default streaming: `true`
- Default resource requests: 100m CPU, 256Mi memory

**Additional Configurations:**
For production use, configure:

- PostgreSQL database connection (`database.type=postgres`, `database.postgres.url`)
- LLM API keys via Secrets (`providers.openAI.apiKeySecretRef`)
- TLS for external LLM connections (`modelConfig.tls`)
- Resource limits based on workload (`agents.*.resources`)
- OpenTelemetry endpoints (`otel.tracing.enabled`, `otel.tracing.exporter.otlp.endpoint`)
- Network policies for pod isolation
- RBAC policies per agent based on required permissions

**New API Types:**
Kagent introduces these Kubernetes API types:

- `Agent`: Defines an AI agent with tools, model config, and deployment spec
- `ModelConfig`: LLM provider configuration with credentials and parameters
- `RemoteMCPServer`: External MCP tool server registration
- `Memory`: Vector store configuration for agent memory
- `ToolServer`: MCP tool server registration

These do not modify existing Kubernetes APIs or cloud provider APIs.

**API Compatibility:**

- **Kubernetes API Server**: Compatible with Kubernetes 1.27+ (uses standard CRD and controller-runtime patterns)
- **API Versioning**: Currently `v1alpha2` for core types, `v1alpha1` for memory types
- **Backward Compatibility**: Breaking changes allowed in alpha versions, will stabilize in v1beta1 and v1
- **Conversion Webhooks**: Planned for v1beta1 to support multiple API versions simultaneously

**API Versioning and Breaking Changes:**

- **Alpha** (`v1alpha1`, `v1alpha2`): Breaking changes allowed between versions, deprecated APIs removed after 1-2 releases
- **Beta** (planned `v1beta1`): Breaking changes discouraged, deprecated APIs supported for 2+ releases
- **Stable** (planned `v1`): Strong backward compatibility guarantees, deprecated APIs supported for 3+ releases
- **Deprecation Policy**: Follows Kubernetes deprecation policy - announcements in release notes, migration guides provided
- **Version Skew**: Controller supports N and N-1 API versions during transitions

**Release Process:**
Kagent follows semantic versioning ([https://semver.org/](https://semver.org/)):

- **Major Releases** (x.0.0): Breaking API changes, major new features
  - Require migration guides
  - Example: v1.0.0 (planned)

- **Minor Releases** (0.x.0): New features, non-breaking changes
  - Monthly cadence (target)
  - New agent types, tool integrations, LLM providers
  - Backward compatible within major version

- **Patch Releases** (0.0.x): Bug fixes, security patches
  - As needed, typically weekly for active issues
  - No new features
  - Always backward compatible

Release artifacts:

- Container images: `cr.kagent.dev/kagent-dev/kagent/*`
- Helm charts: `oci://ghcr.io/kagent-dev/kagent/helm/*`
- CLI binaries: GitHub releases
- Release notes: GitHub releases with changelog

Release process (managed by maintainers):

1. Version bump in relevant files
2. Create release branch
3. Run full CI/CD pipeline including E2E tests
4. Build and push multi-arch container images
5. Package and publish Helm charts
6. Create GitHub release with notes
7. Update documentation site

See [CONTRIBUTION.md](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/CONTRIBUTION.md#releasing) for details.

### Installation

**Installation Methods:**

Kagent provides multiple installation paths to suit different use cases. See https://kagent.dev/docs/kagent/introduction/installation for end-user details. There are also more detailed developer installation guides for each method below.

**1. Helm Installation (Recommended for Production):**

See [helm/README.md](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/helm/README.md#using-helm) for more details.

**2. CLI Installation (Quickest for Getting Started):**

See [README.md](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/helm/README.md#using-kagent-cli) for more details.


**3. Local Development (Kind):**

See [README.md](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/helm/README.md#using-make) for more details.


**Configuration Requirements:**

- **Minimal**: Kubernetes cluster (1.27+), LLM API key
- **Optional**: PostgreSQL for persistence, Prometheus/Grafana for observability, custom RBAC policies

**Initialization:**

After installation, kagent automatically:

1. Deploys controller, UI, and default agents
2. Creates default ModelConfig from Helm values
3. Registers KMCP tool server (if enabled)
4. Starts health check endpoints

No manual initialization steps required beyond providing LLM credentials.

**Installation Validation:**

**1. Check Pod Status:**

```bash
kubectl get pods -n kagent
# Expected: All pods Running

kubectl wait --for=condition=Ready pods --all -n kagent --timeout=120s
```

**2. Verify CRDs:**

```bash
kubectl get crds | grep kagent.dev
# Expected: agents.kagent.dev, modelconfigs.kagent.dev, etc.
```

**3. Check Agents:**

```bash
kubectl get agents -n kagent
# Expected: Default agents (k8s-agent, observability-agent, etc.) with Ready=True
```

**4. Test API:**

```bash
# Port-forward controller
kubectl port-forward svc/kagent-controller 8083:8083 -n kagent

# Check version
curl http://localhost:8083/version
# Expected: {"kagent_version":"v0.x.x","git_commit":"...","build_date":"..."}

# List agents
curl http://localhost:8083/api/agents
```

**5. Test Agent Invocation:**

```bash
# Using CLI
kagent agent invoke k8s-agent "List all namespaces"

# Or via UI
# Navigate to http://localhost:8001 (after port-forward)
# Select agent and send message
```

**6. Check Logs:**

```bash
# Controller logs
kubectl logs -n kagent deployment/kagent-controller --tail=50

# Agent logs
kubectl logs -n kagent deployment/k8s-agent --tail=50
```

**7. Validate Observability (if enabled):**

```bash
# Check metrics endpoint
kubectl port-forward svc/kagent-controller 8083:8083 -n kagent
curl http://localhost:8083/metrics

# Check traces in Jaeger (if configured)
# Navigate to Jaeger UI and search for kagent traces
```

**Troubleshooting:**
Common issues and solutions documented at:
- [DEVELOPMENT.md#troubleshooting](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/DEVELOPMENT.md#troubleshooting)
- Helm README: [helm/README.md](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/helm/README.md)

**Quick Start Guide:**

https://kagent.dev/docs/kagent/getting-started/quickstart

### Security

**Security Self-Assessment:**
Kagent's comprehensive security self-assessment is available at:
[contrib/cncf/security-self-assessment.md](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/contrib/cncf/security-self-assessment.md)

**Cloud Native Security Tenets:**

Kagent satisfies the [Cloud Native Security Tenets](https://github.com/cncf/tag-security/blob/main/community/resources/security-whitepaper/secure-defaults-cloud-native-8.md) as follows:

1. **Secure by Default:**
   - RBAC enforced by default for all agents
   - Secrets required for LLM API keys (no plaintext credentials)
   - Network policies supported out-of-box
   - No privileged containers by default
   - TLS support for external LLM connections

2. **Defense in Depth:**
   - Multiple security layers: Kubernetes RBAC, namespace isolation, secret management, network policies
   - Container security scanning (Trivy) in CI/CD
   - Audit logging of all agent operations
   - Session isolation in database

3. **Least Privilege:**
   - Controller runs with minimal RBAC permissions (see [go/config/rbac/role.yaml](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/go/config/rbac/role.yaml))
   - Each agent gets individual ServiceAccount with scoped permissions
   - No cluster-admin privileges required
   - Agents cannot access secrets in other namespaces

4. **Immutable Infrastructure:**
   - Container images are immutable
   - Configuration via Kubernetes manifests (GitOps compatible)
   - No runtime modification of agent code
   - Declarative agent definitions

5. **Auditable:**
   - All API calls logged
   - Agent operations tracked in database
   - OpenTelemetry traces for complete request flow
   - Kubernetes audit logs capture CRD changes

6. **Automated:**
   - Automated vulnerability scanning in CI/CD
   - Automated testing including security scenarios
   - Dependency updates via Dependabot (in-progress: [https://github.com/kagent-dev/kagent/pull/958](https://github.com/kagent-dev/kagent/pull/958))

7. **Segregated:**
   - Namespace-based isolation
   - Per-agent RBAC policies
   - Network policies for pod-to-pod communication
   - Database session isolation (planned full multi-tenancy)

8. **Hardened:**
   - Minimal container base images
   - No unnecessary packages or tools
   - Non-root user execution where possible
   - Read-only root filesystems supported

**Loosening Security Defaults:**

For development or specific use cases, users may need to relax security:

1. **Development Mode Authentication:**
   - Default: UnsecureAuthenticator (no auth checks)
   - Production: Configure proper authentication via [Issue #476](https://github.com/kagent-dev/kagent/issues/476)
   - Documentation: Planned for v1.0 release

2. **Expanded RBAC Permissions:**
   - Default: Read-only access to most resources
   - Custom: Edit agent RBAC templates in [helm/agents/*/templates/rbac.yaml](https://github.com/kagent-dev/kagent/tree/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/helm/agents)
   - Example: Grant write access for agents that need to modify resources

3. **Cross-Namespace Access:**
   - Default: Agents can only access resources in their namespace
   - Custom: Use ClusterRole instead of Role for cluster-wide access
   - Warning: Increases security risk, use with caution

4. **TLS Verification:**
   - Default: TLS verification enabled for external connections
   - Custom: Disable via `modelConfig.tls.insecureSkipVerify: true` (not recommended)
   - Use case: Self-signed certificates in development

5. **Network Policies:**
   - Default: No network policies (Kubernetes default-allow)
   - Recommended: Apply network policies to restrict pod-to-pod traffic
   - Example policies: To be documented

Documentation for security configuration: https://kagent.dev/docs/kagent (security section planned)

**Security Hygiene:**

**Frameworks and Practices:**

1. **Code Review**: All PRs require maintainer review before merge
2. **Automated Testing**: Unit, integration, and E2E tests in CI/CD
3. **Vulnerability Scanning**:
   - Trivy scans for container images
   - `govulncheck` for Go dependencies
   - `uv run pip-audit` for Python dependencies
   - `npm audit` for UI dependencies
   - Run via `make audit` (see [Makefile](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/Makefile))
4. **Dependency Management**:
   - Go modules with version pinning
   - Python uv.lock for reproducible builds
   - npm package-lock.json
   - Regular dependency updates
5. **Signed Commits**: DCO (Developer Certificate of Origin) required
6. **Security Policy**: [SECURITY.md](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/SECURITY.md) with responsible disclosure process
7. **OpenSSF Best Practices**: Badge at https://www.bestpractices.dev/projects/10723

**Security Risk Evaluation:**
Features evaluated for security risk:

- **Agent Code Execution**: Sandboxed Python code execution for `executeCodeBlocks` feature
- **Tool Invocation**: RBAC-controlled access to Kubernetes APIs and external services
- **Secret Access**: Scoped to agent's namespace, no cross-namespace access
- **Database Access**: Session isolation prevents cross-user data access
- **A2A Communication**: Authentication framework for agent-to-agent calls

Ongoing evaluation via:

- Security issue triage (severity-based prioritization)
- Community security reports via kagent-vulnerability-reports@googlegroups.com

**Cloud Native Threat Modeling:**

**Minimal Privileges:**
Controller requires:

- **Read/Write**: `agents`, `modelconfigs`, `toolservers`, `memories`, `remotemcpservers`, `mcpservers` (kagent.dev API group)
- **Read/Write**: `deployments`, `services`, `configmaps`, `secrets`, `serviceaccounts` (for agent lifecycle)
- **Read**: All other resources (for status reporting and validation)

Agents require (configurable per agent):

- **Read**: Kubernetes resources relevant to their function (e.g., k8s-agent needs read access to pods, deployments, etc.)
- **Write**: Only for agents that modify resources (e.g., helm-agent needs write access for releases)
- **Execute**: Tool invocation via MCP servers

Reasons for privileges:

- Controller needs write access to create/update agent deployments and services
- Agents need read access to perform their operational tasks
- Write access for agents is optional and scoped to specific use cases

**Certificate Rotation:**

- **LLM Connections**: TLS certificates for external LLM providers are managed by the provider
- **In-Cluster**: Kubernetes handles certificate rotation for service-to-service communication
- **Custom CA**: Support for custom CA certificates via `modelConfig.tls.caCert` (base64-encoded PEM)
- **Certificate Expiry**: No automatic rotation, users must update secrets when certificates expire
- **Planned**: Automatic certificate rotation via cert-manager integration (roadmap item)

**Secure Software Supply Chain:**

Kagent follows [CNCF SSCP best practices](https://project.linuxfoundation.org/hubfs/CNCF_SSCP_v1.pdf):

1. **Source Code Management:**
   - Public GitHub repository with branch protection
   - Required code reviews for all changes
   - Signed commits via DCO
   - No force-push to main branch

2. **Build Process:**
   - Reproducible builds via Docker multi-stage builds
   - Build provenance tracked (version, git commit, build date)
   - Automated builds in GitHub Actions (no manual builds)
   - Build logs publicly available

3. **Artifact Management:**
   - Container images signed (planned via Cosign)
   - SBOM generation (planned for v1.0)
   - Multi-architecture builds (amd64, arm64)
   - Immutable tags (version-based, no `latest` in production)

4. **Dependency Management:**
   - Lock files for all dependencies (go.sum, uv.lock, package-lock.json)
   - Automated vulnerability scanning
   - Dependabot for security updates
   - Minimal dependencies (reduce attack surface)

5. **Testing:**
   - Comprehensive test suite (unit, integration, E2E)
   - Security-focused tests (RBAC, secret handling, TLS)
   - Mock LLM servers for deterministic testing
   - Test coverage tracking

6. **Release Process:**
   - Semantic versioning
   - Release notes with security advisories
   - Changelog generation
   - Signed releases (planned)

7. **Monitoring:**
   - CVE scanning in CI/CD (blocks on high/critical)
   - OpenSSF Scorecard (planned)
   - Security advisories via GitHub Security

**Planned Enhancements:**
See [security self-assessment](https://github.com/kagent-dev/kagent/blob/9438c9c0f2c79daf632555df1d7d3cb2d04b7b81/contrib/cncf/security-self-assessment.md#future-state) for details:

- SLSA provenance attestations
- Cosign image signing
- SBOM in SPDX/CycloneDX format


## Day 1 \- Installation and Deployment Phase

_Coming Soon_
