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
| Page-load smoke (`h1` renders, page reachable, onboarding steps 1–2, model-edit page opens) | Cypress (`cypress/e2e/smoke.cy.ts`) | skip |
| **Multi-step flows: form submission, payload correctness, streaming, wizard completion, error/edge states** | **Playwright (this suite)** | — |

Rule: if Cypress / Chromatic / Jest already assert it, Playwright does not.

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
  helpers/        # (Stage 1) reusable drivers: page, forms, select, dialog, nav
  mocks/
    server.mjs    # stub backend (runtime source of happy-path data)
    data.ts       # typed spec-side builders (mirror server.mjs shapes)
  fixtures/
    test.ts       # import { test, expect } from here in every spec
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
- One spec file per feature area (`tests/agents/`, `tests/chat/`, …); use
  `test.step()` for sub-phases of a multi-step flow.
- **`data-testid` policy:** prefer `getByRole` / `getByLabel`. Add `data-testid`
  only where role/text is ambiguous or unstable (list rows, per-item action
  buttons, wizard steps, combobox options). Add incrementally — no upfront sweep.
  Do **not** remove/rename the existing `data-test` model-edit hooks; Cypress
  depends on them.

## Roadmap

- **Stage 0 (done):** foundation — config, stub backend, CI, one smoke test.
- **Stage 1:** helper/driver library (`helpers/*`) + per-test scenario overrides
  via the stub's `/__mock/scenario` endpoint. Prefer stateless scenario selection
  keyed by request content (e.g. `?namespace=empty-ns` → `[]`); fall back to the
  control endpoint with `workers: 1` for endpoints lacking a discriminator.
- **Stage 2:** feature flows (gap-scoped), ordered by importance —
  Create Agent → Chat/session (A2A SSE mock) → Models → MCP → Onboarding completion.
