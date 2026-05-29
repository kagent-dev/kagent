# Migrating to A2A v1 Protocol

## Overview

This guide explains how to migrate your kagent installation from the legacy A2A protocol (`trpc-a2a-go`) to the official A2A v1 protocol. The migration is designed to be **zero-downtime** and allows you to upgrade your agents, UI, and custom clients incrementally.

## What's Changing

| Before | After |
|--------|-------|
| Legacy `trpc-a2a-go` protocol | Official A2A v1 protocol |
| Legacy storage format | Official A2A v1 JSON format |
| Missing `A2A-Version` header | Required `A2A-Version: 1.0` header |
| Protocol version not tracked | Explicit `protocol_version` field in storage |

### Benefits of A2A v1

- **Standardized protocol**: Uses the official Google A2A specification
- **Better interoperability**: Works with standard A2A clients and tools
- **Future-proof**: Aligns with the evolving A2A ecosystem
- **Improved wire format**: Cleaner JSON structure with explicit versioning

## Migration Timeline

The migration follows a phased release approach:

```
Current → 0.10.0 → 0.11.0 → (run migration) → 0.12.0
```

| Release | What Happens | Your Action Required |
|---------|--------------|---------------------|
| **0.10.0** | Controller learns to read both legacy and v1 formats | Upgrade normally |
| **0.11.0** | Controller starts writing v1 format; UI and runtimes switch to v1 | Upgrade normally |
| **0.11.0** (post-upgrade) | Historical data migration | **Run CLI command** |
| **0.12.0** | Legacy support removed; v1 only | Upgrade after migration complete |

## Upgrade Steps

### Step 1: Upgrade to 0.10.0 (Bridge Release)

This release prepares your system for the v1 migration:

```bash
# Upgrade to 0.10.0
helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds  --version 0.10.0
helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent --version 0.10.0
```

**What to expect:**
- Controller can now read both legacy and v1 data formats
- All writes still use legacy format
- UI and agent runtimes remain on legacy behavior
- No breaking changes to your agents or clients

**Validation:**
```bash
# Check all pods are running
kubectl get pods -n kagent

# Verify controller logs show successful startup
kubectl logs -n kagent deployment/kagent-controller
```

### Step 2: Upgrade to 0.11.0 (v1 Write Release)

This release switches to v1 for all new data:

```bash
# Upgrade to 0.11.0
helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds  --version 0.11.0
helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent  --version 0.11.0
```

**What to expect:**
- All new tasks and notifications are stored in A2A v1 format
- UI switches to v1 protocol (`A2A-Version: 1.0`)
- Managed agent runtimes use v1 interfaces
- Legacy data is still readable through conversion

**Validation:**
```bash
# Verify UI is accessible and functional
kubectl port-forward -n kagent svc/kagent-ui 3000:8080

# Chat with agents with CLI
kagent invoke --agent "your-agent" --task "hi"
```

### Step 3: Migrate Historical Data (Required before 0.12.0)

Before upgrading to 0.12.0, you **must** migrate any existing legacy data:

```bash
# First, do a dry run to see what will be migrated
kagent migrate a2a-v1 --dry-run

# Run the actual migration
kagent migrate a2a-v1

# You can set a custom connection string for db
kubectl port-forward svc/kagent-postgres 5432:5432
kagent migrate a2a-v1 --postgres-database-url "postgres://kagent@localhost:5432/kagent?sslmode=disable"
```

**Migration characteristics:**
- **Batch-based**: Processes data in chunks
- **Idempotent**: Safe to run multiple times
- **Restartable**: Can be interrupted and resumed
- **Concurrent-safe**: Won't interfere with ongoing operations

**Monitor progress:**
The command will report counts of migrated, skipped, and failed rows. Keep running it until the "remaining legacy rows" count is zero.

**Validation:**
```bash
# Check migration status
kagent migrate a2a-v1 --status

# Or query the database directly (if you have access)
# Look for tasks with protocol_version IS NULL or not equal to "1.0"
```

### Step 4: Upgrade to 0.12.0 (Cleanup Release)

Once historical migration is complete:

```bash
# Upgrade to 0.12.0
helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds  --version 0.12.0
helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent  --version 0.12.0
```

**What to expect:**
- Legacy storage parsers removed
- Legacy wire handling removed (unless explicitly enabled)
- All traffic uses A2A v1 exclusively
- `trpc-a2a-go` dependencies removed from controller

## Fresh Installations

If you're doing a fresh install of kagent 0.11.0 or later (no existing data):

1. **Install 0.11.0 or later directly**
2. **No data migration required** (no legacy rows exist)
3. **Skip Step 3** above and proceed directly to 0.12.0 when ready

## For Users with Custom A2A Clients or UI

If you're using your own A2A client implementation or a custom UI:

### Library Update Required

**Swap out your A2A library** from the v0.3 version to v1 version. The following are official migration guides for each SDK:

| A2A SDK | Migration Guide | 
|----------|---------------|
| Python | [a2a-python migration guide](https://github.com/a2aproject/a2a-python/blob/main/docs/migrations/v1_0/README.md) |
| TypeScript | Legacy types | [a2a-js migration guide](https://github.com/a2aproject/a2a-js/blob/v1.0.0-alpha.0/docs/migration-guide.md) |

### Timing for Custom Clients

| When | What to Do |
|------|-----------|
| During 0.10.0 | Start updating your client code to use A2A v1 library |
| During 0.11.0 | Deploy your updated client with `A2A-Version: 1.0` header |
| Before 0.12.0 | Ensure all your clients are updated to v1 |
| 0.12.0+ | Legacy wire support removed; v1 clients only |

### Backwards Compatibility During Migration

**During 0.10.0 and 0.11.0:**
- Missing `A2A-Version` header → Legacy protocol response
- `A2A-Version: 1.0` → v1 protocol response

**After 0.12.0:**
- Missing or legacy version header → Error or opt-in compatibility mode
- `A2A-Version: 1.0` → v1 protocol response only

## FAQ

**Q: Can I rollback from 0.11.0 to 0.10.0?**
A: Yes, but new v1 tasks created in 0.11.0 won't be readable by 0.10.0 controller. If you have active tasks, wait for them to complete before rolling back

**Q: Can I rollback from 0.12.0?**
A: No, once you upgrade to 0.12.0 and the migration is complete, you cannot rollback. Legacy code paths are removed in 0.12.0

**Q: Do I need to update my agents?**
A: No, the controller handles protocol conversion. However, updating to use native A2A v1 improves performance and removes conversion overhead.

**Q: What happens to my existing task history?**
A: All historical tasks are preserved. The migration command converts them to the new format while keeping all data intact.

**Q: Can I skip 0.10.0 and go directly to 0.11.0?**
A: Only if doing a fresh install. For upgrades, you must pass through 0.10.0 to ensure all controllers can handle v1 data.

**Q: Do I need to run the migration command on fresh installs?**
A: No, only if upgrading from a version prior to 0.11.0.

**Q: Will my HITL (Human-in-the-Loop) workflows still work?**
A: Yes, HITL, ask-user, tool calls, subagent activity, and tracing are all preserved with the same behavior in A2A v1.

---

**Migration Checklist:**

- [ ] Upgraded to 0.10.0
- [ ] Upgraded to 0.11.0
- [ ] Ran `kagent migrate a2a-v1` (dry-run first)
- [ ] Verified migration completed (no legacy rows remaining)
- [ ] Updated custom clients to A2A v1 (if applicable)
- [ ] Upgraded to 0.12.0
- [ ] Verified all agents and clients working
