# Web interface

A Vite single-page app. It is a static bundle served by nginx: there is no server
process, so there are no server components, no server-side data fetching and no
file-system routing.

**Stack:** Vite + React 19, TypeScript, antd 6 for components, Emotion for styling
(the `css` prop, via `jsxImportSource`), SWR for reads, Yarn 4. React Router owns
routing; there is no file-system routing and no server rendering.

### Commands

Run these from `ui/`:

| Task | Command |
|------|---------|
| Dev server | `yarn dev` |
| Unit tests | `yarn test` |
| End-to-end, no cluster needed | `yarn test:pw` (Chromium and Firefox) |
| End-to-end against a real cluster | `yarn test:pw:live` |
| Type check | `yarn typecheck` |
| Lint | `yarn lint` |

Only lint **errors** gate a change; a handful of warnings are pre-existing.

`../scripts/setup-cluster/setup-cluster.sh` builds a Kind cluster with kagent on it in one
command, for work that needs a real backend.

### Settings reach the app at runtime, not at build time

Configuration is read from `window.environmentVariables`, which the container
rewrites from its own environment on every start. So one image serves every
deployment, and a setting is an operator's decision rather than something frozen
into a build. Locally the same values come from `.env` (git-ignored;
`.env.example` documents each one).

Two consequences worth knowing before touching that code:

- The script that supplies them is **synchronous** in `index.html`. Several modules
  read settings at import time, so anything awaited would be read before it arrived.
- `import.meta.env` is for build-time flags only. A value that an operator should be
  able to change belongs in `window.environmentVariables`.

### Fixtures are opt-in

`ENABLE_MOCK_UI=true` serves the whole API from an in-browser mock (MSW) with no
cluster at all, and `?mock=ok|empty|error|slow` picks which scenario the fixtures
play. **It is off unless asked for**, in a dev server exactly as in a built image: a
page that quietly serves fixtures when the backend is down looks healthy while
showing data that was never real.

When mock mode is on it overrides every backend setting, and anything reporting who
is signed in correctly reports nobody — there is no backend to have signed in to.

### Extension points

An `AppExtensionConfig` contributes navigation entries and overrides, routes and
route handles, slots, form fields, table columns, API overrides, providers, theme
tokens, shell regions, branding, provider icons, chat part renderers and agent
links. Components read every colour, radius and font from those tokens, so
overriding them restyles components an extension never touches. When adding a
feature, check whether it belongs behind an extension point rather than as a branch
inside a shared component.

Several are installed at once, as the ordered `activeAppExtensions` array. Additive
contributions from every entry take effect in order; singular ones are merged with
the later entry winning.

The full guide is [app extension guide](docs/app-extensions.md).

### Conventions specific to this codebase

- **Say when data is not real.** A page showing fixtures says so on the page. Never
  suppress an error because a mock flag is set — a broken backend must not render as
  healthy mock data.
- **Normalise at the client boundary.** Go marshals a nil slice as JSON `null`, so
  any collection the controller has nothing for arrives as null. Fix it once where the
  response is parsed, not at each use.
- **Fixtures must match the controller, not each other.** A fixture, a type and a
  test can agree perfectly and all three be wrong; that has happened here more than
  once and each time only a real cluster objected. Check the CRD.
- **Prefer a smaller honest test suite** over a green one that proves nothing.
  Coverage debt belongs in `playwright/DEFERRED.md`, not in skipped specs.
