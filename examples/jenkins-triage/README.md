# Jenkins triage agent (read-only, via `cictl`)

This example shows how to wire an external CI inspection CLI into a kagent
Agent for read-only Jenkins triage. The agent diagnoses failed builds by
fetching console logs, build metadata, job configuration, and Kubernetes
cloud configuration — **never mutating Jenkins state**.

It is a worked example of the "external CLI + skill markdown + custom runtime
image" pattern, complementary to kagent's MCP-server-based agents.

## What you get

When a user describes a failed Jenkins build, the agent autonomously runs:

```
cictl jenkins build get <job> <num>
cictl jenkins console <job> <num> --tail 200
cictl jenkins job config <job>           # if pipeline-shaped failure suspected
cictl jenkins cloud list                 # if agent/executor failure suspected
```

…and explains the failure. By construction, no build can be retriggered or
modified — `cictl` ships only read-only verbs.

## Architecture

```
  user prompt ──▶ Agent (declarative, Go runtime)
                    │
                    │  skill loaded from gitRefs:
                    │    https://github.com/Feelings0220/cictl @ v0.1.0
                    │    path: skills
                    │
                    └─▶ runs cictl jenkins … via BashTool
                            │
                            ▼  reads
                        /home/agent/.config/cictl/credentials.yaml
                        (mounted from Secret jenkins-cictl-credentials)
                            │
                            ▼  HTTPS GET
                        Jenkins REST API (read-only)
```

The `cictl` binary itself lives in the Agent runtime image. See
[runtime setup](https://github.com/Feelings0220/cictl/blob/v0.1.0/docs/runtime-setup.md)
for two ways to get it there — the recommended path is a small custom runtime
image built from kagent's `app` image with `cictl` COPY'd in.

## Prerequisites

1. **kagent installed** with `app.agentImage` either pointing at a custom image
   that has `cictl` in `PATH`, or otherwise made available to the Agent's main
   container. Build instructions:
   [cictl/docs/runtime-setup.md](https://github.com/Feelings0220/cictl/blob/v0.1.0/docs/runtime-setup.md).
2. **A Jenkins API token** for a read-only user. Generate at
   *People → \<your user\> → Configure → API Token*.
3. **A working `ModelConfig`** in the `kagent` namespace (the default
   `default-model-config` works).

## Step 1 — Create the credentials Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: jenkins-cictl-credentials
  namespace: kagent
type: Opaque
stringData:
  credentials.yaml: |
    default-context: prod
    contexts:
      prod:
        url: https://jenkins.example.com
        username: alice
        # Get this from Jenkins → People → alice → Configure → API Token
        token: REPLACE_WITH_JENKINS_API_TOKEN
```

```bash
kubectl apply -f - <<'EOF'
# … paste the above YAML …
EOF
```

## Step 2 — Apply the Agent CRD

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: jenkins-triage
  namespace: kagent
spec:
  type: Declarative
  description: |
    Diagnoses Jenkins build failures read-only. Fetches console logs, build
    metadata, job config, and cloud config. Cannot trigger, retry, or abort
    builds — `cictl` is read-only by construction.
  skills:
    gitRefs:
      - url: https://github.com/Feelings0220/cictl
        ref: v0.1.0
        path: skills
  declarative:
    runtime: go
    modelConfig: default-model-config
    systemMessage: |
      You are a Jenkins triage agent.

      You have access to the `cictl` CLI for querying Jenkins read-only. The
      `jenkins` skill in /skills describes every available command and a
      triage playbook. Follow it.

      Hard rules:
      - You MUST NOT construct curl commands or any other mechanism to mutate
        Jenkins state. `cictl` has no mutating subcommands; if the user asks
        you to retry/abort/replay, direct them to the Jenkins UI.
      - Never print or echo the credentials file.
      - Console logs can be huge — default to `--tail 200` and only escalate
        to `--full` after locating the failure region.
    deployment:
      env:
        - name: HOME
          value: /home/agent
      volumes:
        - name: cictl-credentials
          secret:
            secretName: jenkins-cictl-credentials
            items:
              - key: credentials.yaml
                path: credentials.yaml
      volumeMounts:
        - name: cictl-credentials
          mountPath: /home/agent/.config/cictl
          readOnly: true
```

## Step 3 — Try it

Port-forward the kagent UI and find the `jenkins-triage` agent:

```bash
kubectl port-forward -n kagent svc/kagent-ui 3000:8080
```

Open `http://localhost:3000`, pick `jenkins-triage`, and try a prompt:

> Build #42 of `team/checkout-service/main` failed. What happened?

The agent should run `cictl jenkins build get` and `cictl jenkins console`
to fetch context, then explain the failure.

## Verify it is actually read-only

Tail Jenkins' access log while the agent runs a triage session:

```bash
kubectl -n jenkins logs -f deploy/jenkins | grep -vE 'GET '
```

If anything other than `GET` requests appears, file an issue on `cictl`.

## See also

- [cictl on GitHub](https://github.com/Feelings0220/cictl) — the CLI's source,
  release binaries, and skill markdown.
- [cictl/docs/runtime-setup.md](https://github.com/Feelings0220/cictl/blob/v0.1.0/docs/runtime-setup.md)
  — how to add `cictl` to the kagent runtime image.
- [jenkins-shared-library-guide.md](https://github.com/Feelings0220/cictl/blob/v0.1.0/docs/jenkins-shared-library-guide.md)
  *(if shipped in repo)* — pairing this agent with a Jenkins Shared Library so
  pipelines auto-trigger triage on `post { failure { kagentAnalyze() } }`.

## Why not an MCP server?

kagent's built-in agents (k8s, istio, observability, …) use MCP servers as
tool providers. `cictl` is intentionally a CLI + skill instead because:

- It is usable by **any** agent framework that can shell out (Claude Code,
  Cursor, Continue, kagent BYO agents, plain humans). Not kagent-specific.
- Compile-time read-only guarantee: the binary has no `POST`/`PUT`/`DELETE`
  helpers at all. Hard to misuse.
- A future MCP wrapper around `cictl` is plausible — see `cictl`'s roadmap.

This example fills a gap in kagent's `examples/` directory: how to integrate
an external CLI tool without first building an MCP server.
