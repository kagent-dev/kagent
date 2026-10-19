---
id: ui-reread-settling-agents
fork: kagent
theme: test-flakes
category: bugfix
status: carried
commits:
  - 'fix(ui): re-read Agents while the controller is still preparing them'
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
verified: 2026-10-19
---
# fix(ui): re-read Agents while the controller is still preparing them

## Change

`fix(ui): re-read Agents while the controller is still preparing them`

## Reason

an Agent whose new revision waits on its ActorTemplate golden snapshot reads as Updating or Preparing, a state the controller changes on its own; the UI read Agents once, so a Dashboard or Agents page opened during a rollout kept "Updating" and "0 ready" after every Agent was Ready, until a reload (giantswarm/kagent-upstream#546). The Agent list and detail hooks re-read every 5 s while an Agent on screen is settling and stop once all have settled

## History

to file — the hooks are upstream main's (`useAgents.ts` unchanged)
