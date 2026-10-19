---
id: adk-run-bash-tool-commands-on-an-allowlisted
fork: kagent
theme: harness-variety
category: bugfix
status: carried
commits:
  - 'fix(adk): run bash tool commands on an allowlisted environment'
upstream:
  outcome: open
  refs: [kagent-dev/kagent#3002, kagent-dev/kagent#3014]
  exit: '#3014 or ours merges'
depends_on: []
verified: 2026-10-19
---
# fix(adk): run bash tool commands on an allowlisted environment

## Change

`fix(adk): run bash tool commands on an allowlisted environment`

## Reason

the Go ADK's bash tool never set `cmd.Env`, so every command a model wrote inherited the runtime's whole environment — the model provider keys the controller injects and every `KAGENT_*` value, `KAGENT_CONFIG_JSON` among them — and its output went back to the model (giantswarm/kagent-upstream#301). The command now runs on an allowlist: `PATH`, `HOME`, `USER`, `LOGNAME`, `LANG`, `LC_*`, `TERM`, `TZ`, `TMPDIR`, the CA-bundle variables a command needs behind the egress gateway, and `PWD` set to the working directory; proxy variables stay out because their URLs may carry credentials

## History

kagent-dev/kagent#3002; kagent-dev/kagent#3014 (open) strips secret-looking names instead, which passes every `KAGENT_*` value; offered as the stricter alternative
