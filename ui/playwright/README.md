# Playwright E2E tests

Page-level browser end-to-end tests for the kagent UI, run against a real kagent
backend in a kind cluster. This suite covers **multi-step user flows across
components** — the gap unit tests, Storybook, and Chromatic don't fill.

## What this suite covers

| Layer | Tool |
|---|---|
| Atoms (`src/components/ui/*`) | shadcn primitives — skip |
| Visual / render states | Storybook + Chromatic |
| Unit / logic | Jest / Vitest |
| **Multi-step flows: create → configure → use → delete, validation, streaming** | **Playwright (this suite)** |

## How it works

The UI fetches data server-side (Next.js server actions), and the chat stream
`POST /a2a/**` runs server-side through the Next route handler
(`src/app/a2a/[namespace]/[agentName]/route.ts`). Both resolve their target via
`getBackendUrl()` (`src/lib/utils.ts`), which reads `BACKEND_INTERNAL_URL`.

A lightweight proxy (`mocks/server.mjs`) sits at that address and:

- forwards every `/api/**` request to the real kagent backend (`KAGENT_BACKEND_URL`);
- intercepts `/a2a/**` and `/a2a-sandboxes/**`, answering with a canned SSE reply,
  so the suite never needs a live LLM.

```
Browser ─▶ next dev :8001 ─┬─ /api/* (server actions) ─┐
                           └─ /a2a/* (route handler) ───┤
                                       proxy :8899 ─┬─ /a2a/* ─▶ mocked SSE
                                                    └─ /api/* ─▶ real backend :8083
```

`playwright/setup.ts` port-forwards the controller to `:8083` for the run;
`playwright/teardown.ts` stops it. Playwright's `webServer` boots the proxy and
`next dev`.

## Layout

```
playwright/
  tests/          # <area>.spec.ts + <area>-errors.spec.ts per area, plus cleanup.spec.ts
  helpers/        # page, nav, select, a2a drivers
  mocks/
    server.mjs    # proxy: forwards /api to the real backend, mocks chat
  fixtures/
    test.ts       # import { test, expect } from here
  scripts/
    setup.sh      # build + install kagent into kind
```

## Running

```bash
cd ui
npm install
npx playwright install chromium       # first time only

./playwright/scripts/setup.sh         # kind cluster + real kagent (once)
yarn run test:e2e                     # (or: npm run test:e2e)
```

`setup.sh` builds the images and installs kagent via `make create-kind-cluster`
and `make helm-install`. It needs a provider key; since chat is mocked, a dummy
works (`export OPENAI_API_KEY=fake`). `test:e2e` port-forwards the controller,
boots the proxy + `next dev`, and runs the suite.

Point at an already-reachable backend with `KAGENT_BACKEND_URL=http://<host>:8083`.

Interactive / debug:

```bash
npm run test:pw:ui       # interactive UI mode
npm run test:pw:debug    # step-through debugger
```

## Conventions

- Import `{ test, expect }` from `../fixtures/test`.
- Two specs per feature area: `<area>.spec.ts` (the success/CRUD journey) and
  `<area>-errors.spec.ts` (the validation/error journey).
- Mutating specs create uniquely-named resources and delete them; `cleanup.spec.ts`
  sweeps any `e2e-*` leftovers from interrupted runs. Seeded resources (never
  prefixed `e2e-`) are left untouched.
- Prefer `getByRole` / `getByLabel`; add `data-testid` only where role/text is
  ambiguous (list rows, per-item action buttons).
- The suite runs serially (`workers: 1`) against one shared cluster.
```
