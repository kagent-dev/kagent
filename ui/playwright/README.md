# Playwright E2E tests

Page-level browser end-to-end tests for the kagent UI, run against a **mocked
backend**. This suite fills the one gap nothing else covers: **multi-step user
flows across components**.

## What this suite does and does not cover

| Layer | Tool | Not Playwright's job |
|---|---|---|
| Atoms (`src/components/ui/*`) | shadcn primitives | skip |
| Visual / render states | Storybook + Vitest-browser + Chromatic | skip |
| Unit / logic | Jest (`*.test.ts(x)`) | skip |
| Page-load smoke (`h1` renders, page reachable) | Playwright (folded into the app-shell journey, `tests/app-shell.spec.ts`) | — |
| **Multi-step flows: form submission, payload correctness, streaming, wizard completion, error/edge states** | **Playwright (this suite)** | — |

Rule: if Chromatic / Jest already assert it, Playwright does not.

## How mocking works (important)

Nearly every `/api/**` call runs **server-side** inside Next.js server actions
(`src/app/actions/*.ts`, all `"use server"`). The browser sends an opaque RPC
POST to the route; the Node server does the actual backend `fetch`. So browser
`page.route("**/api/**")` intercepts **nothing** on load.

Instead we run a standalone **stub backend** (`mocks/server.mjs`) and point Next
at it with `BACKEND_INTERNAL_URL` — `getBackendUrl()` (`src/lib/utils.ts`) checks
that env var first. Playwright's `webServer` (in `../playwright.config.ts`) boots
both the stub (`:8899`) and `next dev` (`:8001`).

The one exception is A2A chat streaming (`POST /a2a/**`, SSE), which **is**
browser-originated and `page.route`-able — used in the chat spec (Stage 2).

## Layout

```
playwright/
  tests/          # *.spec.ts, one per feature area
  helpers/        # reusable drivers: page, nav (forms/select/dialog land with Stage 2)
  mocks/
    server.mjs    # stub backend (happy-path data + /__mock/scenario overrides)
    data.ts       # typed spec-side builders + ok() envelope (mirror server.mjs shapes)
    control.ts    # semantic mock seam: mock.noAgents(), mock.agentsError(), …
  fixtures/
    test.ts       # import { test, expect } from here; provides the `mock` fixture
  tsconfig.json
```

## Running

```bash
cd ui
npm install
npx playwright install chromium   # first time only
npm run test:pw                   # headless
npm run test:pw:ui                # interactive UI mode
npm run test:pw:debug             # step-through debugger
```

Playwright starts the stub + dev server automatically. To drive the app manually
against the stub:

```bash
node playwright/mocks/server.mjs &
BACKEND_INTERNAL_URL=http://127.0.0.1:8899/api npm run dev
# open http://localhost:8001
```

## Conventions

- Import `{ test, expect }` from `../fixtures/test`, never `@playwright/test`.
- One spec file per feature area (`tests/agents/`, `tests/chat/`, …). Each area is
  **two journeys → two videos**:
  - a **success journey** that opens on the empty state (where one exists) then
    walks the whole happy-path lifecycle (create → configure → use → delete →
    confirm), one `test.step()` per phase;
  - a **failure journey** that consolidates every negative/edge path (validation
    blocks, error toasts, not-found states) into `test.step()`s.

  Splitting success from failure keeps a broken edge case from taking the
  happy-path video down with it (and vice versa) while still collapsing to ~2
  report entries per area. Because captured requests and scenario overrides
  accumulate across steps in one `test()`, each failure step calls `mock.reset()`
  first so it starts from a clean slate. The app-shell smoke journey is the one
  exception — a single test covering list states + header nav.
- **`data-testid` policy:** prefer `getByRole` / `getByLabel`. Add `data-testid`
  only where role/text is ambiguous or unstable (list rows, per-item action
  buttons, wizard steps, combobox options). Add incrementally — no upfront sweep.
  Keep the existing `data-test` model-edit hooks; the Stage 2 Models flow relies
  on them.

## Roadmap

- **Stage 0 (done):** foundation — config, stub backend, CI, one smoke test.
- **Stage 1 (done):** page/nav helpers (`helpers/*`) + per-test scenario overrides —
  the `mock` fixture drives the stub's `/__mock/scenario` endpoint via `mocks/control.ts`
  (e.g. `mock.noAgents()`, `mock.agentsError()`), verified by the app-shell journey.
  Runs serially (`workers: 1`) against the shared stub; raising the worker count later
  needs per-worker servers or stateless request-keyed scenarios.
- **Stage 2:** feature flows (gap-scoped), then consolidated so each feature area
  is **one success journey + one failure journey** (see Conventions), landing at
  ~14 videos total. Shared infra (POST-capture, A2A SSE mock, `select` helper) is
  demand-driven. The feature areas:
  - [x] App shell — list states + header nav — `tests/app-shell.spec.ts`
  - [x] Agents — create (declarative + harness) & delete — `tests/agents/agents.spec.ts`
  - [x] Chat / session (A2A SSE mock) — `tests/chat/chat-session.spec.ts`
  - [x] Models / providers — `tests/models/models.spec.ts`
  - [x] MCP servers & tools — `tests/mcp/mcp-server.spec.ts`
  - [x] Prompt libraries — `tests/prompts/prompt-libraries.spec.ts`
  - [x] Onboarding completion — `tests/onboarding/onboarding.spec.ts`
