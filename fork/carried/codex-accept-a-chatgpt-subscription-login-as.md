---
id: codex-accept-a-chatgpt-subscription-login-as
fork: kagent
theme: providers-vertex
category: feature
status: carried
commits:
  - 'feat(codex): accept a ChatGPT subscription login as the OpenAI credential'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3258]
  exit: file row 201 or move the refresher out of the controller
depends_on: [codex-refresh-a-chatgpt-login-only-in]
verified: 2026-10-19
---
# feat(codex): accept a ChatGPT subscription login as the OpenAI credential

## Change

`feat(codex): accept a ChatGPT subscription login as the OpenAI credential`

## Reason

a Codex agent could only run on an OpenAI API key: the Codex compiler always set `OPENAI_API_KEY` against the OpenAI API, and a ChatGPT login (a short-lived access token, a rotating refresh token and an account id, used against the ChatGPT Codex backend) had no way in, nor anything to refresh it. A ModelConfig whose `openAI.credentialType` is `chatgptOAuth` now points a custom Codex provider at the ChatGPT Codex backend, the gateway binds the access token as `authorization: Bearer` and the account id as `chatgpt-account-id`, and the controller is the login's one refresher, writing the rotated tokens back into the Secret; the ADK runtimes reject the mode

## History

to file — prepared in the fork: branch `upstream/codex-chatgpt-login-4` (with the opt-in label of the commit after it)
