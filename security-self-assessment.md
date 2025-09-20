# kagent Security Self-Assessment

## Table of Contents

- [Metadata](#metadata)
  - [Version history](#version-history)
  - [Security Links](#security-links)
- [Overview](#overview)
  - [Background](#background)
  - [Actors](#actors)
  - [Actions](#actions)
  - [Goals](#goals)
  - [Non-goals](#non-goals)
- [Self-Assessment Use](#self-assessment-use)
- [Security Functions and Features](#security-functions-and-features)
  - [Critical](#critical)
  - [Security Relevant](#security-relevant)
- [Project Compliance](#project-compliance)
  - [Future State](#future-state)
- [Secure Development Practices](#secure-development-practices)
  - [Development Pipeline](#development-pipeline)
  - [Communication Channels](#communication-channels)
  - [Ecosystem](#ecosystem)
- [Security Issue Resolution](#security-issue-resolution)
  - [Responsible Disclosure Process](#responsible-disclosure-process)
  - [Incident Response](#incident-response)
- [Appendix](#appendix)
  - [Known Issues Over Time](#known-issues-over-time)
  - [Open SSF Best Practices](#open-ssf-best-practices)
  - [Case Studies](#case-studies)
  - [Related Projects / Vendors](#related-projects--vendors)

## Metadata

### Version history

|   |  |
| - | - |
| September 19, 2025 | Initial Draft _(Sam Heilbron, Lin Sun)_  |
|  |  |

### Security Links

|   |  |
| - | - |
| Software         | [kagent Repository](https://github.com/kagent-dev/kagent) |
| Security Policy                  | [SECURITY.md](SECURITY.md)              |
| Security Provider |  No. kagent is designed to facilitate security and compliance validation, but it should not be considered a security provider.  |
| Languages        | Go, Python, TypeScript/JavaScript |
| Security Insights | See [Project Compliance > Future State](#future-state) |
| Security File | See [Project Compliance > Future State](#future-state) |
| Cosign pub-key | See [Project Compliance > Future State](#future-state) |
|   |  |

## Overview

kagent is a Kubernetes native framework for building AI agents. It provides a comprehensive platform that makes it easy to build, deploy, and manage AI agents in Kubernetes environments, offering declarative configuration, extensible tooling, and observability features.

### Background

kagent addresses the growing need for AI-powered automation in cloud-native environments. As organizations increasingly adopt Kubernetes for orchestration, there's a need for intelligent agents that can understand, monitor, and operate within these complex distributed systems. kagent provides a framework that bridges AI capabilities with Kubernetes-native operations, enabling organizations to build sophisticated AI agents for tasks like cluster management, troubleshooting, and automated operations.

### Actors

**Controller**: The Kubernetes controller that watches kagent custom resources (Agents, ModelConfigs, ToolServers, Memory) and creates the necessary resources to run the agents. It manages the lifecycle of AI agents and their dependencies.

**UI**: A web-based user interface that allows users to manage agents, view execution logs, configure tools, and monitor agent performance. It provides both administrative and operational views.

**Engine**: The runtime engine that executes AI agents using the Agent Development Kit (ADK). It handles agent execution, tool invocation, and session management.

**CLI**: A command-line interface that enables developers and operators to interact with kagent resources, deploy agents, and manage configurations programmatically.

**Agents**: AI-powered entities that perform specific tasks within the Kubernetes environment. They can access tools, maintain memory, and interact with various Kubernetes resources and external systems.

**MCP Servers**: Model Context Protocol servers that provide tools and capabilities to agents. These include built-in tools for Kubernetes, Istio, Helm, Argo, Prometheus, Grafana, and Cilium operations.

### Actions

**Agent Deployment**: The controller receives agent specifications via Kubernetes custom resources, validates configurations, creates necessary resources (deployments, services, secrets), and manages the agent lifecycle. Security checks include RBAC validation and resource quota enforcement.

**Tool Execution**: Agents invoke tools through MCP servers to perform operations like querying Kubernetes APIs, accessing monitoring data, or modifying cluster resources. Security controls include authentication, authorization checks, and audit logging.

**Memory Operations**: Agents store and retrieve contextual information through the memory system backed by vector databases (Qdrant). Data is encrypted at rest and access is controlled through session-based authentication.

**Model Communication**: Agents communicate with various LLM providers (OpenAI, Anthropic, Azure OpenAI, Google Vertex AI, Ollama) through configured model providers. API keys and credentials are securely managed through Kubernetes secrets.

**Session Management**: The system maintains agent sessions with authentication and authorization context, ensuring proper isolation between different users and agents.

### Goals

- **Kubernetes-Native AI Agents**: Provide a framework for building AI agents that operate naturally within Kubernetes environments with full integration of Kubernetes security models.
- **Secure Multi-Tenancy**: Enable multiple users and teams to deploy and manage their own agents with proper isolation and access controls.
- **Extensible Tool Ecosystem**: Offer a secure and extensible system for agents to access various tools and services while maintaining proper authorization boundaries.
- **Observable Operations**: Provide comprehensive observability for agent operations, including audit trails, performance metrics, and security events.
- **Declarative Configuration**: Enable infrastructure-as-code practices for agent deployment and management with version control and review processes.

### Non-goals

- **Direct Cluster Administration**: kagent does not replace Kubernetes RBAC or cluster security policies; it operates within existing security boundaries.
- **LLM Model Hosting**: kagent does not host or provide LLM models; it integrates with external model providers.
- **General-Purpose Computing Platform**: kagent is specifically designed for AI agent workloads and is not intended as a general application platform.
- **Real-Time System Guarantees**: kagent does not provide hard real-time guarantees for agent operations or tool executions.

## Self-Assessment Use

This self-assessment is created by the kagent team to perform an internal analysis of the project's security. It is not intended to provide a security audit of kagent, or function as an independent assessment or attestation of kagent's security health.

This document serves to provide kagent users with an initial understanding of kagent's security, where to find existing security documentation, kagent plans for security, and general overview of kagent security practices, both for development of kagent as well as security of kagent.

This document provides the CNCF TAG-Security with an initial understanding of kagent to assist in a joint-assessment, necessary for projects under incubation. Taken together, this document and the joint-assessment serve as a cornerstone for if and when kagent seeks graduation and is preparing for a security audit.

## Security Functions and Features

### Critical

- **Authentication System**: Implements a pluggable authentication system with support for various authentication providers. Currently includes UnsecureAuthenticator for development and A2AAuthenticator for agent-to-agent communication. The system supports session-based authentication with proper principal management.

- **Authorization Framework**: Provides a comprehensive authorization system with role-based access control. The Authorizer interface enables fine-grained permission checks for different operations (get, create, update, delete) on various resources.

- **Kubernetes RBAC Integration**: Leverages Kubernetes native RBAC for controlling access to cluster resources. Agents operate within defined service accounts with specific permissions, ensuring principle of least privilege.

- **Secret Management**: Integrates with Kubernetes secret management for storing sensitive data like API keys, credentials, and configuration data. Secrets are automatically mounted into agent containers and accessed through secure channels.

- **Container Security**: All container images are scanned for vulnerabilities using Trivy in the CI/CD pipeline, with high and critical severity issues blocking releases.

### Security Relevant

- **Audit Logging**: Comprehensive logging of agent operations, tool executions, and administrative actions for security monitoring and compliance purposes.

- **Network Policies**: Support for Kubernetes network policies to control communication between agents, tools, and external services.

- **Resource Quotas**: Integration with Kubernetes resource quotas and limits to prevent resource exhaustion attacks and ensure fair resource allocation.

- **TLS Encryption**: All communication between components uses TLS encryption, including agent-to-controller, UI-to-backend, and external API communications.

- **Input Validation**: Comprehensive input validation for all API endpoints, configuration files, and user inputs to prevent injection attacks and malformed data processing.

- **Session Isolation**: Proper session isolation ensures that different users and agents cannot access each other's data or operations.

## Project Compliance

- **Apache 2.0 License**: The project is licensed under Apache 2.0, ensuring open source compliance and clear licensing terms.
- **Kubernetes Security Standards**: Follows Kubernetes security best practices including Pod Security Standards, RBAC, and network policies.
- **Container Security**: Adheres to container security best practices with vulnerability scanning, minimal base images, and non-root execution where possible.

### Future State

In the future, kagent intends to build and maintain compliance with several industry standards and frameworks:

**Supply Chain Levels for Software Artifacts (SLSA)**:

- All release artifacts include signed provenance attestations with cryptographic verification
- Build process isolation and non-falsifiable provenance are implemented
- Both container images and release binaries have complete SLSA provenance chains

**Container Security Standards**:

- All container images are signed with Cosign using keyless signing
- Software Bill of Materials (SBOM) generation for all releases
- Multi-architecture container builds with attestation

## Secure Development Practices

### Development Pipeline

- **Code Reviews**: All code changes require review from at least one maintainer before merging. Reviews focus on functionality, security implications, and adherence to coding standards.

- **Automated Testing**: Comprehensive test suite including unit tests, integration tests, and end-to-end tests. Tests are automatically run on all pull requests and must pass before merging.

- **Vulnerability Scanning**: Container images are automatically scanned for vulnerabilities using Trivy. High and critical vulnerabilities block the release process until resolved.

- **Dependency Management**: Regular dependency updates and security scanning of dependencies. Go modules, Python packages, and npm packages are monitored for known vulnerabilities.

- **Static Code Analysis**: Automated linting for Go code to identify potential security issues and maintain code quality.

- **Signed Commits**: The project requires signed commits and is evaluating requirements for commit signing.

### Communication Channels

- **Internal**: Team members communicate through Discord (#core-contrib channel), GitHub discussions, and regular community meetings.
- **Inbound**: Users can communicate with the team through GitHub issues, Discord community channels, and the [kagent-vulnerability-reports@googlegroups.com](kagent-vulnerability-reports@googlegroups.com) security email for vulnerability reports.
- **Outbound**: The team communicates with users through GitHub releases, documentation updates, Discord announcements, and community meetings.

### Ecosystem

kagent operates within the cloud-native ecosystem as a Kubernetes-native application. It integrates with:

- **Kubernetes**: Native integration with Kubernetes APIs, RBAC, and resource management
- **Helm**: Deployment and management through Helm charts
- **Prometheus/Grafana**: Observability and monitoring integration
- **OpenTelemetry**: Distributed tracing and observability
- **Vector Databases**: Integration with Qdrant for agent memory storage
- **LLM Providers**: Secure integration with major AI model providers
- **MCP Ecosystem**: Extensible tool system through Model Context Protocol

## Security Issue Resolution

### Responsible Disclosure Process

- **Reporting**: Security vulnerabilities should be reported to kagent-vulnerability-reports@googlegroups.com. This private Google group ensures confidential handling of security issues.

- **Response Process**: The kagent team evaluates vulnerability reports for severity level, impact on kagent code, and potential dependencies on third-party code. The team strives to keep vulnerability information private on a need-to-know basis during the remediation process.

- **Communication**: Reporters receive acknowledgment of their report and are kept informed of the remediation progress. Public disclosure is coordinated with the reporter after fixes are available.

### Incident Response

- **Triage**: Security incidents are triaged based on severity, impact, and exploitability. Critical issues receive immediate attention with dedicated resources.

- **Confirmation**: The team works to reproduce and confirm reported vulnerabilities, assessing their impact on different deployment scenarios.

- **Notification**: Stakeholders are notified through appropriate channels based on the severity of the issue. This may include security advisories, GitHub security alerts, and community notifications.

- **Patching**: Security fixes are prioritized and released through regular release channels or emergency patches for critical issues. Patches are thoroughly tested before release.

- **Post-Incident Review**: After resolution, the team conducts post-incident reviews to identify process improvements and prevent similar issues.

## Appendix

### Known Issues Over Time

As of the time of this assessment, no critical security vulnerabilities have been publicly reported or discovered in kagent. The project maintains a clean security record with proactive vulnerability scanning and security practices in place.

### Open SSF Best Practices

kagent is working toward OpenSSF Best Practices certification. That work is tracked by [https://github.com/kagent-dev/community/issues/9](https://github.com/kagent-dev/community/issues/9).

### Case Studies

1. **Kubernetes Troubleshooting Agent**: A large enterprise uses kagent to deploy AI agents that automatically diagnose and resolve common Kubernetes issues. The agents operate with limited RBAC permissions and provide detailed audit logs of their actions.

2. **Multi-Tenant Development Platform**: A platform company uses kagent to provide AI-powered assistance to their development teams. Each team has isolated agents with access only to their namespace resources, demonstrating the multi-tenancy capabilities.

3. **Automated Operations Agent**: An organization uses kagent to deploy agents that monitor cluster health and perform automated scaling decisions. The agents integrate with Prometheus for metrics and use secure tool access patterns for cluster modifications.

### Related Projects / Vendors

- **Kubernetes Operators**: While Kubernetes operators provide automation, kagent adds AI-powered decision making and natural language interfaces.
- **Traditional Monitoring Tools**: Unlike passive monitoring tools, kagent enables active, intelligent responses to system conditions.
- **AI/ML Platforms**: kagent focuses specifically on operational AI agents rather than model training or general ML workloads.
- **Automation Frameworks**: kagent provides higher-level, AI-driven automation compared to traditional scripting or workflow automation tools.

The key differentiator is kagent's focus on AI-powered, Kubernetes-native agents that can understand context, make intelligent decisions, and operate safely within existing security boundaries.
