# kagent UI

React + TypeScript, built with Vite. Routing is React Router, data fetching is
SWR, components are Ant Design styled with Emotion, and the UI is extensible
through named extension points — see
[docs/vendor-extensions.md](./docs/vendor-extensions.md).

Node is pinned in [`.nvmrc`](.nvmrc); Yarn ships via corepack.

## Running locally

```bash
yarn install
yarn dev          # http://localhost:8001
```

**No cluster is required.** By default the app runs against an in-browser mock
backend, so a fresh checkout is usable immediately.

### Driving states in mock mode

Append `?mock=<scenario>` to any route to force a state that is otherwise hard to
reach by hand:

| Scenario | Effect |
| --- | --- |
| `ok` | normal data (the default) |
| `empty` | every collection comes back empty |
| `error` | requests fail, so error handling is visible |
| `slow` | responses are delayed, so loading states are visible |

The choice persists across in-app navigation, e.g. `/agents?mock=error`.

### Running against a real backend

Follow [DEVELOPMENT.md](../DEVELOPMENT.md) to get kagent running, then
port-forward the controller:

```bash
kubectl port-forward svc/kagent-controller 8083
```

Then start the UI in live mode:

```bash
cp .env.example .env     # then set ENABLE_MOCK_UI=false
yarn dev
```

The dev server proxies `/api` and `/a2a` to `127.0.0.1:8083`, matching what nginx
does in a deployed cluster, so the app uses the same relative URLs either way.
Override the target with `KAGENT_DEV_CONTROLLER_URL`, or set `API_BASE_URL` to
call a backend directly and bypass the proxy.

Which backend is serving is decided in exactly one place, `src/api/config.ts`.
Nothing above the data layer knows or cares.

### Deployment settings

Settings an operator configures per deployment cannot travel in the bundle — the
bundler inlines `import.meta.env` at build time, which would freeze the chart's
values as of the image build. They arrive on `window.environmentVariables`
instead, rendered from the pod's environment by `scripts/init.sh` on every start
and loaded by a script tag ahead of the app, so they are readable synchronously
by the first module that runs. `src/env.ts` lists them; `.env.example` documents
setting them locally.

That synchronous delivery is load-bearing rather than incidental: the API base URL
is a module-level constant, so there is no point early enough for an awaited fetch
to have landed.

| Setting | Effect |
| --- | --- |
| `API_BASE_URL` | where the browser calls the API (default `/api`) |
| `ENABLE_MOCK_UI` | `true` serves the whole API from in-browser fixtures |
| `SSO_REDIRECT_PATH` | where "Sign in with SSO" sends the browser |
| `STREAM_TIMEOUT_MS` | chat stream inactivity timeout |

`ENABLE_MOCK_UI` is a development setting. The release image deliberately ships no
mock backend, so a built bundle logs a warning and uses the real API rather than
entering a mode it cannot serve.

## Checks

```bash
yarn typecheck    # tsc
yarn lint         # eslint
yarn test         # unit tests (vitest)
yarn test:pw      # end-to-end tests (playwright)
```

The end-to-end suite runs against the mock backend and needs **no cluster, no
port-forward, and no provider credentials**. See
[playwright/README.md](./playwright/README.md), and
[playwright/DEFERRED.md](./playwright/DEFERRED.md) for coverage not yet ported.

## Layout

| Path | Contents |
| --- | --- |
| `src/api/` | data layer — typed domain models, SWR hooks, the chat client interface |
| `src/mocks/` | in-browser mock backend and its fixtures |
| `src/pages/` | one module per route |
| `src/components/Structure/` | app shell — header, sidebar, page frame |
| `src/vendorExtensions/` | extension framework, and a worked example |
| `src/theme/` | design tokens, in one place so styling can be overridden |
| `playwright/` | end-to-end suite |
