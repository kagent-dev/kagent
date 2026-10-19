---
id: models-stream-the-anthropic-call-behind-a-non
fork: kagent
theme: providers-vertex
category: bugfix
status: carried
commits:
  - 'fix(models): stream the Anthropic call behind a non-streaming request'
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
verified: 2026-10-19
---
# fix(models): stream the Anthropic call behind a non-streaming request

## Change

`fix(models): stream the Anthropic call behind a non-streaming request`

## Reason

the Go runtime answered a non-streaming `GenerateContent` with the Anthropic SDK's `Messages.New`, which refuses a request before sending it when its `max_tokens` could take longer than ten minutes to generate (more than 21,333 tokens, or a model's own non-streaming cap), and the request carries the ModelConfig's `maxTokens`, sized for the agent's streamed replies. An installation's default ModelConfig caps output at 32000 because an adaptive-thinking model shares `max_tokens` with the visible answer, so the context compaction summarizer — the ADK's LLM summarizer asks for a non-streaming call — failed at every crossing (`tail-retention summarization failed: summarizer model call failed: anthropic API error: streaming is required for operations that may take longer than 10 minutes`) and every conversation ran uncompacted. The non-streaming path streams the call and accumulates the message with the SDK's `Message.Accumulate`; a caller still gets one final response

## History

to file — prepared branch `upstream/anthropic-nonstreaming-stream`; no upstream issue or pull request (kagent-dev/kagent searched 2026-09-24)
