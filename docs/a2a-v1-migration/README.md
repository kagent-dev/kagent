# A2A v1 Migration

This directory contains documentation for migrating from the legacy `trpc-a2a-go` protocol to the official A2A v1 protocol.

## Documents

| Document | Description |
|----------|-------------|
| [Migration Guide](./migration-guide.md) | **User-facing guide** with step-by-step upgrade instructions |
| [Migration Plan](./a2a-migration-plan.md) | Internal technical plan for the migration implementation |

## Quick Links

- [What's Changing](./migration-guide.md#whats-changing)
- [Upgrade Steps](./migration-guide.md#upgrade-steps)
- [Custom Clients](./migration-guide.md#for-users-with-custom-a2a-clients-or-ui)
- [FAQ](./migration-guide.md#faq)

## Summary

The migration from legacy A2A to A2A v1 follows this path:

```
Current → 0.10.0 → 0.11.0 → (migrate data) → 0.12.0
```

1. **0.10.0**: Controller learns to read both formats (bridge release)
2. **0.11.0**: Controller starts writing v1 format; requires historical data migration
3. **0.12.0**: Legacy support removed; v1 only

For detailed instructions, see the [Migration Guide](./migration-guide.md).
