# General Technical Review - kagent / Incubation

_This document provides a General Technical Review of the kagent project. This is a living document that demonstrates to the Technical Advisory Group (TAG) that the project satisfies the Engineering Principle requirements for moving levels. This document follows the template outlined [in the TOC subproject review](https://github.com/cncf/toc/blob/main/toc_subprojects/project-reviews-subproject/general-technical-questions.md)_


- **Project:**
- **Project Version:**: 
- **Website:**
- **Date Updated:** YYYY-MM-DD
- **Template Version:** v1.0
- **Description:** <!-- Short project description --> 


## Day 0 - Planning Phase

### Scope

  * Describe the roadmap process, how scope is determined for mid to long term features, as well as how the roadmap maps back to current contributions and maintainer ladder?
  * Describe the target persona or user(s) for the project?
  * Explain the primary use case for the project. What additional use cases are supported by the project?
  * Explain which use cases have been identified as unsupported by the project.  
  * Describe the intended types of organizations who would benefit from adopting this project. (i.e. financial services, any software manufacturer, organizations providing platform engineering services)?  
  * Please describe any completed end user research and link to any reports.

### Usability

  * How should the target personas interact with your project?  
  * Describe the user experience (UX) and user interface (UI) of the project.  
  * Describe how this project integrates with other projects in a production environment.

### Design

  * Explain the design principles and best practices the project is following.   
  * Outline or link to the project’s architecture requirements? Describe how they differ for Proof of Concept, Development, Test and Production environments, as applicable.  
  * Define any specific service dependencies the project relies on in the cluster.  
  * Describe how the project implements Identity and Access Management.  
  * Describe how the project has addressed sovereignty.  
  * Describe any compliance requirements addressed by the project.  
  * Describe the project’s High Availability requirements.  
  * Describe the project’s resource requirements, including CPU, Network and Memory.  
  * Describe the project’s storage requirements, including its use of ephemeral and/or persistent storage.  
  * Please outline the project’s API Design:  
    * Describe the project’s API topology and conventions  
    * Describe the project defaults  
    * Outline any additional configurations from default to make reasonable use of the project  
    * Describe any new or changed API types and calls \- including to cloud providers \- that will result from this project being enabled and used  
    * Describe compatibility of any new or changed APIs with API servers, including the Kubernetes API server   
    * Describe versioning of any new or changed APIs, including how breaking changes are handled  
  * Describe the project’s release processes, including major, minor and patch releases.

### Installation

  * Describe how the project is installed and initialized, e.g. a minimal install with a few lines of code or does it require more complex integration and configuration?  
  * How does an adopter test and validate the installation?

### Security

  * Please provide a link to the project’s cloud native [security self assessment](https://tag-security.cncf.io/community/assessments/).  
  * Please review the [Cloud Native Security Tenets](https://github.com/cncf/tag-security/blob/main/community/resources/security-whitepaper/secure-defaults-cloud-native-8.md) from TAG Security.  
    * How are you satisfying the tenets of cloud native security projects?  
    * Describe how each of the cloud native principles apply to your project.  
    * How do you recommend users alter security defaults in order to "loosen" the security of the project? Please link to any documentation the project has written concerning these use cases.  
  * Security Hygiene  
    * Please describe the frameworks, practices and procedures the project uses to maintain the basic health and security of the project.   
    * Describe how the project has evaluated which features will be a security risk to users if they are not maintained by the project?  
  * Cloud Native Threat Modeling  
    * Explain the least minimal privileges required by the project and reasons for additional privileges.  
    * Describe how the project is handling certificate rotation and mitigates any issues with certificates.  
    * Describe how the project is following and implementing [secure software supply chain best practices](https://project.linuxfoundation.org/hubfs/CNCF\_SSCP\_v1.pdf) 

## Day 1 \- Installation and Deployment Phase

### Project Installation and Configuration

  * Describe what project installation and configuration look like.

### Project Enablement and Rollback

  * How can this project be enabled or disabled in a live cluster? Please describe any downtime required of the control plane or nodes.  
  * Describe how enabling the project changes any default behavior of the cluster or running workloads.  
  * Describe how the project tests enablement and disablement.  
  * How does the project clean up any resources created, including CRDs?

### Rollout, Upgrade and Rollback Planning

  * How does the project intend to provide and maintain compatibility with infrastructure and orchestration management tools like Kubernetes and with what frequency?  
  * Describe how the project handles rollback procedures.  
  * How can a rollout or rollback fail? Describe any impact to already running workloads.  
  * Describe any specific metrics that should inform a rollback.  
  * Explain how upgrades and rollbacks were tested and how the upgrade-\>downgrade-\>upgrade path was tested.  
  * Explain how the project informs users of deprecations and removals of features and APIs.  
  * Explain how the project permits utilization of alpha and beta capabilities as part of a rollout.

## Day 2 \- Day-to-Day Operations Phase

### Scalability/Reliability

  * Describe how the project increases the size or count of existing API objects.
  * Describe how the project defines Service Level Objectives (SLOs) and Service Level Indicators (SLIs).  
  * Describe any operations that will increase in time covered by existing SLIs/SLOs.  
  * Describe the increase in resource usage in any components as a result of enabling this project, to include CPU, Memory, Storage, Throughput.  
  * Describe which conditions enabling / using this project would result in resource exhaustion of some node resources (PIDs, sockets, inodes, etc.)  
  * Describe the load testing that has been performed on the project and the results.  
  * Describe the recommended limits of users, requests, system resources, etc. and how they were obtained.  
  * Describe which resilience pattern the project uses and how, including the circuit breaker pattern.

### Observability Requirements

  * Describe the signals the project is using or producing, including logs, metrics, profiles and traces. Please include supported formats, recommended configurations and data storage.  
  * Describe how the project captures audit logging.  
  * Describe any dashboards the project uses or implements as well as any dashboard requirements.  
  * Describe how the project surfaces project resource requirements for adopters to monitor cloud and infrastructure costs, e.g. FinOps  
  * Which parameters is the project covering to ensure the health of the application/service and its workloads?  
  * How can an operator determine if the project is in use by workloads?  
  * How can someone using this project know that it is working for their instance?  
  * Describe the SLOs (Service Level Objectives) for this project.
  * What are the SLIs (Service Level Indicators) an operator can use to determine the health of the service?

### Dependencies

  * Describe the specific running services the project depends on in the cluster.  
  * Describe the project’s dependency lifecycle policy.  
  * How does the project incorporate and consider source composition analysis as part of its development and security hygiene? Describe how this source composition analysis (SCA) is tracked.
  * Describe how the project implements changes based on source composition analysis (SCA) and the timescale.

### Troubleshooting

  * How does this project recover if a key component or feature becomes unavailable? e.g Kubernetes API server, etcd, database, leader node, etc.  
  * Describe the known failure modes.

### Compliance

  * What steps does the project take to ensure that all third-party code and components have correct and complete attribution and license notices?
  * Describe how the project ensures alignment with CNCF [recommendations](https://github.com/cncf/foundation/blob/main/policies-guidance/recommendations-for-attribution.md) for attribution notices.
    <!--Note that each question describes a use case covered by the referenced policy document.-->
    * How are notices managed for third-party code incorporated directly into the project's source files?
    * How are notices retained for unmodified third-party components included within the project's repository?
    * How are notices for all dependencies obtained at build time included in the project's distributed build artifacts (e.g. compiled binaries, container images)?

### Security

  * Security Hygiene  
    * How is the project executing access control?  
  * Cloud Native Threat Modeling  
    * How does the project ensure its security reporting and response team is representative of its community diversity (organizational and individual)?  
    * How does the project invite and rotate security reporting team members?
 