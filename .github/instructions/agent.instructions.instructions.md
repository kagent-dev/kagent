---
applyTo: 'helm/agents/**/agent.yaml'
---

Every agent definition done in yaml format and 
should follow the following structure:

```yaml
apiVersion: kagent.dev/v1alpha1
kind: Agent
metadata:
  name: agent-name
  namespace: {{ include "kagent.namespace" . }}
  labels:
    {{- include "kagent.labels" . | nindent 4 }}
spec:
  description: Expert AI Agent specializing in <inser your agent's specialization>
  systemMessage: |
    # <Agent Name>

    ## Core Capabilities
    ## Operational Guidelines
    ### Investigation Protocol
    ### Problem-Solving Framework
    ## Available Tools
    ### Informational Tools
    ### Modification Tools
    
    ## Safety Protocols
    ## Response Format
    ## Limitations
    Always start with the least intrusive approach, and escalate diagnostics only as needed. 
    When in doubt, gather more information before recommending changes.
  modelConfig: {{ .Values.modelConfigRef | default (printf "%s" (include "kagent.defaultModelConfigName" .)) }}
  tools:
    - type: Builtin
      builtin:
        name: kagent.tools.k8s.CheckServiceConnectivity
      builtin:
        name: kagent.tools.docs.QueryTool
        config:
          docs_download_url: https://doc-sqlite-db.s3.sa-east-1.amazonaws.com
  a2aConfig:
  a2aConfig:
    skills:
      - id: skill-id
        name: skill-name
        description: skill description
          - tag1
          - tag2
        examples:
          - "example prompt"
      - id: service-mesh-diagnostics
