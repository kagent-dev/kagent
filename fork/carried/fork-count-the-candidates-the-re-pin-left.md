---
id: fork-count-the-candidates-the-re-pin-left
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): count the candidates the re-pin left behind when numbering the next one'
upstream:
  outcome: fork-only
  refs: [giantswarm/devctl#2498]
  exit: devctl#2498
depends_on: []
verified: 2026-10-19
---
# ci(fork): count the candidates the re-pin left behind when numbering the next one

## Change

`ci(fork): count the candidates the re-pin left behind when numbering the next one`

## Reason

the generated auto-release numbers the next `-rc.N` by counting the candidates of the target reachable from the branch; after a re-pin none of the earlier ones is reachable, so the push after the 2026-10-05 re-pin would have cut `v1.3.0-rc.1`, a tag that exists, and failed as giantswarm/substrate's did (its `v1.4.0-rc.8` was tagged by hand). The baseline step exports `LINE_REPINNED=true` when it moved the line's stable tag, and the counter then takes every tag of the target

## History

the same change as giantswarm/devctl#2498 on the template; the align-files bot regenerates the file with it once devctl releases, and this commit falls away
