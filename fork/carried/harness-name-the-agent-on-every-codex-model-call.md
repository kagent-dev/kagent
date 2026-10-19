---
id: harness-name-the-agent-on-every-codex-model-call
fork: kagent
theme: identity-authorizer
category: feature
status: carried
commits:
  - 'feat(harness): name the agent on every codex model call'
upstream:
  outcome: to-file
  refs: []
  exit: file with identity headers
depends_on: [runtime-name-the-agent-and-the-user]
verified: 2026-10-19
---
# feat(harness): name the agent on every codex model call

## Change

`feat(harness): name the agent on every codex model call`

## Reason

a codex agent's model calls carried no agent identity, so the platform's `llm` listener, which labels usage and keys rate limits by `x-kagent-agent` and `x-kagent-agent-namespace`, read every codex call as an unknown agent (giantswarm/kagent-upstream#613). The codex compiler injects and owns `KAGENT_AGENT_TEMPLATE` like the kagent compiler, and the codex adapter names the agent on the OpenAI provider through `env_http_headers`, on Amazon Bedrock through literal `http_headers` (the pinned Codex 0.148.0 refuses `env_http_headers` on its built-in Bedrock provider)

## History

to file — prepared in the fork: branch `upstream/codex-model-call-identity-headers`, on top of `upstream/model-call-identity-headers` (the identity it extends, `runtime-name-the-agent-and-the-user.md`), so it is filed with or after that one
