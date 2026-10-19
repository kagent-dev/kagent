---
id: e2e-run-each-send-under-the-callers-context
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'chore(e2e): run each send under the caller''s context'
upstream:
  outcome: to-file
  refs: []
  exit: file in the test batch
depends_on: []
verified: 2026-10-19
---
# chore(e2e): run each send under the caller's context

## Change

`chore(e2e): run each send under the caller's context`

## Reason

`TestSessionInteraction/byo-adk` flaked in the gVisor lane (giantswarm/kagent-upstream#276): `sendMessageWithRetry` ran every `SendMessage` under the context of its 30 s retry poll, so a blocking send whose verdict took longer on a loaded lane failed `DeadlineExceeded` before the turn ended; `sendHTTPStreamingMessageWithRetry` cut a JSON-RPC stream the same way. Each send now runs under the caller's context; the 30 s window bounds only the resends of the gateway's proven refusal (`KAGENT_SEND_NOT_ACCEPTED`), and a refusal still standing at its end is returned. The assertions are unchanged

## History

to file; upstream main has the same helpers
