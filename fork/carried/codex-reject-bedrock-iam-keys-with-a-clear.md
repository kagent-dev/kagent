---
id: codex-reject-bedrock-iam-keys-with-a-clear
fork: kagent
theme: providers-vertex
category: bugfix
status: carried
commits:
  - 'fix(codex): reject Bedrock IAM keys with a clear error'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3258]
  exit: file the branch
depends_on: []
verified: 2026-10-19
---
# fix(codex): reject Bedrock IAM keys with a clear error

## Change

`fix(codex): reject Bedrock IAM keys with a clear error`

## Reason

The Codex harness README says AWS IAM keys are rejected for Bedrock because they cannot be injected as outbound HTTP headers, but the compiler accepted a Secret with `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` and only failed later in the generic credential pass, with an error that names neither Bedrock nor IAM keys (giantswarm/kagent-upstream#498). The compiler now requires `AWS_BEARER_TOKEN_BEDROCK` and says so

## History

to file — prepared in the fork: branch `upstream/codex-bedrock-reject-iam-keys`
