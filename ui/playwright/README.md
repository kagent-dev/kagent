# Playwright end-to-end tests

Browser tests for the kagent UI. This suite is the project's acceptance bar: the
rewrite is done when the same general set of journeys still passes.

## Running

```bash
cd ui
yarn test:pw                 # or: yarn test:e2e
UI_LOOP_PORT=8012 yarn test:pw   # when something else owns the default port
```

Nothing else is needed — no cluster, no port-forward, no provider key.

There is a second suite that does need all three; see
[Live runs](#live-runs-against-a-real-backend) at the foot of this file.

## What changed from the old suite

The old suite ran against a real kagent backend in a kind cluster. Running it
meant building images, `make create-kind-cluster` and `make helm-install`,
exporting a provider key, and a port-forward held open for the duration, with a
Node proxy in front to forward `/api/**` to the controller and mock the chat SSE
stream. Roughly: **several minutes of setup, a cluster, and a provider key.**

This suite needs none of that — `yarn test:pw`, about eight seconds, on any
machine that can run the dev server. `playwright/setup.ts`, `teardown.ts`,
`mocks/server.mjs` and `scripts/setup.sh` are gone with the apparatus they
served.

Worth stating plainly because it is a change in how contributors work, and
because it is a trade: the suite no longer exercises the real controller, so it
proves the UI behaves, not that the backend contract still holds. Contract drift
is caught by the Go tests and by whatever runs against a live cluster in CI — not
here.

## What it runs against

The suite runs against the in-browser mock backend (`src/mocks/`), pinned by the
`webServer` block in `playwright.config.ts` so an inherited `VITE_API_MODE=live`
cannot silently point a run at a real cluster. That buys three things the old
kind-cluster setup could not offer: the data is fixed, so a spec can assert exact
rows; the run takes seconds; and failure is a first-class state rather than
something you have to break a cluster to see.

**Scenarios.** The mock backend reads how it should behave from the query string
on every request, so a spec drives the awkward states by navigating:

| | |
|---|---|
| `?mock=ok` | normal data (the default) |
| `?mock=empty` | every list comes back empty |
| `?mock=error` | every request fails with a 500 |
| `?mock=slow` | a long delay, so the loading state is observable |

The scenario is remembered for the browsing session, so **always pass one
explicitly** — `withScenario()` in `helpers/app.ts` does this, and `loadPage()`
defaults to `ok`. A bare path inherits whatever the previous step asked for,
which is convenient in a browser and a trap in a test.

## Layout

```
playwright/
  tests/           app-shell, routing, and <area>/<area>{,-errors}.spec.ts
  helpers/         app (navigation, tables, scenarios), nav (shell chrome),
                   extensions (vendor slots)
  fixtures/test.ts import { test, expect } from here — never @playwright/test
  live/            the live suite: specs, plus helpers/ of its own
  DEFERRED.md      the specs not yet portable, and what each one is waiting on
```

## Vendor extensions: two servers, two projects

Which extension a build ships with is decided at build time, so "installed" and
"not installed" cannot be two states of one server. The config boots two:

| Project | Server | Specs |
|---|---|---|
| `chromium` | bare — no extension, on `UI_LOOP_PORT` | everything not matching `*.vendor.spec.ts` |
| `chromium-vendor` | `VITE_VENDOR_EXTENSIONS=example`, on `UI_LOOP_PORT + 50` | `*.vendor.spec.ts` |

A spec opts into the extension-installed app by being named `*.vendor.spec.ts`.

The gap of 50 between the ports is deliberate. Vite falls forward to the next
free port when the one it is told to use is busy, so with adjacent ports a
slow-to-die server from a previous run can push one app onto the other's port —
which surfaces as a spec mysteriously unable to find the contribution it is
asserting on. `globalSetup` also checks that each port is serving the build its
project expects, and fails the run immediately with that explanation if not, so
a harness problem cannot be mistaken for a product one.

**Assert the mechanism, never the example.** The bundled Example extension is
documentation that happens to run, and it is expected to change. Specs go
through the `vendor-slot-<id>` test id that `VendorSlot` emits — that a
configured component mounts at its point, in the DOM position the point promises,
carrying the context the point declares. Nothing asserts Example's copy.

One assertion in there is subtler than it looks: every per-row badge renders
*identical* text, so a contribution that ignored its context entirely would
satisfy any text assertion. What proves context is per-row is that the
contributions are **distinguishable from each other** — so that spec asserts
distinctness and deliberately says nothing about the values.

## Conventions

- **Import `{ test, expect }` from `../fixtures/test`.** That fixture fails any
  test where the app logged an error or threw, which is how a spec can trust its
  own green — a page can satisfy every assertion while throwing in an effect.
  Deliberate noise (the 500 the error scenario provokes) is filtered there, in
  one place, with a reason.
- **Two specs per area**: `<area>.spec.ts` for the success journey,
  `<area>-errors.spec.ts` for the failure journey.
- **One test per journey**, with each criterion a numbered `test.step`. Playwright
  records one trace per test, and a journey split across tests loses the thing
  worth watching — that the state one step established is the state the next one
  acted on.
- **Prefer roles and test ids over prose.** Most of these pages are still going to
  be rebuilt; a spec anchored to copy will not survive that, and one anchored to
  `nav-agents` or `getByRole("row")` will.
- **Assert against the list a user would read**, not against a toast or a closed
  modal. A success message proves the app thinks it worked.

## Live runs, against a real backend

```bash
cd ui
yarn test:pw:live
UI_LOOP_LIVE_PORT=8312 yarn test:pw:live   # to run beside something on 8301
```

Unlike `yarn test:pw`, this one **does** need a cluster, with the controller
port-forwarded. It is not run in CI. The specs live in `playwright/live/`, and the
coverage deliberately left out of it is in `DEFERRED.md`.

A live run reaches the controller through Vite's proxy, exactly as a deployed
build reaches it through nginx, so the app uses the same relative URLs either way
and this mode tests the addressing a real deployment uses.

**Why a separate mode rather than a third project.** `UI_LOOP_LIVE=true` swaps the
whole `projects`/`webServer` pair in `playwright.config.ts` instead of appending to
it, because the two modes' requirements are mutually exclusive. A live project in
the default list would make `yarn test:pw` — which is meant to need nothing but a
machine that can run the dev server — fail on any laptop without a cluster in
front of it. And a live run has no use for the two mock servers, so starting them
would cost every live run the time to boot Vite twice for nothing. The two runs
are disjoint. The live project also gets its own port, 8301, far from the mock
servers' 8001/8051 for the same reason those two are 50 apart.

**A green live run has to have been live.** `VITE_API_MODE` is pinned at build
time as well as at runtime, because a build-time pin is the one thing an inherited
`.env` cannot override — and a live suite that quietly answered from fixtures
would be worse than a red one, since a green one gets taken as evidence the
cluster works. `globalSetup` asks the page what settings it was actually handed
and refuses the run if they are not the live ones. Traces are kept on failure:
unlike the mock suite there is no fixed fixture to re-read afterwards, so the
trace is the only record of what the cluster answered.
