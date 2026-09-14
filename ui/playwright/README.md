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
  tests/           one <resource>/<resource>.spec.ts per resource, plus the
                   cross-cutting ones: app-shell, routing, auth, chat, substrate,
                   extensions, dashboard, theme-contrast
  helpers/         app (navigation, tables, scenarios), resource (the moves every
                   CRUD journey makes), nav (shell chrome), extensions (slots),
                   chat, controls, style, mockCalls
  fixtures/test.ts import { test, expect } from here — never @playwright/test
  live/            the live suite: specs, plus helpers/ of its own
  DEFERRED.md      the specs not yet portable, and what each one is waiting on
```

The resources with a lifecycle spec are **models**, **MCP servers**, **prompt
libraries**, **agent templates**, **harnesses** and **schedules**. Two of them are
narrower than CRUD, and in both cases that is the product rather than a gap: a
registered MCP server's address is its identity, so `ToolService` serves no update;
and the harnesses tab offers no edit, though `HarnessService` would take one.

**Agents are not on that list, because an agent is not created.** An agent is an
`AgentTemplate` paired with a `Harness` — it exists the moment a harness admits a
template — so `agents/` covers what the pairing means rather than a lifecycle.

## App extensions: two servers, two projects

Which extensions a build installs is decided at build time, so "installed" and
"not installed" cannot be two states of one server. The config boots two:

| Project | Server | Specs |
|---|---|---|
| `chromium` | bare — no extension, on `UI_LOOP_PORT` | everything not matching `*.withExtension.spec.ts` |
| `chromium-with-extension` | `VITE_EXAMPLE_EXTENSION=true`, on `UI_LOOP_PORT + 50` | `*.withExtension.spec.ts` |

A spec opts into the extension-installed app by being named `*.withExtension.spec.ts`.

The gap of 50 between the ports is deliberate. Vite falls forward to the next
free port when the one it is told to use is busy, so with adjacent ports a
slow-to-die server from a previous run can push one app onto the other's port —
which surfaces as a spec mysteriously unable to find the contribution it is
asserting on. `globalSetup` also checks that each port is serving the build its
project expects, and fails the run immediately with that explanation if not, so
a harness problem cannot be mistaken for a product one.

**Assert the mechanism, never the example.** The bundled Example App Extension is
documentation that happens to run, and it is expected to change. Specs go
through the `extension-slot-<id>` test id that `ExtensionSlot` emits — that a
configured component mounts at its point, in the DOM position the point promises,
carrying the context the point declares. Nothing asserts the example's copy.

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
- **One spec per resource, holding one test: that resource's whole life.** Create
  it, read it back, change it, delete it, and the empty and failure states around
  those — all in one `test`, each criterion a numbered `test.step`. Playwright
  records one video and one trace per *test*, so a lifecycle split across four of
  them is one you have to reassemble from four recordings, none of which shows
  that the thing the delete removed is the thing the create made.

  The trade is deliberate: a step that fails stops the ones after it, so a broken
  create hides whether delete works. That is the right way round here — a resource
  whose create is broken is broken, and the recording shows where it stopped.

  This replaced a `<area>.spec.ts` / `<area>-errors.spec.ts` pair per area, plus
  four cross-cutting specs (`forms/required-fields`, `lists/list-filters`,
  `refresh-toast`, `mcp-servers/row-interaction`) that each asserted one property
  across five pages. Those properties now sit in the journey of the resource they
  are about, which costs one page load instead of five and puts the claim where
  somebody changing that page will see it.
- **Keep the writes in one browsing context.** The fixture backend keeps writes in
  the page's own memory, so a `page.goto` starts a backend that has never heard of
  the thing just created — and the failure reads as "the create did not stick"
  when nothing is wrong. Click through from the list once the journey has written
  something.
- **A lifecycle gets a longer budget than a journey.** Each resource spec sets
  `test.describe.configure({ timeout: LIFECYCLE_TIMEOUT })`. Per file, not across
  the suite: the thirty-second default is load-bearing everywhere else, where a
  mock-backed page that needs longer is stuck rather than merely long.
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
