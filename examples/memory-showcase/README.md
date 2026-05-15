# Memory Showcase

This example demonstrates kagent long-term memory with two interactions:

1. The agent stores a durable fact with the `save_memory` tool.
2. A later interaction asks for that fact from a new A2A context, and the agent retrieves it with `load_memory`.

The demo fact is:

```text
In the memory showcase, my release codename is blue-sunrise.
```

## How It Works

The manifest creates two `ModelConfig` resources and one declarative `Agent`:

- `memory-showcase-chat` uses `gpt-4.1-mini` for normal chat.
- `memory-showcase-embedding` uses `text-embedding-3-small` for memory embeddings.
- `memory-showcase-agent` enables memory with:

```yaml
memory:
  modelConfig: memory-showcase-embedding
  ttlDays: 15
```

When memory is configured, kagent adds memory tools to the ADK agent. The demo prompt tells the agent to use `save_memory` when the user asks it to remember a fact, and to call `load_memory` before answering questions about the saved release codename.

## Persistence Model

kagent stores long-term memory through the kagent API in the backend `memory` table, backed by pgvector search. Each memory row includes the agent name, user ID, text content, embedding vector, metadata, creation time, expiration time, and access count.

Memory search is scoped by agent name and user ID. This demo sends a stable `X-User-ID` header so the second interaction can retrieve memory saved by the first interaction, even though the script uses a separate A2A context ID for the second turn.

`ttlDays` controls how long a memory remains valid before it is eligible for pruning. This example sets `ttlDays: 15`; omitting it or setting it to zero uses the server default of 15 days. Expired memories with low access counts are deleted by pruning. Expired memories with an access count of at least 10 are considered popular, have their TTL extended by 15 days, and have their access count reset.

## Run The Demo

Prerequisites:

- A running kagent stack.
- The `kagent-openai` secret in the `kagent` namespace with `OPENAI_API_KEY`.
- Local `kubectl`, `curl`, and `jq`.
- Access to the kagent controller API. If running locally, port-forward it:

```bash
kubectl port-forward -n kagent deploy/kagent-controller 8083:8083
```

Apply the demo resources:

```bash
kubectl apply -f examples/memory-showcase/memory-showcase.yaml
kubectl wait --for=condition=Ready agent/memory-showcase-agent -n kagent --timeout=2m
```

Run the scripted interaction:

```bash
KAGENT_URL=http://localhost:8083 ./examples/memory-showcase/run-demo.sh
```

The output shows:

- Memory before turn 1, usually empty for `memory-showcase-user@example.com`.
- Turn 1 asking the agent to remember `blue-sunrise`.
- Memory after turn 1, showing the stored fact.
- Turn 2 asking from a separate A2A context and receiving `blue-sunrise` from memory.

By default the script deletes memories for `memory-showcase-agent` and the demo `USER_ID` before it starts, then exits nonzero if the saved memory or second response does not include `blue-sunrise`. Set `RESET_MEMORY=false` to keep existing memories for that agent/user.

For trusted-proxy auth mode, provide a valid bearer token and set `USER_ID` to the token subject:

```bash
AUTH_HEADER="Bearer ${TOKEN}" USER_ID="you@example.com" ./examples/memory-showcase/run-demo.sh
```

Clean up:

```bash
kubectl delete -f examples/memory-showcase/memory-showcase.yaml
```
