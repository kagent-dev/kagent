# kagent Security Self-Assessment

This document provides a self-assessment of the kagent project following the guidelines outlined by the [CNCF TAG Security and Compliance group](https://tag-security.cncf.io/community/assessments/guide/self-assessment/#self-assessment). The purpose is to evaluate kagent's current security posture and alignment with best practices, ensuring that it is suitable for adoption at a CNCF incubation level.

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
| September 22, 2025 | Initial Draft _(Sam Heilbron)_  |
|  |  |

### Security Links

|   |  |
| - | - |
| Software | [kagent Repository](https://github.com/kagent-dev/kagent) |
| Security Policy | [SECURITY.md](SECURITY.md) |
| Security Provider | No. kagent is designed to facilitate security and compliance validation, but it should not be considered a security provider. |
| Languages | Go, Python, TypeScript/JavaScript |
| Security Insights | See [Project Compliance > Future State](#future-state) |
| Security File | See [Project Compliance > Future State](#future-state) |
| Cosign pub-key | See [Project Compliance > Future State](#future-state) |
|   |  |

## Overview

Kagent is an innovative AI agent platform designed specifically for Kubernetes environments. It empowers developers and operations teams to create intelligent, autonomous agents that can monitor, manage, and automate complex Kubernetes workloads using the power of large language models (LLMs).

### Background

kagent addresses the growing need for AI-powered automation in cloud-native environments. As organizations increasingly adopt Kubernetes for orchestration, there's a need for intelligent agents that can understand, monitor, and operate within these complex distributed systems. kagent provides the tools and framework to bring AI to your Kubernetes infrastructure.

### Actors

**Controller**: The Kubernetes controller that watches kagent custom resources and creates the necessary resources to run the agents. It manages the lifecycle of AI agents and their dependencies.

**Engine**: The runtime engine that is responsible for running the agent's conversation loop. It is built on top of the ADK framework. It handles agent execution and tool invocation and session management.

**CLI**: A command-line interface that enables developers and operators to interact with kagent resources, deploy agents, and manage configurations programmatically.

**UI**: A web-based user interface that allows users to manage agents, view execution logs, configure tools, and monitor agent performance.

**Agents**: AI-powered entities that perform specific tasks within the Kubernetes environment. They can access tools, and interact with various Kubernetes resources and external systems.

**MCP Servers**: Model Context Protocol servers that provide tools and capabilities to agents. These include built-in tools for Kubernetes, Istio, Helm, Argo, Prometheus, Grafana, and Cilium operations.

### Actions

**Agent Deployment**: The controller receives agent specifications via Kubernetes custom resources, validates configurations, creates necessary resources (deployments, services, secrets), and manages the agent lifecycle. Security checks include RBAC validation and resource quota enforcement.

**Tool Execution**: Agents invoke tools through MCP servers to perform operations like querying Kubernetes APIs, accessing monitoring data, or modifying cluster resources.

**Model Communication**: Agents communicate with various LLM providers (OpenAI, Anthropic, Azure OpenAI, Google Vertex AI, Ollama) through configured model providers. API keys and credentials are securely managed through Kubernetes secrets.

### Goals

- **Kubernetes-Native AI Agents**: Provide a framework for building AI agents that operate naturally within Kubernetes environments with full integration of Kubernetes security models.
- **Secure Multi-Tenancy**: Enable multiple users and teams to deploy and manage their own agents with proper isolation and access controls. This is not yet implemented, but is on the project roadmap.
- **Extensible Tool Ecosystem**: Offer a secure and extensible system for agents to access various tools and services while maintaining proper authorization boundaries.
- **Declarative Configuration**: Enable infrastructure-as-code practices for agent deployment and management with version control and review processes.

### Non-goals

- **Direct Cluster Administration**: kagent does not replace Kubernetes RBAC or cluster security policies; it operates within existing security boundaries.
- **LLM Model Hosting**: kagent does not host or provide LLM models; it integrates with external model providers.

## Self-Assessment Use

This self-assessment is created by the kagent team to perform an internal analysis of the project's security. It is not intended to provide a security audit of kagent, or function as an independent assessment or attestation of kagent's security health.

This document serves to provide kagent users with an initial understanding of kagent's security, where to find existing security documentation, kagent plans for security, and general overview of kagent security practices, both for development of kagent as well as security of kagent.

This document provides the CNCF TAG-Security with an initial understanding of kagent to assist in a joint-assessment, necessary for projects under incubation. Taken together, this document and the joint-assessment serve as a cornerstone for if and when kagent seeks graduation and is preparing for a security audit.

## Security Functions and Features

### Critical

- **Authentication System**: Currently includes UnsecureAuthenticator for development and A2AAuthenticator for agent-to-agent communication. https://github.com/kagent-dev/kagent/issues/476 is on the roadmap to create a more extensible authentication and authorization system.

- **Secret Management**: Integrates with Kubernetes secret management for storing sensitive data like API keys, credentials, and configuration data. Secrets are automatically mounted into agent containers and accessed through secure channels. The kagent API does not allow for any cross namespace referencing of secrets and/or resources which reference secrets. We are interested in potentially adding this ability via something like a ReferenceGrant in the future.

- **Container Security**: All container images are scanned for vulnerabilities using Trivy in the CI/CD pipeline, with high and critical severity issues blocking releases.

### Security Relevant

- **Audit Logging**: Comprehensive logging of agent operations, tool executions, and administrative actions for security monitoring and compliance purposes.

- **Network Policies**: Support for Kubernetes network policies to control communication between agents, tools, and external services.

- **Resource Quotas**: Integration with Kubernetes resource quotas and limits to prevent resource exhaustion attacks and ensure fair resource allocation.

- **Session Isolation**: Session isolation ensures that different users and agents cannot access each other's data or operations.  [https://github.com/kagent-dev/kagent/issues/476](https://github.com/kagent-dev/kagent/issues/476) is on the roadmap to properly support session isolation.

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

- **Vulnerability Scanning**: Container images are automatically scanned for vulnerabilities using Trivy. High and critical vulnerabilities are reported to the dev team.

- **Dependency Management**: Regular dependency updates and security scanning of dependencies. Go modules, Python packages, and npm packages are monitored for known vulnerabilities.

- **Static Code Analysis**: Automated linting for Go code to identify potential security issues and maintain code quality.

- **Signed Commits**: The project requires signed commits via DCO.

### Communication Channels

|   |  |
| - | - |
| Documentation | https://kagent.dev/docs/kagent |
| Contributing | https://github.com/kagent-dev/kagent/blob/main/CONTRIBUTION.md |
| Slack | https://cloud-native.slack.com/archives/C08ETST0076 |
| Discord | https://discord.com/invite/Fu3k65f2k3 |
| Community meetings | https://calendar.google.com/calendar/u/0?cid=Y183OTI0OTdhNGU1N2NiNzVhNzE0Mjg0NWFkMzVkNTVmMTkxYTAwOWVhN2ZiN2E3ZTc5NDA5Yjk5NGJhOTRhMmVhQGdyb3VwLmNhbGVuZGFyLmdvb2dsZS5jb20 |
| | |

### Ecosystem

kagent operates within the cloud-native ecosystem as a Kubernetes-native application. It integrates with other technologies in this ecosystem in two ways:

- Natively in the product
- [Tools](https://github.com/kagent-dev/tools) that are available, but optional

Native integration:

- **Kubernetes**: Native integration with Kubernetes APIs, RBAC, and resource management
- **Helm**: Deployment and management through Helm charts
- **OpenTelemetry**: Distributed tracing and observability
- **LLM Providers**: Secure integration with major AI model providers (OpenAI, Azure OpenAI, Anthropic, Google Vertex AI, Ollama, and custom models)
- **MCP Ecosystem**: Extensible tool system through Model Context Protocol
- **Prometheus**: Expose prometheus metrics for observability

Optional tooling:

- **kgateway**: Gateway and Kubernetes Gateway API integration
- **Grafana**: Observability and monitoring integration
- **Istio**: Integration with Istio Service Mesh APIs
- **Argo**: Integration with Argo Rollouts
- **Cilium**: Integration through specialized agents for eBPF-based networking


## Security Issue Resolution

### Responsible Disclosure Process

- **Reporting**: See [SECURITY.md](/SECURITY.md) for details about reporting practices.

- **Response Process**: The kagent team evaluates vulnerability reports for severity level, impact on kagent code, and potential dependencies on third-party code. The team strives to keep vulnerability information private on a need-to-know basis during the remediation process.

- **Communication**: Reporters receive acknowledgment of their report and are kept informed of the remediation progress. Public disclosure is coordinated with the reporter after fixes are available.

### Incident Response

- **Triage**: Security incidents are triaged based on severity, impact, and exploitability. Critical issues receive immediate attention with dedicated resources.

- **Confirmation**: The team works to reproduce and confirm reported vulnerabilities, assessing their impact on different deployment scenarios.

- **Notification**: Stakeholders are notified through appropriate channels based on the severity of the issue. This may include security advisories, GitHub security alerts, and community notifications.

- **Patching**: Security fixes are prioritized and released through regular release channels or emergency patches for critical issues. Patches are thoroughly tested before release.

## Appendix

### Known Issues Over Time

As of the time of this assessment, no critical security vulnerabilities have been publicly reported or discovered in kagent. The project maintains a clean security record with proactive vulnerability scanning and security practices in place.

### Open SSF Best Practices

kagent is working toward OpenSSF Best Practices certification. That work is tracked by [https://github.com/kagent-dev/community/issues/9](https://github.com/kagent-dev/community/issues/9).

The badge is visible in the [project README](/README.md)

### Case Studies

1. **Amdocs: Telecommunications Infrastructure Automation**
Amdocs, a leading provider of software and services to communications and media companies, has deployed kagent to manage their complex telecommunications infrastructure running on Kubernetes. Their implementation focuses on a mechanism to detect malicious users. kagent provides an environment to coordinate custom MCP servers that help with troubleshooting issues in the platform.

2. **Au10tix: Identity Verification Platform Security**
Au10tix, specializing in identity verification and fraud prevention, leverages kagent to enhance the security and reliability of their Kubernetes-based identity verification platform.

3. **Krateo: Cloud-Native Platform Engineering**
Krateo, focused on cloud-native platform solutions, uses kagent to automate and secure their internal development platform and customer deployments. kagent integrates with the teams they already have, and the AI reasoning is transparent, to make debugging issues easier.

### Related Projects / Vendors

- **Kubernetes Operators**: While Kubernetes operators provide automation, kagent adds AI-powered decision making and natural language interfaces.
- **AI/ML Platforms**: kagent focuses specifically on operational AI agents rather than model training or general ML workloads.