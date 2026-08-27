# Developing an app extension

## Install one

Two lines in `src/appExtensions/activeExtensions.ts` — an import, and an entry in
the array:

```ts
import { exampleAppExtension } from "./example/exampleExtension"; // 1. import it

export const activeAppExtensions: readonly AppExtensionConfig[] = [
  exampleAppExtension, // 2. add it
];
```

That is the whole integration surface. Installing a second extension is one more
entry; nothing else in the application changes.

Nothing is installed by default, including the bundled example. To see the example
running without editing the file, start the dev server with
`VITE_APP_EXTENSIONS=example yarn dev`.

---

This UI is built to be extended without editing the application itself. A
distribution contributes navigation entries, whole pages, components at named
points inside existing pages, extra form fields, API endpoint overrides and
payload transforms, and app-level React providers — all declared in **one
configuration object** per extension.

The model is deliberately close to [Backstage](https://backstage.io) plugins: a
plugin is a self-contained module, and installing it costs a small, explicit edit
in the host application rather than magic discovery.

## Several extensions at once

`activeAppExtensions` is an ordered array, and the order is the precedence. An
extension's config is the same shape whether it is installed alone or alongside
others — what the array decides is only how two of them are reconciled:

| | Rule |
|---|---|
| `navItems`, `routes`, `formFields`, `tableColumns`, `providers`, `slots` | **Additive.** Every extension's contributions take effect, in array order. Nav items are then re-sorted by `order`, so a contribution lands at its position regardless of which extension supplied it. |
| `theme`, `shell`, `branding`, `navOverrides`, `agentLinks`, `providerIcons`, `api` | **Merged, later wins**, field by field. An extension replacing the header does not blank a sidebar an earlier one replaced. |

Two consequences worth knowing:

- **A slot renders every contribution mounted at it**, not just one. A point is a
  place in the layout, not a single appointment, so two extensions each adding a
  header action get both — installing one must never silently switch off part of
  another.
- **API transforms compose; overrides do not.** Operation, endpoint and base-URL
  overrides are single-valued, so the later extension wins. Request and response
  transforms are a list applied in order, so both extensions' transforms run and the
  later one sees the earlier one's work.

Collisions that cannot be reconciled — two extensions claiming the same route path
or the same nav key, or the same extension installed twice — are rejected at
startup rather than resolved arbitrarily. See [Validation](#validation).

Import everything from the `@/appExtensions` barrel. Anything below it is
internal and free to move:

```ts
import { ExtensionSlot, defineExtensionFormField } from "@/appExtensions";
import type { AppExtensionConfig } from "@/appExtensions";
```

### Running the bundled example

A complete worked example — the **Example App Extension** — lives in
`src/appExtensions/example/`. The application ships with **no** extension
installed, so a default build renders only what this project itself provides.
Switch it on with:

```bash
VITE_APP_EXTENSIONS=example yarn dev
```

Read that directory alongside this document — it exercises every extension point
described here.

---

## The configuration object

```ts
interface AppExtensionConfig {
  id: string;                                          // stable machine id, e.g. "example"
  name: string;                                        // human-readable name
  navItems?: readonly ExtensionNavItemContribution[];     // sidebar entries
  routes?: readonly ExtensionRouteContribution[];         // whole pages
  slots?: ExtensionSlotComponents;                        // components at named points
  formFields?: readonly ExtensionFormFieldContribution[]; // extra fields in core forms
  api?: ExtensionApi<EndpointId>;                // endpoint overrides + transforms
  providers?: readonly ExtensionProviderComponent[];      // app-level context providers
}
```

Only `id` and `name` are required. A config that contributes a single nav item is
a complete, valid config. This is one extension's whole declaration; installing it
alongside others changes nothing about the shape.

### One opinion worth stating up front

**The extension always supplies the whole renderer.** There is no "just give me a
label and a link" shorthand anywhere, and none will be added. Partial
configuration surfaces multiply forever — every consumer eventually needs one
more property, an icon, a badge, a tooltip, a variant — and each one becomes a
compatibility obligation for this project. Requiring a component costs an
extension a few lines once and costs the host nothing thereafter.

If you want something that looks exactly like a core element, import the core
component and render it yourself.

---

## Navigation entries

`order` is the only positioning input. Core items sit at multiples of 100, so
`250` lands **between** Agents (200) and Models (300) — contributions interleave
with the application's own navigation rather than being appended after it.

```tsx
const exampleNavItem: ExtensionNavItemContribution = {
  key: "exampleInsights",   // unique across core and extension items
  order: 250,
  path: "/example/insights", // used for active-state matching only
  Component: ExampleNavItem, // receives { isActive: boolean }
};
```

Your component renders its own link. `path` is optional and exists only so the
framework can tell you whether you are the active item.

## Pages

Contributed routes are merged into the router ahead of the catch-all, so `*`
keeps meaning "not found". A contributed page renders **inside** the app shell by
default, because an extension page is a page of this application rather than a
separate site. `standalone: true` opts out for full-screen flows such as your own
login.

```tsx
routes: [
  { path: "/example/insights", element: <ExampleInsightsPage /> },
  { path: "/example/onboarding", element: <ExampleOnboarding />, standalone: true },
]
```

A path that collides with a core route is rejected at startup — see
[Validation](#validation).

## Component slots

Every point the application offers is listed in `EXTENSION_POINT_IDS`. IDs are
shaped `app_<area>_<page>_<component>_<slot>`, so the name alone says where the
point lives.

| Extension point ID | Context passed to your component |
| --- | --- |
| `app_shell_appHeader_actions_leading` | none |
| `app_shell_appLayout_contentArea_leadingBanner` | none |
| `app_shell_appLayout_contentArea_globalOverlay` | none |
| `app_shell_appLayout_appSidebar_footer` | none |
| `app_agents_agentsList_pageHeader_actions` | none |
| `app_agents_agentsList_agentListItem_badge` | `{ agentName: string; namespace: string }` |
| `app_agents_agentChat_agentChatMessage_additionalActionsButton` | `{ messageId: string; role: "user" \| "agent"; text: string }` |
| `app_dashboard_dashboardOverview_summaryGrid_leadingCard` | none |

Mount a component by naming the point:

```tsx
slots: {
  app_shell_appLayout_contentArea_leadingBanner: ExamplePolicyBanner,
  app_agents_agentsList_agentListItem_badge: ExampleAgentBadge, // gets agentName + namespace
}
```

The ID union is derived from the runtime list, so a point cannot exist in the
type system without also existing at runtime. **A typo is a compile error**, and
`tsc` will suggest the correct ID.

### Render modes

Each point declares how it reaches the DOM, in `EXTENSION_POINT_RENDER_MODE`:

- **`inline`** — rendered where the slot sits. Correct whenever the contribution
  belongs in the surrounding layout flow. This is nearly everything.
- **`portal`** — rendered into `document.body`. Used only where a contribution
  must escape its parent's DOM position. Today that is
  `app_shell_appLayout_contentArea_globalOverlay`: the content area is an
  `overflow: auto` scroll container with its own stacking context, so a floating
  overlay declared inside it would be clipped by the scroll box and trapped
  beneath sibling chrome.

Prefer `inline`. Reach for `portal` only when clipping or stacking genuinely
requires it.

## Form fields

A contributed field declares its own component **and** how its value maps into
and out of the request payload — necessary because a downstream API rarely uses
the same shape as the reference one.

Target forms are listed in `EXTENSION_FORM_IDS`:
`app_agents_agentNew_agentForm`, `app_models_modelNew_modelForm`,
`app_mcpServers_mcpServerNew_mcpServerForm`.

```ts
export const exampleComplianceTierField = defineExtensionFormField({
  id: "exampleComplianceTier",
  formId: "app_agents_agentNew_agentForm",
  Component: ExampleComplianceTierField,
  fromPayload: (payload) => payload.metadata?.labels?.["example.tier"] ?? "standard",
  toPayload: (payload, value) => ({
    ...payload,
    metadata: {
      ...payload.metadata,
      labels: { ...payload.metadata?.labels, "example.tier": value },
    },
  }),
  validate: (value) => (value ? undefined : "Pick a compliance tier"),
});
```

`fromPayload` seeds the field when editing; `toPayload` writes it wherever your
API expects it; `validate` returns a message or `undefined`.

## Table columns

A slot cannot add a column. A slot occupies a position in the DOM, whereas a
column is a heading, a per-row renderer and a place in an ordering — three things
that must be declared together for a table to lay out at all.

This is how a product whose domain is wider than this application's shows that
extra dimension on a page the application still owns. Nothing replaces the page.

```ts
export const clusterColumn = defineExtensionTableColumn<AgentResponse>({
  id: "cluster",
  tableId: "app_agents_agentsList_table",
  title: "Cluster",
  after: "namespace",          // positioned after that core column's key
  render: (row) => row.agent.metadata.labels?.["cluster"] ?? "—",
});
```

Target tables are listed in `EXTENSION_TABLE_IDS`. `after` naming a column the table
does not have puts the contribution at the end rather than dropping it, so a core
table can lose a column without an extension's disappearing with it.

Pages fold contributions in with `withExtensionColumns`, so adding one needs no
change to the page.

## API overrides and transforms

Keyed by the data layer's own endpoint IDs, so naming a call that does not exist
fails to compile.

```ts
api: {
  baseUrl: "https://control-plane.example.com/api",   // optional: replace the API root
  endpoints: { "agents.list": "/managed-agents" },      // optional: per-endpoint path
  transforms: {
    "agents.list": {
      request: (context) => ({
        ...context,
        headers: { ...context.headers, "x-example-tenant": currentTenant() },
      }),
      response: (body) => unwrapExampleEnvelope(body),
    },
  },
}
```

`request` runs after the URL resolves and before the call is sent; `response`
runs on the parsed body before it reaches the caller. Both may be async.
`installExtensionApi` folds this declarative shape into the data layer's
runtime registry — resolution itself belongs to `src/api`, so there is exactly
one description of a request in the codebase.

### Something every request needs

A control plane usually demands something of *all* its traffic rather than of one
endpoint — an authorization header, a tenant, a correlation id. The per-endpoint
table is the wrong shape for that: it means an entry per endpoint, and the endpoint
somebody adds next week goes out without it.

```ts
api: {
  baseUrl: "https://control-plane.example.com/api",
  request: (context) => ({
    ...context,
    headers: { ...context.headers, authorization: `Bearer ${token()}` },
  }),
}
```

This hook runs **last** — after `baseUrl` and after any per-endpoint transform —
so `context.url` is the URL the request will actually be sent to. That ordering is
the point rather than an accident: a hook attaching a credential needs to be able
to tell a call bound for the extension's own control plane from one going anywhere
else, and it can only do that if it sees the final destination.

It cannot call hooks — it runs per request, outside React. Anything it needs from
application state has to be reachable without one: a module-level value, storage,
or something a provider published on its way past.

## Restyling the application

A slot changes what is inside it. A product with its own design language needs
the *application's* components to look different too — its buttons, tables,
inputs and headings, none of which the extension owns.

Overriding design tokens is what achieves that. Every component in this project
reads its colours, radii and fonts from the tokens, so replacing values restyles
components the extension never touches.

```ts
theme: {
  tokens: {
    color: { primary: "#0084c0", primaryHover: "#006ba6" },
    radius: { sm: 2, md: 4, lg: 6 },
    font: { body: "'Open Sans', sans-serif" },
  },
  // The component library's own internals, where a token cannot reach.
  antd: { components: { Button: { controlHeight: 36 } } },
  // Anything neither reaches — gradient borders, scrollbars, resets. Applied
  // after the application's own global styles, so it wins on ties.
  globalStyles: css`
    [data-testid="app-header"] {
      border-bottom: 1px solid transparent;
      border-image: linear-gradient(90deg, #0084c0, #79d4f8) 1;
    }
  `,
  // Fetched before the first render; a font arriving later reflows the page.
  stylesheets: ["https://fonts.googleapis.com/css?family=Open+Sans:300,400,600,700"],
}
```

Token **names** are fixed and a typo is a compile error; token **values** are
not, so any colour or radius is accepted. The spacing scale is deliberately not
overridable — it is a function every component calls, and replacing it would
make layout unpredictable in ways no reviewer could anticipate.

### If the extension renders components from its own library

A component library outside this project reads the Emotion theme in whatever
shape *it* was built against, which is unlikely to be the shape above. This
project nests its tokens (`theme.color.bg`); a library may expect flat keys
(`theme.background`). Nothing warns you: a library shipping no Emotion module
declaration leaves `Theme` widened only by this project, so TypeScript accepts
`theme.background`, and at runtime every colour resolves to `undefined` — the
components render, unstyled or invisibly low-contrast, with no error anywhere.

Supply both shapes by nesting a provider around the library's components rather
than replacing the outer theme:

```tsx
<ThemeProvider theme={(outer) => ({ ...outer, ...myLibraryTheme })}>
```

The extension's own components then still see `theme.color.*`, and the library
sees the keys it expects. Verify it by reading a computed colour off a rendered
library component — not by checking that it mounted, which it will either way.

## Replacing a region of the shell

Contributing a nav item is enough when a product wants its pages listed
alongside the application's. It is not enough when the navigation is a different
*shape* — grouped sections, a collapse control, a logo, a footer — because those
are properties of the sidebar itself and no number of items adds them.

```tsx
shell: {
  Sidebar: MySidebar,  // receives { coreNavItems, extensionNavItems }
  Header: MyHeader,
  Layout: MyLayout,    // replaces the whole shell; takes precedence over both
}
```

`Layout` is for when the shell's *arrangement* differs rather than its regions.
Swapping the sidebar can only produce a variation on this application's
arrangement — header above, sidebar beside. A product whose logo lives in the
sidebar and which has no top bar needs a different arrangement, so it replaces
the layout. **A replacement layout must render React Router's `<Outlet />`**, or
no page appears at all.

## Changing the application's own navigation

Contributing an entry covers "this product has a page the application does not".
This covers the other half: a product that lists the *same* pages differently, or
that supplies its own version of a destination.

```ts
navOverrides: {
  dashboard: { path: "/overview" },   // send a familiar entry somewhere else
  substrate: { hidden: true },        // unlist it — the route still resolves
  mcpServers: { label: "Tool Servers", order: 250 },
}
```

Keys are the application's own nav keys and are type-checked. `hidden` only
unlists an entry — the route still resolves, so a typed URL never 404s because of
a navigation choice.

## Replacing one of the application's routes

A path the application already claims is rejected by default: an accidental
collision should always be an error. A contribution that **declares what it
replaces** is allowed to take that path.

```tsx
routes: [
  { path: "/", element: <ProductOverview />, replaces: "dashboard" },
]
```

The named route is dropped and the contribution serves the path instead. Naming
the route rather than matching its path means the replacement survives a path
change, and that a genuine collision is still caught.

Reach for this only when the destination **belongs to the product** rather than
being a variant of the application's page. Replacing a page the application
maintains means its improvements stop arriving. Adding a point to the page is
almost always better.

## Branding

Identity is not styling, and it should not cost a layout replacement — a product
happy with this application's chrome may still want its own mark on it.

```tsx
branding: {
  AppIcon: MyMark,        // receives { collapsed }; supplied whole, like everything else
  appName: "My Product",  // used for the document title
}
```

A replacement owns the region completely, **including rendering the
application's own navigation** — which is why it is handed `coreNavItems` rather
than keeping a copy that drifts as pages are added.

## App-level providers

Wrapped around the application, outermost first — your query client, feature
flags, telemetry, tenant context:

```ts
providers: [ExampleTenantProvider, ExampleTelemetryProvider],
```

They sit inside the app's own theming, so they can read it, and outside the
router, so they survive navigation.

---

## Adding a new extension point

Points are added by this project, not by extensions — an extension cannot invent
one, because the ID union is the contract.

1. Add the ID to `EXTENSION_POINT_IDS` in `src/appExtensions/extensionPoints.ts`.
2. Add an entry to `EXTENSION_POINT_RENDER_MODE` (`inline` unless clipping demands
   `portal`).
3. If the point passes context, add its shape to `ExtensionPointPropsMap`.
4. Mount it where it belongs: `<ExtensionSlot id="..." />`, plus `context={{ … }}`
   for a context-carrying point.

```tsx
import { ExtensionSlot } from "@/appExtensions";

// contextless — the id alone
<ExtensionSlot id="app_agents_agentsList_pageHeader_actions" />

// per-item context — required, and shape-checked
<ExtensionSlot
  id="app_agents_agentsList_agentListItem_badge"
  context={{ agentName, namespace }}
/>
```

`context` is *conditionally* required: mandatory for points that declare a
contract, and not accepted for points that don't. Omitting it, misspelling the
ID, or passing an unexpected key are all compile errors rather than runtime
surprises.

Changing a point's context shape is a one-line edit to the props map; the
compiler then lists every site that needs updating.

### Slots are safe to leave mounted

A slot with nothing configured renders `null` — no component, no wrapper element,
no whitespace. It is also safe with no provider above it, because the context
defaults to the empty install rather than throwing, so page unit tests need no
setup. Mount points permanently and let configuration decide.

---

## Validation

The whole install is validated at startup and fails loudly rather than degrading
quietly. `AppExtensionConfigError` is raised for, within one extension:

- a slot naming an unknown extension point
- a form field targeting an unknown form ID
- a nav item key declared twice
- a contributed route colliding with a core route, or declared twice

and for the collisions only the install as a whole can see:

- the same extension installed twice
- two extensions contributing the same route path
- two extensions contributing the same nav item key

Two configs can each be perfectly valid and still be impossible to install
together, and every one of those resolves silently and arbitrarily at runtime —
the router takes the first match, React warns about the duplicate key and carries
on. Turning them into a boot error is the point.

Every problem is collected before throwing, across every extension and prefixed
with the id it came from, so one boot reports the whole list instead of making you
fix them one restart at a time.

A silent no-op is the worst outcome here: an extension author cannot tell the
difference between "my component is not mounted" and "I named the point wrong."
The slot check is the load-bearing one — TypeScript already rejects an unknown
point in typed config, but a config deserialised from JSON has no compiler in
front of it.

---

## Testing an extension

- **Both states.** Verify your application with the extension installed *and*
  with nothing installed. The uninstalled path is what a default build ships, and
  it is easy to break by accident.
- **Assert the mechanism, not the copy.** `ExtensionSlot` emits a
  `extension-slot-<id>` test id, so a test can assert that a component mounted at a
  point without depending on what it renders.
- **Watch out for identical contributions.** If a per-item contribution renders
  the same text for every row, a text assertion passes even when the per-item
  context is broken. Assert something that actually varies per item.
- **Fail on console errors.** A component can satisfy every assertion while
  throwing in an effect. The e2e suite's shared fixture fails any test where the
  application logged an error; keep that habit.

---

## Deliberate limitations

- **No automatic conflict resolution.** Several extensions install together, but
  only where the composition rule is unambiguous — concatenate, or later wins.
  Anything genuinely contended (two extensions claiming a route path or a nav key)
  is a startup error, not something the framework picks a winner for.
- **No runtime installation.** The array is resolved at build time. There is no
  discovery, no plugin registry and no loading an extension from a URL; installing
  one is an edit and a rebuild.
- **No partial configuration.** Discussed [above](#one-opinion-worth-stating-up-front).
- **Points exist only where the application declares them.** If you need one that
  isn't there, add it upstream — that keeps the set of extension points a
  reviewed, documented surface rather than an accident of what happened to be
  reachable.
