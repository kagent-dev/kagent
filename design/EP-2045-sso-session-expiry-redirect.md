# EP-2045: SSO session-expiry re-authentication redirect

* Issue: [#2045](https://github.com/kagent-dev/kagent/issues/2045)

## Background

kagent is frequently deployed behind [oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy)
acting as an OIDC relying party. oauth2-proxy authenticates the user, maintains
its own session cookie, and forwards the user's `id_token` to the kagent UI as a
`Authorization: Bearer <jwt>` header. The UI decodes the JWT to derive the current
user (`getAuthResult`).

Two distinct failure modes were conflated in the previous implementation, which
decoded the token and returned `CurrentUser | null`:

1. **No proxy in front of the UI** — there is no `Authorization` header at all.
   This is a valid "unsecured" deployment (local dev, no SSO).
2. **Proxy session valid but forwarded `id_token` expired** — oauth2-proxy still
   holds a valid session cookie, but the `id_token` it forwards has expired (its
   lifetime is typically shorter than the cookie session). The JWT decodes to an
   expired token.

Both previously collapsed to `null`, so the UI silently rendered a logged-out
experience. In case (2) the correct behavior is to re-run the OIDC flow against
oauth2-proxy (`/oauth2/start`) so a fresh `id_token` is minted, transparently
restoring the session. In case (1) the UI must **not** redirect — there is no
`/oauth2` endpoint, so a redirect would loop forever.

## Motivation

Users behind SSO are unexpectedly "logged out" mid-session when the forwarded
`id_token` expires, even though their oauth2-proxy session is still valid. They
must manually reload to recover. This is confusing and looks like a kagent bug.

### Goals

- Distinguish three auth states in the UI: `authenticated`, `expired`, `unsecured`.
- On `expired`, transparently re-run the OIDC flow (redirect to the oauth2-proxy
  re-auth endpoint) so a fresh token is obtained without user intervention.
- Never redirect in `unsecured` mode (no proxy), to avoid an infinite loop.
- Guard against redirect loops if re-auth keeps returning a stale token.

### Non-Goals

- Changing the server-side token validation or the controller/HTTP server auth.
- Implementing a kagent-native OIDC client (oauth2-proxy remains the RP).
- Refresh-token handling inside the UI (delegated to oauth2-proxy).

## Implementation Details

### Auth status model (`ui/src/app/actions/auth.ts`)

`getAuthResult()` now returns an `AuthResult` instead of `CurrentUser | null`:

```ts
export type AuthStatus = "authenticated" | "expired" | "unsecured";

export interface AuthResult {
  status: AuthStatus;
  user: CurrentUser | null;
}
```

- No `Authorization: Bearer` header → `{ status: "unsecured", user: null }`.
- Header present but token missing/expired (`isTokenExpired`) → `{ status: "expired", user: null }`.
- Valid token → `{ status: "authenticated", user: claims }`.

### Re-auth redirect (`ui/src/contexts/AuthContext.tsx`)

`AuthProvider` exposes `status` alongside `user`. When `status === "expired"`
(and only then) it redirects the browser to the oauth2-proxy re-auth endpoint,
preserving the current location for return:

```ts
const SSO_REAUTH_PATH = process.env.NEXT_PUBLIC_SSO_REAUTH_PATH || "/oauth2/start";
const REAUTH_GUARD_KEY = "kagent_reauth_attempt";
const REAUTH_GUARD_WINDOW_MS = 10_000;
// ...redirect to `${SSO_REAUTH_PATH}?rd=${encodeURIComponent(path + search)}`
```

A `sessionStorage` guard (`REAUTH_GUARD_WINDOW_MS`) prevents a redirect loop: if a
re-auth attempt happened within the window and the token is still expired, the UI
surfaces an error instead of redirecting again. The guard is cleared on a
successful `authenticated` result.

### UI surface

- `ui/src/components/UserMenu.tsx` — reflects the new status (e.g. distinguishes a
  logged-out/unsecured menu from an authenticated one).
- `ui/src/app/login/page.tsx` — the branded login page (a server component) reads
  the server-side `SSO_REDIRECT_PATH`. `AuthContext` (a client component) reads the
  client-exposed `NEXT_PUBLIC_SSO_REDIRECT_PATH`; both default to `/oauth2/start`
  and the Helm chart injects both from `ui.auth.ssoRedirectPath`.
- `docs/OIDC_PROXY_AUTH_ARCHITECTURE.md` — documents the three states and the
  re-auth flow.

### Configuration

| Env var | Default | Purpose |
|---------|---------|---------|
| `NEXT_PUBLIC_SSO_REAUTH_PATH` | `/oauth2/start` | oauth2-proxy endpoint that restarts the OIDC flow. |

## Test Plan

- **Unit (UI):** `getAuthResult` returns the correct `AuthStatus` for: no header,
  expired token, valid token. `AuthProvider` redirects only on `expired`, never on
  `unsecured`, and respects the loop guard.
- **Manual / e2e:** Deploy behind oauth2-proxy with a short `id_token` lifetime;
  confirm that on token expiry the UI redirects to `/oauth2/start?rd=...` and
  returns to the same page authenticated, without a logout flash. Confirm that a
  no-proxy deployment never redirects.

## Alternatives

- **Silent background refresh via fetch to `/oauth2/auth`** — more complex, and
  oauth2-proxy already mints a fresh token on a full `/oauth2/start` round-trip.
- **Server-side redirect (middleware)** — harder to distinguish unsecured vs
  expired without the decoded claims available client-side, and risks loops.

## Open Questions

- Should the re-auth guard window be configurable per deployment?
- Should `expired` optionally render a non-blocking toast ("re-authenticating…")
  before the redirect for slow networks?
