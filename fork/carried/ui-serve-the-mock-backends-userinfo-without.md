---
id: ui-serve-the-mock-backends-userinfo-without
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'fix(ui): serve the mock backend''s userinfo without a service worker'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3134, kagent-dev/kagent#3163]
  exit: 're-check against #3163'
depends_on: []
verified: 2026-10-19
---
# fix(ui): serve the mock backend's userinfo without a service worker

## Change

`fix(ui): serve the mock backend's userinfo without a service worker`

## Reason

the fork's `ui-tests` retried Firefox specs in about a quarter of runs (giantswarm/kagent-upstream#508): a `page.goto` or `page.reload` drew the next page and never fired its `load`. The mock service worker sat in front of every request the page made and answered each only after asking the page; a page navigating away never answers, so a request in flight at the navigation never settled (the failed attempts' traces show module requests through the worker at that moment). It existed only for `/oauth2/userinfo`, now answered through the oauth2-proxy auth source's fetch seam; mock mode registers no worker and msw is gone. Twenty full suite runs without it: no hung navigation, no agent-rail retry

## History

to file — prepared in the fork: branch `upstream/ui-mock-userinfo-without-worker` (mirror `main` @ `7433d70b` + the patch, DCO-signed, no fork references; typecheck, lint and unit tests green there). kagent-dev/kagent#3163 (merged 2026-10-13, for kagent-dev/kagent#3134) serves the mock suite from a production build, which cuts the module requests through the worker but keeps it
