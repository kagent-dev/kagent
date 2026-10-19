---
id: codex-refresh-a-chatgpt-login-only-in
fork: kagent
theme: providers-vertex
category: bugfix
status: carried
commits:
  - 'fix(codex): refresh a ChatGPT login only in a Secret labelled for it'
upstream:
  outcome: to-file
  refs: []
  exit: with row 201
depends_on: [codex-accept-a-chatgpt-subscription-login-as]
verified: 2026-10-19
---
# fix(codex): refresh a ChatGPT login only in a Secret labelled for it

## Change

`fix(codex): refresh a ChatGPT login only in a Secret labelled for it`

## Reason

the ChatGPT login refresher wrote any Secret in its namespace a `chatgptOAuth` ModelConfig named, so a ModelConfig author could point it at a Secret it was not meant to rewrite. A login's Secret now opts in with the label `kagent.dev/chatgpt-login: "true"`; a ModelConfig naming any other Secret reports `ResolvedRefs` false (`ChatGPTLoginNotOptedIn`) and the Secret is never written. The refresher writes as a ServiceAccount of its own the controller impersonates, granted `update` on Secrets only and admitted by a ValidatingAdmissionPolicy only for a Secret labelled before and after the write

## History

to file — prepared in the fork: branch `upstream/codex-chatgpt-login-4`, folded into the ChatGPT login commit it hardens
