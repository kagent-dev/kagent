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
libraries**, **agent templates**, **harnesses** and **schedules**. Two are narrower
than CRUD because the product is: an MCP server's address is its identity, so
`ToolService` serves no update, and the harnesses tab offers no edit.

**Agents are not among them, because an agent is not created.** An agent is an
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

## Where a new spec goes

Four rules. `playwright/conventions.test.ts` checks 1 to 3 alongside the unit tests,
because they are claims about the *tree* — which folder a file sits in, how many specs
a folder holds — and a lint rule sees one file at a time. ESLint covers the other two
conventions below: the shared fixture import, and antd's class names.

1. **One folder per area, and a test title begins with that folder.**
   `models/models.spec.ts` holds `models: …`, and everything in `chat/` begins
   `chat…`, so `--grep "^chat"` is the whole area. A file with a narrower subject
   keeps both halves — `chat agent rail: …` — so the title says the area *and* what
   in it. A file whose subject is the folder needs no second word.
2. **A resource's folder holds one spec, holding one test**: that resource's whole
   life. **Its empty and failure states go last**, because they need `?mock=`, which
   is per-navigation — so reaching them resets the fixture backend's memory and
   discards whatever the lifecycle created.
3. **Everything that is not a resource sits at the top level** — the shell, routing,
   the dashboard, theme contrast.

   **A folder is a surface, not a subject**, because one subject appears on several:
   a conversation is a row on the agent's page, a row in the chat rail, and the
   transcript between them. "Which page is this about" has an answer; "which spec
   owns conversations" does not. So **where an operation exists on two surfaces, one
   owns it** and the other asserts only what differs about reaching it there — the
   agent's table owns conversation rename and delete, and the rail keeps one test for
   doing either without leaving the conversation. **A journey between surfaces lives
   with the one it starts from**: `agents/agent-chat-entry.spec.ts` sits in `agents/`
   because that is where the reader starts, and it exists because every other spec
   deep-links into the chat page and so cannot test that anything *links* to it.
4. **Fixture identity comes from `helpers/app.ts`**, not a UUID pasted into a spec.
   `instances` and `agents` name each fixture for what it is *for*, so a changed
   fixture is one edit. Where a spec needs one the helper has no name for, give it a
   named constant at the top of the file saying what it stands for — never an inline
   literal.

## Conventions

- **Import `{ test, expect }` from `../fixtures/test`.** That fixture fails any test
  whose page logged an error or threw, which is what lets a spec trust its own green —
  a page can satisfy every assertion while throwing in an effect. Deliberate noise is
  filtered there, in one place, with a reason.
- **One spec per resource, holding one test: that resource's whole life.** Create it,
  read it back, change it, delete it, and the empty and failure states around those —
  one `test`, each criterion a numbered `test.step`. Playwright records one video and
  one trace per *test*, so a lifecycle split across four of them is one you have to
  reassemble from four recordings, none of which shows that the thing the delete
  removed is the thing the create made.

  The trade is deliberate: a failed step stops the ones after it, so a broken create
  hides whether delete works. That is the right way round — a resource whose create is
  broken is broken, and the recording shows where it stopped.
- **Keep the writes in one browsing context.** The fixture backend keeps writes in the
  page's own memory, so a `page.goto` starts a backend that has never heard of the
  thing just created, and the failure reads as "the create did not stick" when nothing
  is wrong. Click through from the list instead.
- **A lifecycle gets a longer budget than a journey.** Each resource spec sets
  `test.describe.configure({ timeout: LIFECYCLE_TIMEOUT })`. Per file, not across the
  suite: the thirty-second default is load-bearing everywhere else, where a
  mock-backed page needing longer is stuck rather than merely long.
- **Prefer roles and test ids over prose.** Most of these pages are still going to be
  rebuilt; a spec anchored to copy will not survive that, and one anchored to
  `nav-agents` or `getByRole("row")` will.

  **Drive by test id, assert on text.** A `data-testid` reached with `getByTestId` —
  not the HTML `id`, which is a different attribute and the page's rather than ours.
  antd generates one per form control from `Form.Item`'s `name`, and that is what
  `getByLabel` resolves through, so a spec leaning on it is coupled to both the
  label's wording *and* a field name it never chose. The id says which control; the
  words are usually what the test is about. `schedule-pause` is one button whose label
  flips between Pause and Resume — the id selects it, and the label is free to be the
  assertion.
- **Reach for antd's own class names only inside `helpers/`.** `.ant-popconfirm`,
  `.ant-select-item-option`, `.ant-modal` and friends are that library's internals,
  and an upgrade that renames one should be a change to a helper rather than to a
  dozen specs. `chooseFilter`, `confirmDelete` and `pressOnce` exist for the ones that
  come up most.
- **Press a dialog's button with `pressOnce`, not `click`.** antd animates a modal and
  a popconfirm in, and Playwright can compute a click's coordinates while one is still
  arriving — measured once at 238px left and 224px below the button, on the backdrop,
  with the button itself never having moved. Its stability check compares two animation
  frames, and a starved main thread serves both from the same frame of the animation;
  `pressOnce` samples on a clock instead.

  **`pressUntil` retries rather than waiting, and needs an argument for why that is
  safe here.** Retrying wants a button that can be pressed twice *and* a page
  underneath that can take a stray click, because a retry going out after the dialog
  has closed lands on whatever is behind it — a popconfirm sits over its own trigger,
  and a modal usually sits over a list of links.
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
