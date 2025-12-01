# General Technical Review - kagent / Incubation

_This document provides a General Technical Review of the kagent project. This is a living document that demonstrates to the Technical Advisory Group (TAG) that the project satisfies the Engineering Principle requirements for moving levels. This document follows the template outlined [in the TOC subproject review](https://github.com/cncf/toc/blob/main/toc_subprojects/project-reviews-subproject/general-technical-questions.md)_

- **Project:** kagent
- **Project Version:** v0.7.4
- **Website:** [https://kagent.dev](https://kagent.dev)
- **Date Updated:** 2025-11-30
- **Template Version:** v1.0
- **Description:** Kagent is an innovative AI agent platform designed specifically for Kubernetes environments. It empowers developers and operations teams to create intelligent, autonomous agents that can monitor, manage, and automate complex Kubernetes workloads using the power of large language models (LLMs).

## Day 0 - Planning Phase

### Scope

**Roadmap Process:**
Kagent's roadmap is managed through a public GitHub project board at https://github.com/orgs/kagent-dev/projects/3. The roadmap process includes:
- Features are proposed through GitHub issues and design documents (see [design template](https://github.com/kagent-dev/kagent/blob/main/design/template.md))
- Significant features require design documents following the enhancement proposal process (e.g., [EP-685-kmcp](https://github.com/kagent-dev/kagent/blob/main/design/EP-685-kmcp.md))
- Community input is gathered through Discord (https://discord.gg/Fu3k65f2k3), Slack (#kagent-dev on CNCF Slack), and community meetings
- The maintainer ladder is defined in [CONTRIBUTION.md](https://github.com/kagent-dev/kagent/blob/main/CONTRIBUTION.md), with clear paths from contributor to maintainer based on sustained contributions

**Target Personas:**
1. **Platform Engineers**: Building and maintaining internal developer platforms with AI-powered automation
2. **DevOps/SRE Teams**: Automating operational tasks, troubleshooting, and incident response in Kubernetes environments
3. **Kubernetes Administrators**: Managing complex multi-cluster environments with intelligent agents
4. **Application Developers**: Building AI-powered applications that need to interact with Kubernetes infrastructure

**Primary Use Case:**
The primary use case is enabling AI-powered automation and intelligent operations for Kubernetes clusters. This includes:
- Automated troubleshooting and diagnostics of cluster issues
- Intelligent resource management and optimization
- Natural language interfaces for cluster operations
- Automated compliance checking and security monitoring

**Additional Supported Use Cases:**
- Multi-agent coordination for complex operational workflows (via A2A protocol)
- Integration with service mesh (Istio), observability (Prometheus/Grafana), and deployment tools (Helm, Argo Rollouts)
- Custom agent development using multiple frameworks (ADK, CrewAI, LangGraph)
- Document querying and knowledge management for operational documentation

**Unsupported Use Cases:**
- LLM model training or fine-tuning (kagent integrates with existing model providers)
- General-purpose AI/ML workload orchestration (focus is on operational agents, not ML pipelines)
- Direct cluster administration bypassing Kubernetes RBAC (kagent operates within existing security boundaries)
- Real-time streaming data processing at scale (agents are designed for operational tasks, not data pipelines)

**Target Organizations:**
- **Telecommunications**: Companies like Amdocs managing complex infrastructure requiring intelligent monitoring and malicious user detection
- **Financial Services**: Organizations requiring secure, auditable AI-powered operations with strict compliance requirements
- **Identity Verification**: Companies like Au10tix needing reliable, secure automation for critical verification platforms
- **Platform Engineering Teams**: Organizations like Krateo providing cloud-native platform solutions to internal teams and customers
- **Any organization** running production Kubernetes workloads seeking to reduce operational overhead through intelligent automation

**End User Research:**
Current case studies are documented in the [security self-assessment](https://github.com/kagent-dev/kagent/blob/main/contrib/cncf/security-self-assessment.md#case-studies), including deployments at Amdocs, Au10tix, and Krateo. Formal user research reports are planned as the project matures toward v1.0.

### Usability

**Interaction Methods:**
Target personas interact with kagent through multiple interfaces:

1. **Web UI**: A modern Next.js-based dashboard (http://localhost:8080 after installation) providing:
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
- **Documentation**: Comprehensive docs at https://kagent.dev/docs/kagent with tutorials, API references, and examples

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

1. **Kubernetes Native**: Leverages Kubernetes primitives (CRDs, RBAC, Services) for all operations
2. **Declarative**: All configuration via YAML manifests, enabling GitOps workflows
3. **Extensible**: Plugin architecture via MCP servers for tools, support for multiple agent frameworks
4. **Observable**: OpenTelemetry integration for tracing, metrics, and logging from the ground up
5. **Flexible**: Support for multiple LLM providers, agent types (Declarative, BYO), and deployment patterns
6. **Testable**: Comprehensive testing including unit, integration, and E2E tests with mock LLM servers

**Architecture:**
Architecture diagram: https://github.com/kagent-dev/kagent/blob/main/img/arch.png

Core components:
- **Controller** (Go): Kubernetes operator managing CRD lifecycle, written in controller-runtime
- **Engine** (Python): Agent runtime using Google ADK, CrewAI, or LangGraph
- **UI** (TypeScript/Next.js): Web dashboard for management and monitoring
- **CLI** (Go): Command-line tool for development and deployment

**Environment Differences:**
- **Development**: 
  - Kind cluster with local registry
  - SQLite database
  - Single replica deployments
  - Debug logging enabled
  - See [DEVELOPMENT.md](https://github.com/kagent-dev/kagent/blob/main/DEVELOPMENT.md)

- **Test/CI**:
  - Automated Kind cluster creation
  - Mock LLM servers for deterministic testing
  - Ephemeral resources cleaned up after tests
  - See [.github/workflows/ci.yaml](https://github.com/kagent-dev/kagent/blob/main/.github/workflows/ci.yaml)

- **Production**:
  - PostgreSQL database recommended for persistence
  - Multi-replica controller deployments for HA
  - Resource limits enforced
  - TLS for external LLM connections
  - Network policies for pod-to-pod communication
  - See [helm/kagent/values.yaml](https://github.com/kagent-dev/kagent/blob/main/helm/kagent/values.yaml)

**Service Dependencies:**
Required in-cluster:
- **Kubernetes API Server**: Core dependency for all operations
- **etcd**: Via Kubernetes for state storage
- **DNS**: Kubernetes CoreDNS for service discovery

Optional in-cluster:
- **PostgreSQL**: For production database (SQLite default for development)
- **Qdrant**: For vector memory storage (optional feature)
- **KMCP**: Kubernetes MCP server for tool execution (enabled by default)
- **Prometheus**: For metrics collection (optional)
- **Jaeger/OTLP Collector**: For distributed tracing (optional)

External dependencies:
- **LLM Providers**: OpenAI, Anthropic, Azure OpenAI, Google Vertex AI, or Ollama (user-configured)

**Identity and Access Management:**
Kagent implements a multi-layered IAM approach:

1. **Kubernetes RBAC**: 
   - Controller uses ServiceAccount with ClusterRole for CRD management
   - Agents receive individual ServiceAccounts with configurable RBAC permissions
   - Example roles in [go/config/rbac/role.yaml](https://github.com/kagent-dev/kagent/blob/main/go/config/rbac/role.yaml)
   - Per-agent RBAC templates in [helm/agents/*/templates/rbac.yaml](https://github.com/kagent-dev/kagent/tree/main/helm/agents)

2. **API Authentication** (planned enhancement - [Issue #476](https://github.com/kagent-dev/kagent/issues/476)):
   - Current: UnsecureAuthenticator for development, A2AAuthenticator for agent-to-agent
   - Planned: Extensible authentication system with support for API keys, OAuth, and service accounts
   - Framework in [go/pkg/auth/auth.go](https://github.com/kagent-dev/kagent/blob/main/go/pkg/auth/auth.go)

3. **Secret Management**:
   - LLM API keys stored in Kubernetes Secrets
   - Secrets mounted as environment variables or files
   - No cross-namespace secret access (potential future enhancement via ReferenceGrant)
   - Secrets managed via [go/internal/utils/secret.go](https://github.com/kagent-dev/kagent/blob/main/go/internal/utils/secret.go)

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
- **Apache 2.0 License**: Clear open-source licensing
- **OpenSSF Best Practices**: Badge at https://www.bestpractices.dev/projects/10723
- **Dependency Scanning**: Automated CVE scanning via Trivy in CI/CD
- **SBOM Generation**: Planned for v1.0 release
- **Audit Logging**: Comprehensive logging of all agent operations and API calls
- **Security Self-Assessment**: Available at [contrib/cncf/security-self-assessment.md](https://github.com/kagent-dev/kagent/blob/main/contrib/cncf/security-self-assessment.md)

**High Availability:**
- **Controller**: Supports multi-replica deployments with leader election (via controller-runtime)
- **Agents**: Configurable replica counts per agent (default: 1, can scale horizontally)
- **Database**: Supports PostgreSQL with HA configurations (replication, failover)
- **Stateless Design**: Controllers and agents are stateless, state in Kubernetes API and database
- **Rolling Updates**: Zero-downtime upgrades via Kubernetes rolling deployment strategy
- **Health Checks**: Liveness and readiness probes on all components

**Resource Requirements:**

Default per-component requirements (from [helm/kagent/values.yaml](https://github.com/kagent-dev/kagent/blob/main/helm/kagent/values.yaml)):

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

Minimum cluster requirements:
- **Development**: 2 CPU cores, 4GB RAM (Kind on laptop)
- **Production**: 4+ CPU cores, 8GB+ RAM, scales with number of agents

**Storage Requirements:**

Ephemeral Storage:
- Container images: ~500MB per component (controller, UI, agent base images)
- Temporary files: Minimal, used for skill loading and code execution sandboxes
- Logs: Configurable retention, typically 100MB-1GB per component

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
   - `toolservers.kagent.dev/v1alpha1` - MCP tool server definitions (deprecated)
   - `remotemcpservers.kagent.dev/v1alpha2` - Remote MCP servers
   - `mcpservers.kagent.dev` (via KMCP dependency)
   - `memories.kagent.dev/v1alpha1` - Memory/vector store configurations

2. **HTTP REST API** (controller):
   - Base path: `/api`
   - Endpoints: `/agents`, `/sessions`, `/modelconfigs`, `/tools`, `/feedback`, etc.
   - Format: JSON request/response
   - Authentication: Pluggable (currently development mode)
   - See [go/internal/httpserver/server.go](https://github.com/kagent-dev/kagent/blob/main/go/internal/httpserver/server.go)

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

These do not modify existing Kubernetes APIs or cloud provider APIs. All interactions with cloud providers (for LLMs) are initiated by agent pods, not the controller.

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
Kagent follows semantic versioning (https://semver.org/):

- **Major Releases** (x.0.0): Breaking API changes, major new features
  - Require migration guides
  - Announced 3+ months in advance
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

See [CONTRIBUTION.md](https://github.com/kagent-dev/kagent/blob/main/CONTRIBUTION.md#releasing) for details.

### Installation

**Installation Methods:**

Kagent provides multiple installation paths to suit different use cases:

**1. Helm Installation (Recommended for Production):**
```bash
# Install CRDs first
helm install kagent-crds oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
  --namespace kagent --create-namespace

# Install kagent with OpenAI provider
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.openAI.apiKey=<your-api-key>

# Or with Anthropic
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.default=anthropic \
  --set providers.anthropic.apiKey=<your-api-key>
```

**2. CLI Installation (Quickest for Getting Started):**
```bash
# Install CLI
curl -fsSL https://kagent.dev/install.sh | sh

# Install kagent to cluster
export OPENAI_API_KEY=<your-api-key>
kagent dashboard
```

**3. Local Development (Kind):**
```bash
# Clone repository
git clone https://github.com/kagent-dev/kagent.git
cd kagent

# Create Kind cluster and install
make create-kind-cluster
export OPENAI_API_KEY=<your-api-key>
make helm-install

# Access UI
kubectl port-forward svc/kagent-ui 8001:80 -n kagent
```

Full installation guide: https://kagent.dev/docs/kagent/introduction/installation

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
- [DEVELOPMENT.md#troubleshooting](https://github.com/kagent-dev/kagent/blob/main/DEVELOPMENT.md#troubleshooting)
- Helm README: [helm/README.md](https://github.com/kagent-dev/kagent/blob/main/helm/README.md)

**Quick Start Guide:**
https://kagent.dev/docs/kagent/getting-started/quickstart

### Security

**Security Self-Assessment:**
Kagent's comprehensive security self-assessment is available at:
[contrib/cncf/security-self-assessment.md](https://github.com/kagent-dev/kagent/blob/main/contrib/cncf/security-self-assessment.md)

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
   - Controller runs with minimal RBAC permissions (see [go/config/rbac/role.yaml](https://github.com/kagent-dev/kagent/blob/main/go/config/rbac/role.yaml))
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
   - Automated Helm chart updates
   - Dependency updates via Dependabot

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
   - Custom: Edit agent RBAC templates in [helm/agents/*/templates/rbac.yaml](https://github.com/kagent-dev/kagent/tree/main/helm/agents)
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
   - `npm audit` for UI dependencies
   - `uv audit` for Python dependencies
   - Run via `make audit` (see [Makefile](https://github.com/kagent-dev/kagent/blob/main/Makefile))
4. **Dependency Management**:
   - Go modules with version pinning
   - Python uv.lock for reproducible builds
   - npm package-lock.json
   - Regular dependency updates
5. **Signed Commits**: DCO (Developer Certificate of Origin) required
6. **Security Policy**: [SECURITY.md](https://github.com/kagent-dev/kagent/blob/main/SECURITY.md) with responsible disclosure process
7. **OpenSSF Best Practices**: Badge at https://www.bestpractices.dev/projects/10723

**Security Risk Evaluation:**
Features evaluated for security risk:
- **Agent Code Execution**: Sandboxed Python code execution for `executeCodeBlocks` feature
- **Tool Invocation**: RBAC-controlled access to Kubernetes APIs and external services
- **Secret Access**: Scoped to agent's namespace, no cross-namespace access
- **Database Access**: Session isolation prevents cross-user data access
- **A2A Communication**: Authentication framework for agent-to-agent calls

Ongoing evaluation via:
- Threat modeling sessions (planned quarterly)
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
- SLSA provenance attestations
- Cosign image signing
- SBOM in SPDX/CycloneDX format
- Supply chain security dashboard

See [security self-assessment](https://github.com/kagent-dev/kagent/blob/main/contrib/cncf/security-self-assessment.md#future-state) for details. 

## Day 1 \- Installation and Deployment Phase

### Project Installation and Configuration

**Installation Overview:**
Kagent installation is designed to be simple and follows cloud-native best practices using Helm charts.

**Step-by-Step Installation:**

1. **Prerequisites:**
   - Kubernetes cluster 1.27+ (any distribution: EKS, GKE, AKS, Kind, k3s, etc.)
   - `kubectl` configured to access cluster
   - `helm` 3.x installed
   - LLM provider API key (OpenAI, Anthropic, Azure OpenAI, or local Ollama)

2. **Install CRDs:**
   ```bash
   helm install kagent-crds oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
     --namespace kagent \
     --create-namespace
   ```
   
   This installs Custom Resource Definitions for Agents, ModelConfigs, ToolServers, etc.
   CRDs are separated to allow independent lifecycle management (Helm best practice).

3. **Install Kagent:**
   ```bash
   helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
     --namespace kagent \
     --set providers.openAI.apiKey=$OPENAI_API_KEY
   ```

4. **Verify Installation:**
   ```bash
   kubectl get pods -n kagent
   kubectl get agents -n kagent
   ```

**Configuration Options:**

**Basic Configuration (via Helm values):**
```yaml
# values.yaml
providers:
  default: openAI  # or anthropic, azureOpenAI, ollama, gemini
  openAI:
    apiKey: "sk-..."  # or use apiKeySecretRef
    model: "gpt-4.1-mini"

database:
  type: postgres  # or sqlite (default)
  postgres:
    url: "postgres://user:pass@host:5432/db"

controller:
  replicas: 2  # for HA
  resources:
    requests:
      cpu: 200m
      memory: 256Mi

otel:
  tracing:
    enabled: true
    exporter:
      otlp:
        endpoint: "http://jaeger:4317"
```

**Advanced Configuration:**

1. **Custom Agent Configuration:**
   ```yaml
   apiVersion: kagent.dev/v1alpha2
   kind: Agent
   metadata:
     name: custom-agent
   spec:
     type: Declarative
     declarative:
       systemMessage: "You are a custom agent..."
       modelConfig: custom-model-config
       tools:
         - type: McpServer
           mcpServer:
             name: kmcp
             toolNames: ["k8s_get_resources"]
       deployment:
         replicas: 2
         resources:
           requests:
             cpu: 200m
             memory: 512Mi
   ```

2. **Custom Model Configuration:**
   ```yaml
   apiVersion: kagent.dev/v1alpha2
   kind: ModelConfig
   metadata:
     name: custom-model-config
   spec:
     providerName: OpenAI
     model: gpt-4
     apiKeySecretRef:
       name: my-openai-secret
       key: api-key
     tls:
       caCert: "base64-encoded-ca-cert"
       insecureSkipVerify: false
   ```

3. **RBAC Customization:**
   Edit agent RBAC templates in Helm charts or create custom ClusterRoles:
   ```yaml
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: custom-agent-role
   rules:
   - apiGroups: [""]
     resources: ["pods", "services"]
     verbs: ["get", "list", "watch"]
   ```

4. **Network Policies:**
   ```yaml
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: kagent-network-policy
   spec:
     podSelector:
       matchLabels:
         app.kubernetes.io/part-of: kagent
     policyTypes:
     - Ingress
     - Egress
     ingress:
     - from:
       - namespaceSelector:
           matchLabels:
             name: kagent
     egress:
     - to:
       - namespaceSelector: {}
   ```

**Configuration Files:**
- Helm values: [helm/kagent/values.yaml](https://github.com/kagent-dev/kagent/blob/main/helm/kagent/values.yaml)
- Agent examples: [helm/agents/*/templates/agent.yaml](https://github.com/kagent-dev/kagent/tree/main/helm/agents)
- Model config example: [examples/modelconfig-with-tls.yaml](https://github.com/kagent-dev/kagent/blob/main/examples/modelconfig-with-tls.yaml)

**Configuration Management:**
- **GitOps**: All configuration can be managed via Git (ArgoCD, Flux compatible)
- **Secrets Management**: Integrate with external secret stores (External Secrets Operator, Vault)
- **Environment-Specific**: Use Helm values files per environment (dev, staging, prod)

**Post-Installation Configuration:**
After installation, configure via:
- **UI**: http://localhost:8080 (after port-forward)
- **CLI**: `kagent agent create`, `kagent mcp add`
- **kubectl**: `kubectl apply -f agent.yaml`
- **API**: HTTP REST API at `http://kagent-controller:8083/api`

Full configuration guide: https://kagent.dev/docs/kagent/introduction/installation

### Project Enablement and Rollback

**Enabling Kagent:**

Kagent can be enabled in a live cluster with zero downtime to existing workloads:

```bash
# Install CRDs
helm install kagent-crds oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
  --namespace kagent --create-namespace

# Install kagent
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.openAI.apiKey=$OPENAI_API_KEY
```

**No downtime required:**
- Kagent runs in its own namespace (default: `kagent`)
- Does not modify existing cluster resources
- No admission webhooks or API server modifications
- No node-level components (no DaemonSets by default)
- Controller uses leader election for HA (multiple replicas can run)

**Disabling Kagent:**

**Temporary Disable (scale to zero):**
```bash
# Scale controller to 0 replicas
kubectl scale deployment kagent-controller -n kagent --replicas=0

# Scale all agents to 0
kubectl scale deployment -n kagent --all --replicas=0
```

**Permanent Removal:**
```bash
# Uninstall kagent (keeps CRDs and custom resources)
helm uninstall kagent -n kagent

# Remove CRDs and all custom resources (destructive)
helm uninstall kagent-crds -n kagent

# Or manually delete CRDs
kubectl delete crd agents.kagent.dev
kubectl delete crd modelconfigs.kagent.dev
kubectl delete crd toolservers.kagent.dev
kubectl delete crd memories.kagent.dev
kubectl delete crd remotemcpservers.kagent.dev
```

**Downtime Considerations:**
- **Control Plane**: No downtime, Kubernetes API server unaffected
- **Nodes**: No downtime, no node-level components
- **Existing Workloads**: No impact, kagent is isolated
- **Kagent Agents**: Agents become unavailable during disable/uninstall
- **In-Flight Requests**: Agent requests may fail if agents are terminated mid-operation

**Impact on Cluster Behavior:**

Enabling kagent **does not** change:
- Kubernetes API server behavior
- Existing workload scheduling or networking
- Default RBAC policies
- Node configuration
- Network policies (unless explicitly added)
- Storage classes or persistent volumes

Enabling kagent **does** add:
- New CRDs to Kubernetes API server (agents.kagent.dev, etc.)
- New namespaced resources (deployments, services, secrets in `kagent` namespace)
- Optional: ClusterRoles for agents (if agents need cluster-wide access)
- Optional: Network policies (if configured)
- API server load: Minimal, controller watches CRDs and creates/updates resources

**Testing Enablement and Disablement:**

Kagent tests enablement/disablement through:

1. **E2E Tests** ([.github/workflows/ci.yaml](https://github.com/kagent-dev/kagent/blob/main/.github/workflows/ci.yaml)):
   ```bash
   # Install
   make create-kind-cluster
   make helm-install
   
   # Verify
   kubectl wait --for=condition=Ready agents.kagent.dev -n kagent --all
   
   # Test
   go test -v github.com/kagent-dev/kagent/go/test/e2e
   
   # Uninstall
   make helm-uninstall
   
   # Verify cleanup
   kubectl get agents -n kagent  # Should be empty or error
   ```

2. **Helm Tests** ([helm/kagent/tests/](https://github.com/kagent-dev/kagent/tree/main/helm/kagent/tests)):
   ```bash
   helm test kagent -n kagent
   ```

3. **Manual Testing Procedure:**
   - Install kagent
   - Create test agent
   - Invoke agent and verify response
   - Scale controller to 0, verify agents stop responding
   - Scale controller back up, verify agents resume
   - Uninstall kagent, verify resources cleaned up
   - Reinstall, verify agents recreated

4. **Upgrade Testing:**
   ```bash
   # Install old version
   helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
     --version v0.x.0 --namespace kagent
   
   # Upgrade to new version
   helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
     --version v0.x.1 --namespace kagent
   
   # Verify agents still work
   kubectl get agents -n kagent
   ```

**Resource Cleanup:**

**Automatic Cleanup (via Helm uninstall):**
```bash
helm uninstall kagent -n kagent
```
This removes:
- Deployments (controller, UI, agents)
- Services
- ConfigMaps
- ServiceAccounts
- Roles/RoleBindings
- Secrets (if created by Helm)

**Manual Cleanup (CRDs and Custom Resources):**
```bash
# Delete all custom resources first
kubectl delete agents --all -n kagent
kubectl delete modelconfigs --all -n kagent
kubectl delete toolservers --all -n kagent
kubectl delete memories --all -n kagent
kubectl delete remotemcpservers --all -n kagent

# Then delete CRDs
helm uninstall kagent-crds -n kagent
```

**Cleanup Script:**
```bash
#!/bin/bash
# Complete kagent removal

# Uninstall Helm releases
helm uninstall kagent -n kagent
helm uninstall kagent-crds -n kagent

# Delete namespace (removes all namespaced resources)
kubectl delete namespace kagent

# Delete ClusterRoles and ClusterRoleBindings
kubectl delete clusterrole -l app.kubernetes.io/part-of=kagent
kubectl delete clusterrolebinding -l app.kubernetes.io/part-of=kagent

# Verify CRDs are gone
kubectl get crd | grep kagent.dev
```

**Cleanup Considerations:**
- **Database**: If using external PostgreSQL, data persists after uninstall (manual cleanup required)
- **Secrets**: Secrets with LLM API keys persist (may want to delete manually)
- **Persistent Volumes**: If using PVs for database, they persist (reclaim policy dependent)
- **CRDs**: Helm does not delete CRDs by default (prevents accidental data loss)
- **Finalizers**: Agents may have finalizers that prevent deletion until cleanup completes

**Cleanup Verification:**
```bash
# Verify no kagent resources remain
kubectl get all -n kagent
kubectl get crd | grep kagent.dev
kubectl get clusterrole | grep kagent
kubectl get clusterrolebinding | grep kagent

# Check for stuck resources
kubectl get agents --all-namespaces
kubectl get modelconfigs --all-namespaces
```

Documentation: [helm/README.md#uninstallation](https://github.com/kagent-dev/kagent/blob/main/helm/README.md#uninstallation)

### Rollout, Upgrade and Rollback Planning

**Kubernetes Compatibility:**

Kagent maintains compatibility with Kubernetes through:

- **Supported Versions**: Kubernetes 1.27+ (current LTS and newer)
- **Testing**: CI/CD tests against multiple Kubernetes versions (currently 1.34.0 in Kind)
- **API Compatibility**: Uses stable Kubernetes APIs (apps/v1, core/v1, rbac.authorization.k8s.io/v1)
- **Controller-Runtime**: Leverages controller-runtime for Kubernetes client compatibility
- **Update Frequency**: 
  - Major Kubernetes releases: Tested and supported within 1 month of release
  - Minor releases: Compatibility verified within 2 weeks
  - Security patches: Immediate testing and updates if needed

**Version Support Policy:**
- Support N and N-1 Kubernetes minor versions
- Drop support for versions reaching Kubernetes EOL
- Announce version support changes 3 months in advance

**Upgrade Procedures:**

**Helm Upgrade (Recommended):**
```bash
# Upgrade CRDs first
helm upgrade kagent-crds oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
  --namespace kagent \
  --version v0.x.1

# Then upgrade kagent
helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --version v0.x.1 \
  --reuse-values
```

**Upgrade Process:**
1. CRDs upgraded first (may add new fields, never remove required fields in alpha)
2. Controller deployment performs rolling update (zero downtime)
3. Agents perform rolling updates (may have brief unavailability per agent)
4. Database migrations run automatically on controller startup
5. Backward compatibility maintained for N-1 API versions

**Zero-Downtime Upgrades:**
- Controller: Multiple replicas with leader election, rolling update
- Agents: Rolling update with readiness checks
- Database: Schema migrations are backward compatible
- API: Versioned endpoints maintain compatibility

**Rollback Procedures:**

**Helm Rollback:**
```bash
# List releases
helm history kagent -n kagent

# Rollback to previous version
helm rollback kagent -n kagent

# Or rollback to specific revision
helm rollback kagent 2 -n kagent
```

**Manual Rollback:**
```bash
# Rollback controller deployment
kubectl rollout undo deployment/kagent-controller -n kagent

# Rollback specific agent
kubectl rollout undo deployment/k8s-agent -n kagent

# Verify rollback
kubectl rollout status deployment/kagent-controller -n kagent
```

**Rollback Considerations:**
- **Database Schema**: Downgrades may require manual schema rollback if migrations are not backward compatible
- **CRDs**: Downgrading CRDs may lose data in new fields (alpha versions)
- **API Compatibility**: Ensure agents are compatible with rolled-back controller version
- **In-Flight Requests**: May fail during rollback, clients should retry

**Rollback Testing:**
```bash
# Install v0.x.0
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --version v0.x.0 --namespace kagent

# Upgrade to v0.x.1
helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --version v0.x.1 --namespace kagent

# Test functionality
kubectl get agents -n kagent

# Rollback to v0.x.0
helm rollback kagent -n kagent

# Verify functionality restored
kubectl get agents -n kagent
```

**Rollout/Rollback Failure Scenarios:**

**Potential Failures:**
1. **CRD Incompatibility**: New controller requires CRD fields not present in old version
   - Impact: Controller crashes, agents unavailable
   - Mitigation: Upgrade CRDs before controller

2. **Database Migration Failure**: Schema migration fails or times out
   - Impact: Controller fails to start, agents unavailable
   - Mitigation: Database backups, manual migration rollback

3. **Image Pull Failure**: New image not available or pull fails
   - Impact: Rolling update stalls, old version continues running
   - Mitigation: Pre-pull images, use ImagePullPolicy: IfNotPresent

4. **Resource Exhaustion**: New version requires more resources than available
   - Impact: Pods fail to schedule, deployment stuck
   - Mitigation: Resource quotas, pre-upgrade capacity planning

5. **API Breaking Change**: New version has incompatible API changes
   - Impact: Existing agents fail to communicate with controller
   - Mitigation: API versioning, backward compatibility testing

**Impact on Running Workloads:**
- **Kubernetes Workloads**: No impact, kagent is isolated
- **Agent Operations**: May fail during upgrade/rollback, clients should retry
- **Sessions**: In-flight sessions may be lost if agent pods are terminated
- **Database**: Connections may be briefly interrupted during controller restart

**Rollback Metrics:**

Metrics that should trigger rollback:

1. **Error Rate**: 
   - Metric: `http_requests_total{status=~"5.."}`
   - Threshold: >5% error rate for >5 minutes
   - Action: Immediate rollback

2. **Agent Availability**:
   - Metric: `kube_deployment_status_replicas_available{deployment="kagent-controller"}`
   - Threshold: <50% of desired replicas available for >5 minutes
   - Action: Rollback and investigate

3. **Database Connection Failures**:
   - Metric: `database_connection_errors_total`
   - Threshold: >10 errors in 1 minute
   - Action: Check database, consider rollback

4. **Agent Invocation Failures**:
   - Metric: `agent_invocation_errors_total`
   - Threshold: >10% failure rate for >5 minutes
   - Action: Rollback if not transient

5. **Memory/CPU Exhaustion**:
   - Metric: `container_memory_usage_bytes`, `container_cpu_usage_seconds_total`
   - Threshold: >90% of limits for >5 minutes
   - Action: Rollback and adjust resources

6. **CrashLoopBackOff**:
   - Metric: `kube_pod_container_status_restarts_total`
   - Threshold: >5 restarts in 5 minutes
   - Action: Immediate rollback

**Monitoring Rollouts:**
```bash
# Watch rollout status
kubectl rollout status deployment/kagent-controller -n kagent

# Monitor pod health
kubectl get pods -n kagent -w

# Check logs for errors
kubectl logs -n kagent deployment/kagent-controller --tail=100

# View metrics
kubectl port-forward -n kagent svc/kagent-controller 8083:8083
curl http://localhost:8083/metrics
```

**Upgrade/Rollback Testing:**

**Automated Testing (CI/CD):**
Currently tests basic upgrade scenarios. Full upgrade/downgrade/upgrade testing planned for v1.0.

**Manual Testing Procedure:**
```bash
# 1. Install v0.x.0
helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --version v0.x.0 --namespace kagent --set providers.openAI.apiKey=$OPENAI_API_KEY

# 2. Create test agent and verify
kubectl apply -f test-agent.yaml
kagent agent invoke test-agent "Hello"

# 3. Upgrade to v0.x.1
helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --version v0.x.1 --namespace kagent --reuse-values

# 4. Verify agent still works
kagent agent invoke test-agent "Hello after upgrade"

# 5. Rollback to v0.x.0
helm rollback kagent -n kagent

# 6. Verify agent still works
kagent agent invoke test-agent "Hello after rollback"

# 7. Upgrade again to v0.x.1
helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --version v0.x.1 --namespace kagent --reuse-values

# 8. Final verification
kagent agent invoke test-agent "Hello after re-upgrade"
```

**Database Migration Testing:**
```bash
# Backup database before upgrade
kubectl exec -n kagent deployment/kagent-controller -- \
  sqlite3 /data/kagent.db .dump > backup.sql

# Upgrade
helm upgrade kagent ...

# Verify migrations
kubectl logs -n kagent deployment/kagent-controller | grep migration

# If rollback needed, restore database
kubectl exec -n kagent deployment/kagent-controller -- \
  sqlite3 /data/kagent.db < backup.sql
```

**Deprecation and Removal Communication:**

**Deprecation Policy:**
- **Alpha APIs** (v1alpha1, v1alpha2): Can be deprecated with 1 release notice, removed after 2 releases
- **Beta APIs** (v1beta1): Deprecated with 3 release notice, removed after 6 releases
- **Stable APIs** (v1): Deprecated with 6 release notice, removed after 12 releases

**Communication Channels:**
1. **Release Notes**: Deprecations listed in GitHub releases
2. **Changelog**: CHANGELOG.md updated with deprecation notices
3. **Documentation**: Deprecated features marked in docs with removal timeline
4. **API Warnings**: Kubernetes API server warnings for deprecated CRD versions
5. **Discord/Slack**: Announcements in community channels
6. **Blog Posts**: Major deprecations announced on kagent.dev blog

**Example Deprecation Notice:**
```
## v0.x.0 Release Notes

### Deprecations
- `toolservers.kagent.dev/v1alpha1` is deprecated in favor of `remotemcpservers.kagent.dev/v1alpha2`
- Will be removed in v0.(x+2).0 (approximately 3 months)
- Migration guide: https://kagent.dev/docs/migration/toolservers-to-remotemcpservers

### Breaking Changes
- None in this release

### New Features
- Added `remotemcpservers.kagent.dev/v1alpha2` with improved MCP support
```

**Migration Guides:**
- Provided for all breaking changes
- Step-by-step instructions with examples
- Automated migration tools where possible (e.g., `kagent migrate` CLI command)

**Alpha/Beta Capabilities:**

**Alpha Features:**
- Marked as `v1alpha1` or `v1alpha2` in CRD versions
- Documented as "Alpha" in feature documentation
- May have breaking changes between releases
- Disabled by default in some cases (feature flags)
- Examples:
  - Memory/vector store integration (v1alpha1)
  - Multi-agent coordination features

**Beta Features:**
- Marked as `v1beta1` in CRD versions
- Documented as "Beta" in feature documentation
- API stable, but implementation may change
- Enabled by default
- Examples (planned):
  - Advanced RBAC policies
  - Multi-tenancy support

**Enabling Alpha/Beta Features:**
```yaml
# Via Helm values
features:
  alphaMemoryIntegration: true
  betaMultiTenancy: true

# Via CRD version
apiVersion: kagent.dev/v1alpha2  # Alpha
apiVersion: kagent.dev/v1beta1   # Beta (planned)
apiVersion: kagent.dev/v1        # Stable (planned)
```

**Feature Graduation Process:**
1. **Alpha**: Experimental, may change, opt-in
2. **Beta**: Stable API, tested, enabled by default
3. **Stable**: Production-ready, backward compatibility guaranteed

**Current API Versions:**
- `agents.kagent.dev/v1alpha2` - Alpha, active development
- `modelconfigs.kagent.dev/v1alpha2` - Alpha, active development
- `memories.kagent.dev/v1alpha1` - Alpha, experimental
- `toolservers.kagent.dev/v1alpha1` - Alpha, deprecated
- `remotemcpservers.kagent.dev/v1alpha2` - Alpha, active development

**Roadmap to v1:**
- v0.x releases: Alpha features, API refinement
- v0.9.x: Feature freeze, API stabilization
- v1.0.0: Stable APIs, production-ready, backward compatibility guarantees

Documentation: https://kagent.dev/docs/kagent (upgrade guide planned)

## Day 2 \- Day-to-Day Operations Phase

**Note**: Kagent is currently in alpha/pre-v1.0 development and applying for CNCF Incubation. Day 2 operational characteristics are still being established through production usage. The following sections describe current capabilities and planned improvements, with clear distinctions between what is implemented versus planned.

### Scalability/Reliability

**Scaling API Objects:**

Kagent scales through standard Kubernetes patterns:

1. **Horizontal Scaling (Agent Count)**:
   ```bash
   # Scale specific agent
   kubectl scale deployment k8s-agent -n kagent --replicas=5
   
   # Or via Agent CRD
   kubectl patch agent k8s-agent -n kagent --type=merge \
     -p '{"spec":{"declarative":{"deployment":{"replicas":5}}}}'
   ```

2. **Vertical Scaling (Resources)**:
   ```yaml
   apiVersion: kagent.dev/v1alpha2
   kind: Agent
   spec:
     declarative:
       deployment:
         resources:
           requests:
             cpu: 500m
             memory: 1Gi
           limits:
             cpu: 2000m
             memory: 4Gi
   ```

3. **Controller Scaling**:
   ```bash
   # Scale controller for HA
   helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
     --set controller.replicas=3 \
     --reuse-values
   ```
   Controller uses leader election, only one active at a time for reconciliation.

4. **Database Scaling**:
   - SQLite: Single-node, limited scalability (development only)
   - PostgreSQL: Supports read replicas, connection pooling, horizontal scaling

**Scaling Limits:**
- **Agents per Cluster**: Currently tested with up to 10-15 agents in CI/CD and development environments. Production deployments at early adopters run 5-10 agents. Theoretical limits not yet established through formal load testing.
- **Concurrent Requests per Agent**: Limited by LLM provider rate limits and agent resources. Specific limits not yet documented.
- **Database Connections**: Default pool size 10, configurable
- **Watch Resources**: Controller watches all namespaces by default, can be scoped via `controller.watchNamespaces`

**Service Level Objectives (SLOs):**

**Note**: As an alpha/pre-v1.0 project, formal SLOs have not yet been established. The following are observational targets based on current behavior:

1. **Availability**: Controller API generally available when Kubernetes API server is healthy
   - No formal uptime SLO defined yet

2. **Latency**: 
   - API requests: Typically <500ms for non-agent operations
   - Agent invocations: Highly variable, dependent on LLM provider (typically 1-30s)
   - Formal latency SLOs not yet defined

3. **Error Rate**: No formal error rate SLO defined yet

4. **Agent Readiness**: Agents typically become ready within 60s of creation, dependent on image pull time and LLM provider connectivity

**Service Level Indicators (SLIs):**

**Note**: As an alpha project, formal SLIs with defined targets have not been established. The following metrics are available for monitoring:

1. **Availability**: 
   - Standard Kubernetes metrics via kube-state-metrics
   - Pod status, deployment status

2. **Request Latency**:
   - Metric exposure via `/metrics` endpoint (Prometheus format)
   - Specific metric names to be documented

3. **Error Rate**:
   - HTTP status codes available in logs
   - Metric exposure planned

4. **Agent Readiness**:
   - Standard Kubernetes deployment metrics
   - Agent CRD status conditions

5. **Database Health**:
   - Connection status logged
   - Specific metrics to be documented

6. **LLM Provider Latency**:
   - Traced via OpenTelemetry when enabled
   - Specific metrics to be documented

**Future SLI/SLO Development:**

Formal SLIs and SLOs are planned for v1.0 and will be defined based on production usage patterns and user requirements.

**Resource Usage:**

**Baseline Resource Usage (Minimal Installation):**

Based on default Helm chart values:
- Controller: 100m CPU request, 128Mi memory request / 2 CPU limit, 512Mi memory limit
- UI: 100m CPU request, 256Mi memory request / 1 CPU limit, 1Gi memory limit
- Per Agent: 100m CPU request, 256Mi-384Mi memory request / 1-2 CPU limit, 1Gi memory limit
- Database (SQLite): Negligible (in-memory or small file)
- Total Minimum: ~300m CPU, ~640Mi memory for basic setup with controller + UI + 1 agent

**Resource Increase with Scale:**

**Note**: Formal load testing and resource profiling at scale has not been completed. The following are estimates based on small-scale deployments:

- Small deployments (1-5 agents): ~500m-1 CPU, 2-4Gi memory
- Larger deployments: Resource requirements scale approximately linearly with agent count, but formal benchmarks are not yet available

**Per-Component Scaling:**
- **Controller**: Resource usage scales with number of CRD reconciliations and API requests. Specific scaling characteristics not yet documented.
- **Agents**: Memory usage varies with conversation history length and tool complexity. Typical range 256Mi-1Gi per agent.
- **Database**: Storage scales with session history. Specific growth rates not yet measured.
- **Network**: Throughput depends on agent invocation frequency and LLM streaming. Specific bandwidth requirements not yet documented.

**Resource Exhaustion Scenarios:**

1. **PID Exhaustion**:
   - Condition: >1000 agents with high concurrency
   - Mitigation: Increase node PID limits, use fewer larger nodes
   - Prevention: Set resource quotas per namespace

2. **Socket Exhaustion**:
   - Condition: High concurrent LLM connections, database connections
   - Mitigation: Connection pooling, rate limiting
   - Prevention: Configure connection pool sizes, use HTTP/2

3. **Inode Exhaustion**:
   - Condition: Large number of sessions with SQLite (one file per session)
   - Mitigation: Use PostgreSQL, configure session retention
   - Prevention: Monitor inode usage, set retention policies

4. **Memory Exhaustion**:
   - Condition: Large conversation histories, memory leaks
   - Mitigation: Set memory limits, configure garbage collection
   - Prevention: Session cleanup, memory profiling

5. **Storage Exhaustion**:
   - Condition: Unbounded database growth, log accumulation
   - Mitigation: Configure retention policies, log rotation
   - Prevention: Monitor disk usage, set up alerts

**Load Testing:**

**Current Testing:**
- E2E tests with mock LLM servers in CI/CD (deterministic, 5-10 agents)
- Manual testing during development
- No formal load testing or performance benchmarking has been conducted yet

**Planned Load Testing:**
Comprehensive load testing is planned for the v1.0 release to establish:
- Maximum agent count per cluster
- Concurrent request handling capacity
- Database performance under load
- Resource utilization patterns
- Failure scenario behavior

**Recommended Limits:**

**Note**: As an alpha project without formal load testing, specific operational limits have not been established. The following are based on limited production experience:

1. **Agents per Cluster**: Early production deployments run 5-10 agents. Upper limits not yet determined.
2. **Concurrent Requests per Agent**: Dependent on LLM provider rate limits. Specific recommendations not yet established.
3. **Controller Replicas**: 1-3 replicas supported (leader election implemented)
4. **Database Connections**: Default pool size 10, configurable
5. **Session Retention**: Configurable, default retention policies not yet defined
6. **Max Message Size**: Limited by LLM context windows, specific limits not documented
7. **Concurrent Tool Invocations**: Not yet limited or documented

**How Current Information Was Obtained:**
- Early production deployments at Amdocs, Au10tix, Krateo (5-10 agents each)
- Development and CI/CD testing (up to 10-15 agents)
- Default Helm chart values
- LLM provider documentation

**Resilience Patterns:**

**Currently Implemented:**

1. **Retry with Exponential Backoff**:
   - Used by Kubernetes client-go for API calls
   - LLM client libraries handle retries per provider specifications
   - Specific retry configurations not yet documented

2. **Timeout and Deadline Propagation**:
   - Context-based timeouts used in Go code
   - Default timeout for agent invocations: 600s (configurable via Helm)
   - Specific timeout values for other operations not yet documented

3. **Graceful Degradation**:
   - Optional features (tracing, observability) degrade gracefully if backends unavailable
   - Controller continues operation if optional dependencies fail

4. **Bulkhead Pattern**:
   - Each agent runs in separate deployment
   - Resource limits configurable per agent
   - Agent failures isolated from each other

5. **Health Checks and Readiness Probes**:
   - HTTP `/health` endpoint on controller
   - Kubernetes liveness and readiness probes configured in Helm charts

6. **Leader Election**:
   - Controller uses Kubernetes lease-based leader election (via controller-runtime)
   - Supports multiple controller replicas with single active reconciler

7. **Database Connection Pooling**:
   - Implemented for PostgreSQL via GORM
   - Default pool size: 10 connections

8. **Idempotent Operations**:
   - Kubernetes reconciliation loop is idempotent
   - CRD status updates track reconciliation state

**Planned for v1.0:**
- Circuit breaker pattern for LLM providers
- Rate limiting for API endpoints
- Formal resilience testing and documentation

**Monitoring:**
Basic monitoring available via:
- Kubernetes pod status and events
- Controller logs
- Prometheus metrics endpoint (specific metrics to be documented)

Documentation: Resilience patterns and monitoring guide planned for v1.0

### Observability Requirements

**Observability Signals:**

Kagent implements comprehensive observability using the three pillars:

**1. Logs:**

**Format**: Structured JSON logs (Go components) and Python logging (agent runtime)

**Configuration**:
```yaml
# Helm values
controller:
  loglevel: "info"  # debug, info, warn, error

otel:
  logging:
    enabled: true
    exporter:
      otlp:
        endpoint: "http://otel-collector:4317"
```

**Log Sources**:
- **Controller**: Reconciliation events, CRD validation, errors
- **Agents**: Agent invocations, tool calls, LLM interactions
- **UI**: User actions, API calls
- **Database**: Query logs (optional, for debugging)

**Log Aggregation**:
- Kubernetes native: `kubectl logs`
- Centralized: Fluentd, Fluent Bit, Promtail → Loki, Elasticsearch
- OpenTelemetry: OTLP log export to any OTLP-compatible backend

**Example Logs**:
```json
{
  "level": "info",
  "ts": "2025-11-24T10:00:00Z",
  "logger": "controller",
  "msg": "Reconciling agent",
  "agent": "k8s-agent",
  "namespace": "kagent",
  "generation": 5
}
```

**2. Metrics:**

**Format**: Prometheus format (OpenMetrics compatible)

**Endpoints**:
- Controller: `http://kagent-controller:8083/metrics`
- Agents: `http://<agent>:8080/metrics` (Python agents)

**Key Metrics**:

**Note**: Metrics are exposed via Prometheus format on `/metrics` endpoint. Specific metric names and labels are being standardized and documented.

*Available Metrics* (subject to change in alpha):
- Controller reconciliation metrics (via controller-runtime)
- HTTP request metrics (via instrumentation)
- Standard Go runtime metrics (memory, goroutines, etc.)
- OpenTelemetry metrics when tracing enabled

*Planned Metrics* (for v1.0):
- Agent invocation count and latency
- Tool call success/failure rates
- LLM request count, latency, and token usage
- Database connection pool status
- Agent status gauges

*System Metrics*:
- Standard Kubernetes metrics via kube-state-metrics (user-installed)
- Container metrics via cAdvisor (Kubernetes built-in)
- Node metrics via node-exporter (user-installed)

**Prometheus Configuration**:
```yaml
scrape_configs:
  - job_name: 'kagent-controller'
    kubernetes_sd_configs:
      - role: service
    relabel_configs:
      - source_labels: [__meta_kubernetes_service_label_app_kubernetes_io_name]
        regex: kagent-controller
        action: keep
  
  - job_name: 'kagent-agents'
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app_kubernetes_io_part_of]
        regex: kagent
        action: keep
```

**3. Traces:**

**Format**: OpenTelemetry (OTLP)

**Configuration**:
```yaml
otel:
  tracing:
    enabled: true
    exporter:
      otlp:
        endpoint: "http://jaeger:4317"
        insecure: true
```

**Trace Propagation**:
- HTTP headers: W3C Trace Context
- Agent-to-agent: A2A protocol with trace context
- Tool invocations: Trace context passed to MCP servers
- LLM calls: Instrumented via OpenTelemetry SDKs

**Instrumented Operations**:
- HTTP API requests (FastAPI, Gorilla Mux)
- Agent invocations (full conversation flow)
- Tool calls (MCP server requests)
- LLM API calls (OpenAI, Anthropic, etc.)
- Database queries
- Kubernetes API calls

**Trace Backends**:
- Jaeger (recommended for development)
- Tempo (recommended for production)
- Any OTLP-compatible backend (Honeycomb, Lightstep, etc.)

**Example Trace**:
```
Span: POST /api/a2a/kagent/k8s-agent [200ms]
  ├─ Span: Agent Invocation [180ms]
  │   ├─ Span: LLM Request (OpenAI) [100ms]
  │   ├─ Span: Tool Call: k8s_get_resources [50ms]
  │   │   └─ Span: Kubernetes API: List Pods [40ms]
  │   └─ Span: LLM Request (OpenAI) [30ms]
  └─ Span: Database: Save Session [10ms]
```

**Trace Sampling**:
- Default: 100% (development)
- Production: Configurable (e.g., 10% for high-volume)

**4. Profiles** (Planned for v1.0):
- CPU profiling via pprof (Go components)
- Memory profiling via pprof
- Continuous profiling via Pyroscope (planned)

**Data Storage Recommendations:**

| Signal | Storage | Retention | Size Estimate |
|--------|---------|-----------|---------------|
| Logs | Loki, Elasticsearch | 7-30 days | 1-10GB/day |
| Metrics | Prometheus, Thanos | 30-90 days | 100MB-1GB/day |
| Traces | Jaeger, Tempo | 7-14 days | 1-5GB/day |
| Profiles | Pyroscope | 7-30 days | 100MB-1GB/day |

**Audit Logging:**

Kagent captures audit logs for:

1. **API Operations**:
   - All HTTP requests to controller API
   - User/agent identity (when authentication enabled)
   - Request method, path, status, duration
   - Request/response bodies (configurable, PII-aware)

2. **Agent Operations**:
   - Agent creation, update, deletion
   - Agent invocations (user, message, response)
   - Tool invocations (tool name, parameters, result)
   - Session creation and termination

3. **CRD Changes**:
   - Kubernetes audit logs capture all CRD operations
   - Who, what, when for Agent, ModelConfig, etc.

4. **Database Operations**:
   - All database writes logged
   - Session creation, task updates, feedback

**Audit Log Format**:
```json
{
  "timestamp": "2025-11-24T10:00:00Z",
  "type": "agent_invocation",
  "user": "user@example.com",
  "agent": "k8s-agent",
  "namespace": "kagent",
  "action": "invoke",
  "message": "List all pods",
  "status": "success",
  "duration_ms": 2500,
  "trace_id": "abc123..."
}
```

**Audit Log Storage**:
- Kubernetes audit logs: etcd, external webhook
- Application audit logs: Database, SIEM (Splunk, ELK)
- Compliance: Long-term archival (S3, GCS, Azure Blob)

**Dashboards:**

**Current State**:
- No pre-built Grafana dashboards currently provided
- Users can create custom dashboards using available Prometheus metrics
- Standard Kubernetes dashboards can monitor pod/deployment health

**Planned for v1.0**:
- Pre-built Grafana dashboards for:
  - Kagent overview (controller health, agent count)
  - Agent performance metrics
  - LLM usage and cost tracking
  - Database health
  - Kubernetes resource utilization

**Dashboard Requirements** (when available):
- Grafana (version TBD)
- Prometheus data source
- Optional: Loki for logs, Tempo for traces

**Current Monitoring**:
Users can monitor kagent using:
- Kubernetes built-in tools (`kubectl`, dashboards)
- Standard Prometheus queries on `/metrics` endpoint
- OpenTelemetry traces in Jaeger/Tempo (when configured)
- Application logs via `kubectl logs`

**FinOps / Cost Monitoring:**

**Current State**:
Kagent does not currently provide built-in cost monitoring or FinOps dashboards. Cost tracking can be done using:

1. **LLM Token Usage**:
   - Token usage visible in OpenTelemetry traces (when enabled)
   - No aggregated metrics currently exposed
   - Users must track costs via LLM provider dashboards

2. **Compute Resources**:
   - Kubernetes resource requests/limits defined in Helm charts
   - Actual usage via standard Kubernetes monitoring (cAdvisor, metrics-server)
   - Cost calculation requires external tools

3. **Storage Costs**:
   - Database storage depends on session retention and usage
   - No built-in storage metrics currently exposed
   - Users must monitor via database tools or Kubernetes PV metrics

4. **Network Costs**:
   - Egress to LLM providers not currently tracked
   - Users must rely on cloud provider network monitoring

**Planned for v1.0**:
- LLM token usage metrics and cost estimation
- Resource utilization dashboards
- Cost projection tools
- Integration with cloud cost management tools

**Health Parameters:**

Kagent monitors these parameters for health:

1. **Controller Health**:
   - Pod status: Running, Ready
   - Leader election: One active leader
   - Reconciliation lag: <10s
   - API latency: <500ms (P95)
   - Error rate: <1%

2. **Agent Health**:
   - Pod status: Running, Ready
   - Invocation success rate: >95%
   - Invocation latency: <30s (P95)
   - Tool call success rate: >90%

3. **Database Health**:
   - Connection pool: >50% available
   - Query latency: <100ms (P95)
   - Storage usage: <80% capacity
   - Connection errors: 0

4. **LLM Provider Health**:
   - Request success rate: >95%
   - Request latency: <10s (P95)
   - Rate limit errors: <5%

5. **Kubernetes Health**:
   - API server latency: <500ms
   - etcd health: Healthy
   - Node status: Ready

**Determining Project Usage:**

Operators can determine kagent usage through:

1. **Kubernetes Resources**:
   ```bash
   kubectl get agents --all-namespaces
   kubectl get pods -l app.kubernetes.io/part-of=kagent --all-namespaces
   ```

2. **Metrics**:
   ```promql
   # Active agents
   count(kagent_agent_status{status="Ready"})
   
   # Invocation rate
   sum(rate(kagent_agent_invocations_total[5m]))
   ```

3. **Database**:
   ```sql
   SELECT COUNT(*) FROM agents WHERE status = 'active';
   SELECT COUNT(*) FROM sessions WHERE created_at > NOW() - INTERVAL '1 day';
   ```

4. **Audit Logs**:
   ```bash
   kubectl logs -n kagent deployment/kagent-controller | grep "agent_invocation"
   ```

**Verifying Project Functionality:**

Users can verify kagent is working through:

1. **Health Checks**:
   ```bash
   curl http://kagent-controller:8083/health
   # Expected: {"status":"healthy"}
   ```

2. **Version Check**:
   ```bash
   curl http://kagent-controller:8083/version
   # Expected: {"kagent_version":"v0.x.x",...}
   ```

3. **Agent Status**:
   ```bash
   kubectl get agents -n kagent
   # Expected: All agents with READY=True
   ```

4. **Test Invocation**:
   ```bash
   kagent agent invoke k8s-agent "List all namespaces"
   # Expected: Response with namespace list
   ```

5. **UI Access**:
   ```bash
   kubectl port-forward svc/kagent-ui 8080:80 -n kagent
   # Navigate to http://localhost:8080
   # Expected: Dashboard with agents visible
   ```

6. **Metrics**:
   ```bash
   curl http://kagent-controller:8083/metrics | grep kagent_agent_status
   # Expected: Metrics with agent status
   ```

**Service Level Objectives (SLOs):**

See [Scalability/Reliability](#scalabilityreliability) section above for detailed SLOs.

Summary:
- **Availability**: 99.9% uptime
- **Latency**: P95 <500ms for API, P95 <30s for agent invocations
- **Error Rate**: <1% for API requests
- **Agent Readiness**: 95% ready within 60s

**Service Level Indicators (SLIs):**

See [Scalability/Reliability](#scalabilityreliability) section above for detailed SLIs.

Key SLIs:
- `up{job="kagent-controller"}` - Service availability
- `http_request_duration_seconds` - API latency
- `http_requests_total{status=~"5.."}` - Error rate
- `kube_deployment_status_replicas_available` - Agent availability
- `kagent_agent_invocation_duration_seconds` - Agent performance
- `kagent_llm_request_duration_seconds` - LLM provider performance

**Observability Documentation:**
- OpenTelemetry setup: https://kagent.dev/docs/kagent/getting-started/tracing
- Metrics guide: https://kagent.dev/docs/kagent (planned)
- Dashboard setup: https://kagent.dev/docs/kagent (planned)

### Dependencies

**In-Cluster Service Dependencies:**

**Required Dependencies:**
1. **Kubernetes API Server**:
   - Purpose: CRD storage, resource management, RBAC
   - Version: 1.27+
   - Failure Impact: Complete kagent failure, no operations possible
   - Recovery: Automatic reconnection when API server recovers

2. **etcd** (via Kubernetes):
   - Purpose: Persistent storage for CRDs and Kubernetes state
   - Failure Impact: API server unavailable, kagent cannot operate
   - Recovery: Kubernetes handles etcd recovery

3. **CoreDNS** (Kubernetes DNS):
   - Purpose: Service discovery for agent-to-controller, agent-to-tool communication
   - Failure Impact: Agents cannot reach controller or tools
   - Recovery: Automatic when DNS recovers

**Optional In-Cluster Dependencies:**
1. **PostgreSQL**:
   - Purpose: Persistent database for sessions, tasks, events
   - Default: SQLite (ephemeral)
   - Failure Impact: Session history lost, new sessions cannot be created
   - Recovery: Reconnect when database available, may lose in-flight data

2. **KMCP** (Kubernetes MCP Server):
   - Purpose: Kubernetes tool execution for agents
   - Default: Enabled
   - Failure Impact: Agents cannot execute Kubernetes tools
   - Recovery: Automatic when KMCP pods recover

3. **Qdrant** (Vector Database):
   - Purpose: Agent memory and RAG capabilities
   - Default: Disabled
   - Failure Impact: Memory features unavailable
   - Recovery: Automatic when Qdrant recovers

4. **Prometheus**:
   - Purpose: Metrics collection and storage
   - Default: External (user-provided)
   - Failure Impact: No metrics collection, monitoring unavailable
   - Recovery: Metrics resume when Prometheus recovers

5. **OTLP Collector** (Jaeger, Tempo, etc.):
   - Purpose: Trace collection and storage
   - Default: Disabled
   - Failure Impact: No traces, observability degraded
   - Recovery: Traces resume when collector recovers

**External Dependencies:**
1. **LLM Providers** (OpenAI, Anthropic, etc.):
   - Purpose: AI model inference
   - Failure Impact: Agent invocations fail
   - Recovery: Retry with exponential backoff, circuit breaker (planned)

2. **Container Registry**:
   - Purpose: Image pulls for controller, agents, UI
   - Failure Impact: Cannot deploy new pods
   - Recovery: Use cached images, wait for registry recovery

**Dependency Lifecycle Policy:**

**Go Dependencies** (Controller, CLI):
- **Management**: Go modules with `go.mod` and `go.sum`
- **Update Frequency**: Monthly for minor updates, immediate for security patches
- **Version Policy**: 
  - Direct dependencies: Semantic versioning, pin to minor versions
  - Indirect dependencies: Locked via `go.sum`
  - Major version updates: Require testing and approval
- **EOL Policy**: Drop support for dependencies reaching EOL within 3 months
- **Security**: `govulncheck` in CI/CD, Dependabot alerts

**Python Dependencies** (Agent Runtime):
- **Management**: UV with `uv.lock` for reproducible builds
- **Update Frequency**: Monthly for minor updates, immediate for security patches
- **Version Policy**:
  - Direct dependencies: Pin to minor versions (e.g., `openai>=1.0,<2.0`)
  - Locked dependencies: `uv.lock` ensures reproducibility
  - Major version updates: Require compatibility testing
- **EOL Policy**: Drop support for Python versions reaching EOL within 6 months
- **Security**: `uv audit` in CI/CD, Dependabot alerts

**JavaScript Dependencies** (UI):
- **Management**: npm with `package-lock.json`
- **Update Frequency**: Monthly for minor updates, immediate for security patches
- **Version Policy**:
  - Direct dependencies: Caret versioning (e.g., `^1.0.0`)
  - Locked dependencies: `package-lock.json`
  - Major version updates: Require UI testing
- **EOL Policy**: Drop support for Node.js versions reaching EOL within 6 months
- **Security**: `npm audit` in CI/CD, Dependabot alerts

**Container Base Images**:
- **Management**: Dockerfile `FROM` statements
- **Update Frequency**: Monthly for base image updates, immediate for critical CVEs
- **Version Policy**:
  - Use specific tags (e.g., `cgr.dev/chainguard/go:1.24`)
  - Avoid `latest` tag in production
  - Pin to minor versions
- **EOL Policy**: Update to supported base images within 1 month of EOL
- **Security**: Trivy scans in CI/CD

**Kubernetes Dependencies**:
- **controller-runtime**: Follow Kubernetes version support matrix
- **client-go**: Match controller-runtime version
- **API versions**: Support N and N-1 Kubernetes versions

**Source Composition Analysis (SCA):**

**Tools and Processes:**

1. **Go SCA**:
   ```bash
   # Vulnerability scanning
   govulncheck ./...
   
   # Dependency analysis
   go list -m all
   go mod graph
   ```
   - **Frequency**: Every CI/CD run, daily scheduled scans
   - **Tracking**: GitHub Security tab, Dependabot alerts
   - **Threshold**: No high/critical vulnerabilities in production

2. **Python SCA**:
   ```bash
   # Vulnerability scanning
   uv audit
   
   # Dependency tree
   uv tree
   ```
   - **Frequency**: Every CI/CD run, daily scheduled scans
   - **Tracking**: GitHub Security tab, Dependabot alerts
   - **Threshold**: No high/critical vulnerabilities in production

3. **JavaScript SCA**:
   ```bash
   # Vulnerability scanning
   npm audit
   
   # Dependency tree
   npm ls
   ```
   - **Frequency**: Every CI/CD run, daily scheduled scans
   - **Tracking**: GitHub Security tab, Dependabot alerts
   - **Threshold**: No high/critical vulnerabilities in production

4. **Container Image SCA**:
   ```bash
   # Trivy scanning
   trivy image kagent-dev/kagent/controller:latest
   ```
   - **Frequency**: Every image build, daily scans of published images
   - **Tracking**: CI/CD logs, container registry scanning
   - **Threshold**: No high/critical vulnerabilities in published images

5. **SBOM Generation** (Planned for v1.0):
   ```bash
   # Generate SBOM
   syft packages kagent-dev/kagent/controller:latest -o spdx-json
   ```
   - **Format**: SPDX, CycloneDX
   - **Frequency**: Every release
   - **Distribution**: Attached to GitHub releases

**SCA Tracking:**

1. **GitHub Security**:
   - Dependabot alerts for vulnerable dependencies
   - Security advisories for known CVEs
   - Automated PRs for security updates

2. **CI/CD Pipeline**:
   - Automated scans on every PR
   - Block merge if high/critical vulnerabilities found
   - Daily scheduled scans of main branch

3. **Issue Tracking**:
   - Security issues labeled and prioritized
   - CVE tracking in GitHub issues
   - Regular security review meetings

4. **Audit Command**:
   ```bash
   # Run all security audits
   make audit
   ```
   Output includes:
   - Go vulnerabilities
   - Python vulnerabilities
   - JavaScript vulnerabilities
   - Container image CVEs

**SCA Implementation and Timescale:**

**Severity-Based Response:**

1. **Critical Vulnerabilities** (CVSS 9.0-10.0):
   - **Timescale**: 24-48 hours
   - **Process**:
     1. Immediate triage and assessment
     2. Patch or workaround identified
     3. Emergency release if needed
     4. Security advisory published
     5. Notify users via Discord, Slack, GitHub

2. **High Vulnerabilities** (CVSS 7.0-8.9):
   - **Timescale**: 1 week
   - **Process**:
     1. Triage within 24 hours
     2. Fix in next patch release
     3. Include in release notes
     4. Update dependencies

3. **Medium Vulnerabilities** (CVSS 4.0-6.9):
   - **Timescale**: 1 month
   - **Process**:
     1. Triage within 1 week
     2. Fix in next minor release
     3. Include in changelog
     4. Batch with other updates

4. **Low Vulnerabilities** (CVSS 0.1-3.9):
   - **Timescale**: Next major release or as convenient
   - **Process**:
     1. Track in backlog
     2. Fix when updating dependencies
     3. Include in release notes

**Implementation Process:**

1. **Detection**:
   - Automated: CI/CD scans, Dependabot alerts
   - Manual: Security audits, community reports

2. **Triage**:
   - Assess severity and exploitability
   - Determine impact on kagent
   - Check for available patches

3. **Fix**:
   - Update dependency to patched version
   - Apply workaround if patch unavailable
   - Test fix in CI/CD

4. **Release**:
   - Patch release for critical/high
   - Minor release for medium/low
   - Document in release notes

5. **Communication**:
   - Security advisory for critical/high
   - Release notes for all severities
   - Community notification via Discord/Slack

**Example SCA Workflow:**
```bash
# 1. Dependabot detects vulnerability
# 2. Automated PR created

# 3. Review and test
git checkout dependabot/go_modules/golang.org/x/crypto-0.x.x
make test

# 4. Merge and release
git merge dependabot/go_modules/golang.org/x/crypto-0.x.x
make release

# 5. Verify fix
make audit
```

**SCA Metrics:**

**Note**: As an alpha project, formal SCA metrics and SLAs have not been established. The project aims for:
- Rapid detection via automated scanning (CI/CD)
- Timely patching based on severity
- Minimal vulnerability backlog
- Regular dependency updates

Specific metrics (MTTD, MTTP) will be tracked and published as the project matures toward v1.0.

**SCA Documentation:**
- Security policy: [SECURITY.md](https://github.com/kagent-dev/kagent/blob/main/SECURITY.md)
- Audit command: `make audit` in [Makefile](https://github.com/kagent-dev/kagent/blob/main/Makefile)
- CI/CD scans: [.github/workflows/ci.yaml](https://github.com/kagent-dev/kagent/blob/main/.github/workflows/ci.yaml)

### Troubleshooting

**Recovery from Component Failures:**

**1. Kubernetes API Server Failure:**
- **Impact**: Controller cannot reconcile, agents cannot be created/updated
- **Detection**: Controller logs show connection errors, health check fails
- **Recovery**:
  - Controller automatically retries with exponential backoff
  - When API server recovers, controller resumes reconciliation
  - No manual intervention required
  - In-flight agent invocations may fail, clients should retry
- **Mitigation**: Use HA Kubernetes control plane (multiple API servers)

**2. etcd Failure:**
- **Impact**: Kubernetes API server unavailable, complete cluster failure
- **Detection**: Kubernetes control plane unhealthy
- **Recovery**:
  - Kubernetes handles etcd recovery (restore from backup)
  - Kagent automatically reconnects when API server available
  - CRD state restored from etcd backup
- **Mitigation**: Use etcd clustering, regular backups

**3. Database Failure** (PostgreSQL):
- **Impact**: Cannot create sessions, save events, query history
- **Detection**: Controller logs show database connection errors
- **Recovery**:
  - Controller retries connection with exponential backoff
  - When database recovers, operations resume
  - In-flight writes may be lost
- **Mitigation**: 
  - Use PostgreSQL HA (replication, failover)
  - Regular database backups
  - Configure connection pool with retries

**4. Leader Node Failure** (Controller):
- **Impact**: No active controller to reconcile CRDs
- **Detection**: Leader election timeout, new leader elected
- **Recovery**:
  - Automatic leader election among controller replicas
  - New leader takes over within 15-30 seconds
  - Reconciliation resumes automatically
  - No data loss (state in Kubernetes API)
- **Mitigation**: Run 3+ controller replicas for HA

**5. Agent Pod Failure:**
- **Impact**: Specific agent unavailable, invocations fail
- **Detection**: Pod status CrashLoopBackOff, agent not Ready
- **Recovery**:
  - Kubernetes automatically restarts pod
  - If persistent failure, check logs for root cause
  - Controller may recreate deployment if CRD changed
- **Mitigation**: 
  - Run multiple agent replicas
  - Set appropriate resource limits
  - Monitor agent health

**6. LLM Provider Failure:**
- **Impact**: Agent invocations fail, timeout errors
- **Detection**: High error rate in `kagent_llm_requests_total{status="error"}`
- **Recovery**:
  - Automatic retry with exponential backoff (3 attempts)
  - Circuit breaker opens after threshold (planned)
  - Fallback to alternative provider (manual configuration)
- **Mitigation**:
  - Configure multiple model providers
  - Set appropriate timeouts
  - Monitor LLM provider status

**7. Tool Server Failure** (KMCP, MCP servers):
- **Impact**: Tool invocations fail, agents cannot execute tools
- **Detection**: Tool call errors in agent logs
- **Recovery**:
  - Automatic retry for transient failures
  - Agent returns error to user if tool unavailable
  - Tool server pods restart automatically
- **Mitigation**:
  - Run tool servers with multiple replicas
  - Monitor tool server health
  - Graceful degradation (agents work without tools)

**8. Network Partition:**
- **Impact**: Agents cannot reach controller, tools, or LLM providers
- **Detection**: Connection timeout errors, network unreachable
- **Recovery**:
  - Automatic reconnection when network recovers
  - In-flight requests fail, clients retry
  - No data loss (state in Kubernetes API and database)
- **Mitigation**:
  - Use network policies to prevent unnecessary traffic
  - Monitor network health
  - Configure appropriate timeouts

**Known Failure Modes:**

**1. Controller CrashLoopBackOff:**
- **Symptoms**: Controller pod repeatedly crashing
- **Causes**:
  - Database connection failure (invalid credentials, unreachable)
  - CRD validation errors (corrupted CRDs)
  - Out of memory (insufficient resources)
  - Panic in reconciliation loop (bug)
- **Diagnosis**:
  ```bash
  kubectl logs -n kagent deployment/kagent-controller --previous
  kubectl describe pod -n kagent -l app.kubernetes.io/name=kagent-controller
  ```
- **Resolution**:
  - Check database connectivity
  - Verify CRD definitions
  - Increase resource limits
  - Report bug if panic

**2. Agent Not Ready:**
- **Symptoms**: Agent pod running but not Ready, invocations fail
- **Causes**:
  - Model config invalid (bad API key, wrong endpoint)
  - Tool server unreachable
  - Resource constraints (OOMKilled)
  - Image pull failure
- **Diagnosis**:
  ```bash
  kubectl describe agent -n kagent <agent-name>
  kubectl logs -n kagent deployment/<agent-name>
  kubectl get events -n kagent --field-selector involvedObject.name=<agent-name>
  ```
- **Resolution**:
  - Verify model config and secrets
  - Check tool server availability
  - Increase resources
  - Verify image exists and is pullable

**3. Agent Invocation Timeout:**
- **Symptoms**: Agent invocation takes >600s, times out
- **Causes**:
  - LLM provider slow or rate limited
  - Tool execution slow (e.g., large Kubernetes query)
  - Network latency
  - Agent stuck in loop
- **Diagnosis**:
  ```bash
  kubectl logs -n kagent deployment/<agent-name> --tail=100
  # Check traces in Jaeger for slow operations
  ```
- **Resolution**:
  - Increase timeout in controller config
  - Optimize tool queries
  - Check LLM provider status
  - Review agent system message for loops

**4. Database Connection Pool Exhausted:**
- **Symptoms**: "too many connections" errors, slow API responses
- **Causes**:
  - High concurrent load
  - Connection leaks (bug)
  - Insufficient pool size
- **Diagnosis**:
  ```bash
  kubectl logs -n kagent deployment/kagent-controller | grep "connection"
  # Check metric: database_connections{state="in_use"}
  ```
- **Resolution**:
  - Increase connection pool size
  - Scale controller replicas
  - Fix connection leaks (report bug)

**5. Out of Memory (OOMKilled):**
- **Symptoms**: Pods terminated with OOMKilled, frequent restarts
- **Causes**:
  - Memory leak (bug)
  - Large conversation history
  - Insufficient memory limits
- **Diagnosis**:
  ```bash
  kubectl describe pod -n kagent <pod-name>
  # Check metric: container_memory_usage_bytes
  ```
- **Resolution**:
  - Increase memory limits
  - Configure session retention (limit history)
  - Report memory leak

**6. CRD Validation Failure:**
- **Symptoms**: `kubectl apply` fails with validation error
- **Causes**:
  - Invalid CRD spec (wrong field type, missing required field)
  - CRD version mismatch (old CRD, new controller)
- **Diagnosis**:
  ```bash
  kubectl apply -f agent.yaml --dry-run=server -o yaml
  kubectl get crd agents.kagent.dev -o yaml
  ```
- **Resolution**:
  - Fix CRD spec according to error message
  - Upgrade CRDs to match controller version
  - Validate against schema

**7. Leader Election Failure:**
- **Symptoms**: Multiple controllers active, conflicting reconciliations
- **Causes**:
  - Network partition
  - Lease timeout too short
  - Clock skew
- **Diagnosis**:
  ```bash
  kubectl get lease -n kagent
  kubectl logs -n kagent deployment/kagent-controller | grep "leader"
  ```
- **Resolution**:
  - Check network connectivity
  - Increase lease duration
  - Sync node clocks (NTP)

**8. Image Pull Failure:**
- **Symptoms**: Pods stuck in ImagePullBackOff
- **Causes**:
  - Image doesn't exist
  - Registry unreachable
  - Authentication failure (imagePullSecrets)
  - Rate limit exceeded
- **Diagnosis**:
  ```bash
  kubectl describe pod -n kagent <pod-name>
  kubectl get events -n kagent
  ```
- **Resolution**:
  - Verify image exists in registry
  - Check registry connectivity
  - Configure imagePullSecrets
  - Use cached images or mirror registry

**Troubleshooting Tools:**

1. **Logs**:
   ```bash
   # Controller logs
   kubectl logs -n kagent deployment/kagent-controller --tail=100 -f
   
   # Agent logs
   kubectl logs -n kagent deployment/<agent-name> --tail=100 -f
   
   # Previous pod logs (after crash)
   kubectl logs -n kagent <pod-name> --previous
   ```

2. **Events**:
   ```bash
   kubectl get events -n kagent --sort-by='.lastTimestamp'
   kubectl get events -n kagent --field-selector involvedObject.name=<resource-name>
   ```

3. **Describe**:
   ```bash
   kubectl describe agent -n kagent <agent-name>
   kubectl describe pod -n kagent <pod-name>
   kubectl describe deployment -n kagent <deployment-name>
   ```

4. **Metrics**:
   ```bash
   kubectl port-forward -n kagent svc/kagent-controller 8083:8083
   curl http://localhost:8083/metrics | grep kagent
   ```

5. **Traces**:
   - Open Jaeger UI
   - Search for traces by agent name or operation
   - Identify slow spans

6. **Database**:
   ```bash
   # Check database connectivity
   kubectl exec -n kagent deployment/kagent-controller -- \
     psql $DATABASE_URL -c "SELECT 1"
   
   # Check session count
   kubectl exec -n kagent deployment/kagent-controller -- \
     psql $DATABASE_URL -c "SELECT COUNT(*) FROM sessions"
   ```

**Troubleshooting Guides:**
- [DEVELOPMENT.md#troubleshooting](https://github.com/kagent-dev/kagent/blob/main/DEVELOPMENT.md#troubleshooting)
- Common issues: https://kagent.dev/docs/kagent (planned)
- Discord support: https://discord.gg/Fu3k65f2k3

### Compliance

**Third-Party Attribution and Licensing:**

Kagent ensures proper attribution and licensing through:

**1. License Compliance:**
- **Project License**: Apache 2.0 (permissive, CNCF-compatible)
- **License File**: [LICENSE](https://github.com/kagent-dev/kagent/blob/main/LICENSE) in repository root
- **Dependency Licenses**: All dependencies use Apache 2.0, MIT, BSD, or other permissive licenses
- **License Scanning**: Automated checks in CI/CD to detect incompatible licenses

**2. Attribution Process:**
- **Code Headers**: All source files include Apache 2.0 license header
  ```go
  /*
  Copyright 2025.
  
  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at
  
      http://www.apache.org/licenses/LICENSE-2.0
  */
  ```
- **NOTICE File**: Planned for v1.0, will include all third-party attributions
- **Dependency Attribution**: Tracked via dependency management files

**CNCF Attribution Recommendations:**

Following [CNCF attribution guidelines](https://github.com/cncf/foundation/blob/main/policies-guidance/recommendations-for-attribution.md):

**1. Third-Party Code in Source Files:**

**Management**:
- **Prohibited**: Direct incorporation of third-party code into kagent source files
- **Process**: All third-party code used as dependencies via package managers
- **Exception**: If needed, code is:
  1. Clearly marked with original license header
  2. Listed in NOTICE file
  3. Original license included in repository
  4. Attribution in comments

**Example** (if needed):
```go
// Copyright (c) 2024 Original Author
// Licensed under MIT License
// Source: https://github.com/original/repo
// Modified by kagent team

// Original code with modifications
func helperFunction() {
    // ...
}
```

**2. Unmodified Third-Party Components:**

**Management**:
- **Vendoring**: Not used, all dependencies via package managers
- **Submodules**: Not used
- **Copied Files**: Prohibited without clear attribution
- **License Preservation**: All dependency licenses preserved in their packages

**If Vendored** (not current practice):
- Original LICENSE file preserved
- README with attribution
- No modifications without documentation

**3. Build-Time Dependencies in Artifacts:**

**Go Binaries** (Controller, CLI):
```bash
# Generate dependency list
go list -m all > DEPENDENCIES.txt

# Include in binary (planned)
go build -ldflags="-X main.dependencies=$(cat DEPENDENCIES.txt)"
```

**Container Images**:
- **Base Image Attribution**: Documented in Dockerfile
  ```dockerfile
  # Base image: cgr.dev/chainguard/go:1.24
  # License: Apache 2.0
  # Source: https://github.com/chainguard-images/images
  FROM cgr.dev/chainguard/go:1.24
  ```

- **SBOM Generation** (Planned for v1.0):
  ```bash
  # Generate SBOM for container image
  syft packages kagent-dev/kagent/controller:latest -o spdx-json > sbom.spdx.json
  syft packages kagent-dev/kagent/controller:latest -o cyclonedx-json > sbom.cyclonedx.json
  ```

- **SBOM Attachment**: SBOMs attached to container images as OCI artifacts
- **SBOM Distribution**: Published with GitHub releases

**Python Packages** (Agent Runtime):
```bash
# List all dependencies with licenses
uv pip list --format=json | jq '.[] | {name, version, license}'

# Generate requirements with hashes
uv pip freeze > requirements.txt
```

**JavaScript Packages** (UI):
```bash
# List all dependencies with licenses
npm list --json | jq '.dependencies | to_entries[] | {name: .key, version: .value.version}'

# Generate license report
npx license-checker --json > licenses.json
```

**Attribution in Artifacts:**

**Planned for v1.0:**
1. **NOTICE File**: Comprehensive list of all dependencies and their licenses
2. **LICENSE Directory**: Contains licenses for all dependencies
3. **SBOM**: Machine-readable software bill of materials
4. **Container Labels**: OCI labels with attribution information
   ```dockerfile
   LABEL org.opencontainers.image.licenses="Apache-2.0"
   LABEL org.opencontainers.image.source="https://github.com/kagent-dev/kagent"
   LABEL org.opencontainers.image.vendor="kagent-dev"
   ```

**License Compatibility:**

**Approved Licenses** (compatible with Apache 2.0):
- Apache 2.0
- MIT
- BSD (2-clause, 3-clause)
- ISC
- CC0 (public domain)

**Prohibited Licenses**:
- GPL, LGPL, AGPL (copyleft, incompatible with Apache 2.0)
- Proprietary licenses without permission
- Unknown licenses

**License Scanning**:
```bash
# Check Go dependencies
go-licenses check ./...

# Check Python dependencies
uv pip list | grep -i gpl  # Should return nothing

# Check JavaScript dependencies
npx license-checker --onlyAllow "Apache-2.0;MIT;BSD;ISC;CC0"
```

**Compliance Verification:**

**Automated Checks** (CI/CD):
1. License header check on all source files
2. Dependency license scanning
3. SBOM generation and validation
4. Attribution completeness check

**Manual Reviews**:
1. Quarterly dependency audit
2. New dependency approval process
3. License compatibility review

**Compliance Documentation:**
- License policy: [CONTRIBUTION.md#license](https://github.com/kagent-dev/kagent/blob/main/CONTRIBUTION.md#license)
- Attribution guidelines: Planned for v1.0
- SBOM generation: Planned for v1.0

**Third-Party Notices:**

**Current Practice**:
- Dependency licenses viewable via package manager commands
- No direct code incorporation requiring attribution

**Planned for v1.0**:
- NOTICE file with all attributions
- Automated NOTICE file generation
- SBOM in SPDX and CycloneDX formats
- Container image labels with attribution

### Security

**Security Hygiene:**

**Access Control Implementation:**

Kagent implements multi-layered access control:

**1. Kubernetes RBAC:**
- **Controller Access**: ServiceAccount with ClusterRole for CRD management
  - Permissions defined in [go/config/rbac/role.yaml](https://github.com/kagent-dev/kagent/blob/main/go/config/rbac/role.yaml)
  - Minimal privileges: read/write CRDs, deployments, services, secrets, configmaps
  - No cluster-admin privileges required

- **Agent Access**: Per-agent ServiceAccounts with scoped permissions
  - Default: Read-only access to Kubernetes resources
  - Customizable: Per-agent RBAC policies in Helm charts
  - Examples: [helm/agents/k8s/templates/rbac.yaml](https://github.com/kagent-dev/kagent/tree/main/helm/agents)
  - Principle of least privilege: Agents only get permissions they need

- **User Access**: Kubernetes RBAC for kubectl/API access
  - Separate ClusterRoles for read-only and write access
  - `kagent-getter-role`: List and view agents, model configs
  - `kagent-writer-role`: Create, update, delete resources

**2. API Authentication** (Current and Planned):

**Current State**:
- **Development Mode**: UnsecureAuthenticator (no authentication)
  - Suitable for development/testing only
  - Not recommended for production

- **A2A Authentication**: A2AAuthenticator for agent-to-agent communication
  - Token-based authentication
  - Trace context propagation

**Planned** ([Issue #476](https://github.com/kagent-dev/kagent/issues/476)):
- **API Key Authentication**: Header-based API keys
- **OAuth 2.0**: Integration with identity providers
- **Service Account Tokens**: Kubernetes service account JWT validation
- **mTLS**: Mutual TLS for agent-to-controller communication

**Implementation**:
- Pluggable authentication framework in [go/pkg/auth/auth.go](https://github.com/kagent-dev/kagent/blob/main/go/pkg/auth/auth.go)
- `AuthProvider` interface for custom authentication
- `Authorizer` interface for authorization decisions

**3. Secret Management:**
- **LLM API Keys**: Stored in Kubernetes Secrets
  - Referenced via `apiKeySecretRef` in ModelConfig
  - Never logged or exposed in API responses
  - Mounted as environment variables in agent pods

- **Database Credentials**: Stored in Kubernetes Secrets
  - Mounted as environment variables in controller
  - Connection strings not logged

- **TLS Certificates**: Stored in Kubernetes Secrets
  - CA certificates for custom LLM endpoints
  - Referenced via `tls.caCert` in ModelConfig

- **Secret Isolation**: No cross-namespace secret access
  - Agents can only access secrets in their namespace
  - Controller has limited secret access (only for agents it manages)

**4. Network Policies** (Optional, User-Configured):
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: kagent-network-policy
  namespace: kagent
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/part-of: kagent
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: kagent
  egress:
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 443  # HTTPS for LLM providers
    - protocol: TCP
      port: 6443  # Kubernetes API
```

**5. Pod Security Standards:**
- **Restricted**: Recommended for production
  - Non-root user execution
  - Read-only root filesystem (where possible)
  - No privileged containers
  - Drop all capabilities

- **Baseline**: Default for development
  - Prevents most privilege escalations
  - Allows some flexibility for debugging

**6. Session Isolation:**
- **Database-Backed**: Sessions stored in database with user/agent association
- **No Cross-User Access**: Users cannot access other users' sessions
- **Planned**: Full multi-tenancy with namespace-based isolation ([Issue #476](https://github.com/kagent-dev/kagent/issues/476))

**Access Control Verification:**
```bash
# Check controller RBAC
kubectl describe clusterrole manager-role

# Check agent RBAC
kubectl describe clusterrole kagent-k8s-agent-role

# Verify secret access
kubectl auth can-i get secrets --as=system:serviceaccount:kagent:k8s-agent -n kagent

# Test API authentication (when enabled)
curl -H "Authorization: Bearer $TOKEN" http://kagent-controller:8083/api/agents
```

**Cloud Native Threat Modeling:**

**Security Reporting Team:**

**Current Team**:
- Project maintainers serve as security reporting team
- Contact: kagent-vulnerability-reports@googlegroups.com
- Private Google Group for confidential reports

**Diversity and Representation**:

**Current State**:
- Team consists of maintainers from multiple organizations (Amdocs, Au10tix, Krateo, individual contributors)
- Geographic diversity: North America, Europe, Middle East
- Organizational diversity: Telecommunications, identity verification, platform engineering, independent developers

**Planned Improvements**:
- Formalize security team structure
- Document security team charter
- Establish diversity goals (organizational, geographic, individual)
- Quarterly review of team composition

**Invitation and Rotation Process:**

**Current Process** (Informal):
1. Maintainers with security expertise invited to security group
2. No formal rotation policy

**Planned Process** (v1.0):

**Invitation Criteria**:
- Active project contributor (6+ months)
- Demonstrated security expertise
- Commitment to responsible disclosure
- Availability for security response (24-48 hour response time)
- Organizational diversity (no more than 50% from single organization)

**Invitation Process**:
1. Existing security team nominates candidate
2. Candidate agrees to security team charter
3. Candidate added to private security group
4. Announcement in public channels (with candidate's consent)

**Rotation Policy**:
- **Term Length**: 1-2 years
- **Rotation**: Staggered, 1-2 members per year
- **Emeritus Status**: Former members available for consultation
- **Minimum Size**: 3-5 active members
- **Maximum Size**: 7-10 members

**Rotation Process**:
1. Annual review of security team composition
2. Members can opt-out or be rotated based on activity
3. New members invited based on criteria
4. Knowledge transfer period (1 month overlap)

**Security Team Responsibilities**:
- Triage security reports within 24 hours
- Coordinate security fixes and releases
- Write security advisories
- Maintain security documentation
- Conduct security reviews of major features
- Participate in threat modeling sessions

**Security Team Charter** (Planned):
- Confidentiality requirements
- Response time commitments
- Conflict of interest policy
- Diversity and inclusion goals
- Communication protocols

**Security Reporting Process**:
1. **Report**: Email kagent-vulnerability-reports@googlegroups.com
2. **Acknowledgment**: Within 24 hours
3. **Triage**: Within 48 hours (severity assessment)
4. **Fix**: Based on severity (24 hours to 1 month)
5. **Disclosure**: Coordinated with reporter
6. **Advisory**: Published after fix available

**Security Team Transparency**:
- Security advisories published on GitHub
- Annual security report (planned)
- Public security metrics (MTTD, MTTP)
- Security team membership listed in SECURITY.md (planned)

**Community Security Participation**:
- Bug bounty program (planned for v1.0)
- Security champions program (planned)
- Security office hours (planned)
- Security training for contributors (planned)

**Security Documentation**:
- [SECURITY.md](https://github.com/kagent-dev/kagent/blob/main/SECURITY.md) - Reporting process
- [Security Self-Assessment](https://github.com/kagent-dev/kagent/blob/main/contrib/cncf/security-self-assessment.md) - Comprehensive security analysis
- Security team charter (planned)
- Threat model documentation (planned)
 