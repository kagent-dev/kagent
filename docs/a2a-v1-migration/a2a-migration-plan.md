# A2A v1 Migration Plan

## Goal

Move kagent from the current `trpc-a2a-go` / A2A 0.3-era protocol shape to official A2A v1 without requiring a maintenance window.

The final target state is:

- Go ADK, Python ADK, UI, and `go/core` speak official A2A v1 on the wire.
- New `task` and `push_notification` rows are stored as official A2A v1 JSON.
- Existing legacy rows remain readable during the migration.
- Users upgrade normally across releases (`0.10.0` → `0.11.0` → migrate on `0.11.0` → `0.12.0`) instead of requiring a maintenance-window storage flip.

## Version Signals

The migration uses two separate version signals:

- **Wire version:** selected by request metadata.
  - Missing `A2A-Version` header means legacy/current kagent A2A behavior.
  - `A2A-Version: 1.0` means official A2A v1 behavior.
  - Unknown versions fail with a clear unsupported-version error.
- **Storage version:** selected by DB row metadata.
  - `protocol_version IS NULL` or legacy value means stored `trpc-a2a-go` JSON.
  - `protocol_version = "1.0"` means stored official A2A v1 JSON.

This keeps wire compatibility independent from persisted-data migration.

## Required Upgrade Path

The supported zero-downtime path is:

```text
0.9.x -> 0.10.0 -> 0.11.0 -> (run migrate a2a-v1) -> 0.12.0
```

Direct upgrades from `0.9.x` to `0.11.0` or `0.12.0` should be rejected or documented as unsupported unless the installation first passes through the `0.10.0` bridge release.

Before upgrading to `0.12.0`, every installation that upgraded from a prior kagent release must run the historical storage migration CLI while still on `0.11.0` (see [Optional Historical Migration](#optional-historical-migration)). Fresh installs of `0.11.0` or later that never had legacy rows may skip the CLI.

## Release 0.10.0: Bridge Release

`0.10.0` makes every running controller capable of understanding both legacy and v1 data before any v1 storage writes begin.

High-level behavior:

- Controller can read legacy and v1 `task` / `push_notification` rows.
- Controller always writes legacy `trpc-a2a-go` storage.
- Controller uses official A2A SDK compatibility for public A2A wire handling where possible; it should not add custom JSON-RPC decoding for official v0.3 traffic.
- Controller serves missing-header or v0.3 callers with legacy A2A wire responses.
- Controller serves `A2A-Version: 1.0` callers with v1 wire responses.
- Controller continues selecting A2A `0.3` interfaces when proxying to managed agents.
- First-party UI and managed agent runtimes stay on the existing legacy/v0.3 behavior in this release.
- Controller can convert in both directions as needed:
  - legacy storage -> v1 wire,
  - legacy storage -> legacy wire,
  - v1 storage -> v1 wire,
  - v1 storage -> legacy wire.
- AgentCards advertise both legacy and v1 interfaces, preferably with the same URL and different `protocolVersion` values.
- CORS, proxies, and gRPC metadata preserve `A2A-Version`.

Why this is safe:

- Old controller pods may exist during rollout.
- New controller pods still write legacy storage.
- Therefore old controller pods never need to read rows written in v1 format.

## Release 0.11.0: v1 Write Release

`0.11.0` assumes the installation already passed through `0.10.0`, so all controllers that may read new rows are compatibility-capable. Alternatively, you can do a fresh install of `0.11.0` if you do not have kagent running already.

High-level behavior:

- Controller still dual-reads legacy and v1 rows.
- Controller writes new `task` and `push_notification` rows as official A2A v1 JSON.
- New v1 rows get `protocol_version = "1.0"`.
- UI moves to the A2A v1 SDK/types and sends/selects `protocolVersion: 1.0` / `A2A-Version: 1.0`.
- Managed Go and Python agent runtimes move to v1 interfaces.
- Controller switches upstream managed-agent client selection from A2A `0.3` interfaces to A2A `1.0` interfaces.
- Legacy wire compatibility remains available for missing-header callers through this release; it is removed in `0.12.0`.
- Historical legacy rows do not need to be rewritten to serve traffic on `0.11.0`, but must be migrated via `kagent migrate a2a-v1` before upgrading to `0.12.0`.

Why this is safe:

- Any controller remaining after the `0.11.0` upgrade has the dual-read compatibility introduced in `0.10.0`.
- New v1 writes are readable by all supported controllers in this upgrade path.
- Existing legacy rows continue to be converted on read until `0.12.0`.

## Optional Historical Migration

Historical row migration is not required to serve traffic on `0.11.0`, but it **is required** before upgrading to `0.12.0` for any installation that still has legacy `task` or `push_notification` rows. Run it while still on `0.11.0`:

```bash
kagent migrate a2a-v1 --dry-run
kagent migrate a2a-v1
```

The command converts legacy `task` and `push_notification` rows to official A2A v1 JSON and sets `protocol_version = "1.0"`.

It should be:

- batch-based,
- idempotent,
- restartable,
- safe against concurrent row changes,
- explicit about migrated/skipped/failed counts.

The controller keeps dual-read compatibility through `0.11.0` so traffic continues while the CLI runs. `0.12.0` removes that compatibility; do not upgrade until migrated-row count is zero (or the installation never had legacy rows).

## Component Changes

### Controller / Core

- Add nullable `protocol_version` columns for `task` and `push_notification`.
- Centralize conversion between legacy `trpc-a2a-go` data and official A2A v1 types.
- Use official A2A SDK compatibility for official v0.3/v1 wire handling where possible.
- Negotiate wire format from AgentCard interface selection and `A2A-Version`.
- Select storage parser from `protocol_version`.
- In `0.10.0`, write legacy storage only.
- In `0.10.0`, continue selecting managed-agent A2A `0.3` interfaces.
- In `0.11.0`, write v1 storage by default.
- In `0.11.0`, switch managed-agent interface selection to A2A `1.0`.
- Keep dual-read compatibility through `0.11.0`; remove legacy storage parsers and dual-read in `0.12.0`.
- In `0.12.0`, remove legacy wire handling and `trpc-a2a-go` dependencies from `go/core` (see [Release 0.12.0](#release-0120-cleanup-release)).

### UI

- Stay on legacy/v0.3 behavior in `0.10.0`.
- Move to the A2A v1 SDK/types in `0.11.0`.
- Send/select `protocolVersion: 1.0` / `A2A-Version: 1.0` in `0.11.0`.
- Consume v1 task/message/event shapes in `0.11.0`.
- Rely on the controller for legacy persisted-data compatibility through `0.11.0` only.

### Go And Python Runtimes

- Stay on legacy/v0.3 behavior in `0.10.0`.
- Move runtime A2A servers/clients to official A2A v1 in `0.11.0`.
- In `0.12.0`, drop legacy/v0.3 wire paths; v1 only.
- Preserve kagent behavior for HITL, ask-user, tool calls, subagent activity, usage metadata, tracing, and session IDs.
- Avoid runtime-specific compatibility with historical DB formats; that belongs in `go/core`.

## Release 0.12.0: Cleanup Release

`0.12.0` assumes the installation already passed through `0.11.0` and, for any upgrade from a prior release, that `kagent migrate a2a-v1` was run on `0.11.0` so no legacy `task` or `push_notification` rows remain (`protocol_version IS NULL` count is zero). Fresh installs of `0.11.0` or later with no legacy history may upgrade directly.

High-level behavior:

- Controller reads and writes official A2A v1 storage only; legacy `trpc-a2a-go` parsers and dual-read paths are removed.
- Legacy wire compatibility for missing-header or A2A `0.3` callers is removed (or reduced to an explicit opt-in compatibility flag if product support still requires it).
- AgentCards and managed-agent client selection use A2A `1.0` interfaces only.
- `trpc-a2a-go` runtime dependencies are removed from `go/core` where no longer needed for serving.
- `protocol_version` remains the persisted storage format marker (`"1.0"`).

Why this is safe:

- `0.11.0` introduced v1 writes and dual-read so all controllers in the supported path can read v1 rows.
- Requiring `kagent migrate a2a-v1` on `0.11.0` ensures historical legacy rows are rewritten before `0.12.0` drops legacy storage support.
- One release (`0.11.0`) with dual-read gives operators time to run the CLI without a maintenance window.

## Alternatives Considered

1. Deploying v1 agents and UI alongside the new controller in 0.10.0 release

This would reduce some compatibility code and simplify some `go/core` changes, but this would not be strictly zero-downtime since the new UI cannot talk to the old controller. Similarly, if there are multiple controller instances, new instances will start upgrading agents to v1 and it will fail to talk to old controllers.

2. Start writing v1 data in 0.10.0 release

This does not work because if there are multiple controller instances, old instances will crash if there are v1 data in the database. We must wait until all instances have been upgraded to the compatible code, which is in the next release 0.11.0.

3. Simple migration with a maintenance window

Would be simpler (just a data migration script + direct changes to v1 code in agent, UI, controller) but would not be zero-downtime.