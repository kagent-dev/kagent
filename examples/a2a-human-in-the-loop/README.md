# A2A Human-in-the-Loop With Dynamic Skill Loading

This demo creates two declarative kagent agents:

- `a2a-hitl-coordinator` receives the user request and delegates to the specialist by using kagent's Agent tool, which communicates over A2A.
- `a2a-hitl-specialist` pauses at a human approval checkpoint with the built-in `ask_user` tool, then loads and runs the `kebab-maker` skill from `https://github.com/kagent-dev/kagent.git` at runtime through `spec.skills.gitRefs`.

## Prerequisites

- A Kubernetes cluster with kagent installed and the `kagent.dev/v1alpha2` CRDs available.
- An OpenAI API key for the demo `ModelConfig`.
- The kagent UI or another A2A client that can send messages to `a2a-hitl-coordinator` and answer HITL prompts.

## Deploy

Create the namespace and model API key secret:

```sh
kubectl create namespace kagent --dry-run=client -o yaml | kubectl apply -f -
kubectl -n kagent create secret generic a2a-hitl-openai \
  --from-literal=api-key="$OPENAI_API_KEY" \
  --dry-run=client -o yaml | kubectl apply -f -
```

Apply the demo:

```sh
kubectl apply -k examples/a2a-human-in-the-loop
kubectl -n kagent wait --for=condition=Accepted agent/a2a-hitl-specialist --timeout=120s
kubectl -n kagent wait --for=condition=Accepted agent/a2a-hitl-coordinator --timeout=120s
```

The specialist pod has a skills init container. At startup, kagent clones the remote repo URL and copies `go/core/test/e2e/testdata/skills/kebab-maker` into `/skills/kebab-maker`.

## Run

Open the kagent UI, start a chat with `a2a-hitl-coordinator`, and send:

```text
Run the A2A human-in-the-loop skill demo.
```

Expected flow:

1. The coordinator calls `a2a-hitl-specialist` as an Agent tool. This is the A2A handoff.
2. The specialist calls `ask_user` with the approval question:
   `Approve loading and running the git-loaded kebab-maker skill for the A2A HITL demo?`
3. Choose `Approve` in the UI.
4. The specialist calls `skills(command="kebab-maker")`, then runs the skill script with `bash`.
5. The coordinator relays the specialist's result back to the user.

If you reject the prompt, the specialist stops and reports that the checkpoint was rejected.

## Clean Up

```sh
kubectl delete -k examples/a2a-human-in-the-loop
kubectl -n kagent delete secret a2a-hitl-openai
```
