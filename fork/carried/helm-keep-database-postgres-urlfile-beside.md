---
id: helm-keep-database-postgres-urlfile-beside
fork: kagent
theme: compat-migrations
category: compat
status: carried
upstream:
  outcome: fork-only
  refs: [kagent-dev/kagent#3160]
  exit: agent-platform#977
depends_on: []
commit: e01a9d960
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(helm): keep database.postgres.urlFile beside connectionStringSecretRef` | upstream's re-pinned chart (kagent-dev/kagent#2708) removed `database.postgres.urlFile` and reads the connection string only from the Secret of `connectionStringSecretRef`, assuming `role` (default `kagent_owner`) on every connection; the agent-platform meta chart hands kagent its CloudNativePG app Secret's `uri` as a mounted file through `urlFile`, so the line's controller exited with "KAGENT_POSTGRES_DATABASE_URL is required" and the upgrade, and the rollback past a moved Substrate, failed (giantswarm/kagent-upstream#464). With `urlFile` set the chart renders `KAGENT_POSTGRES_DATABASE_URL_FILE` and no Secret reference, the controller reads the URL from that file (it wins over `KAGENT_POSTGRES_DATABASE_URL`; an unreadable or empty file is an error, never a fall back), and an unset `role` assumes none, since the URL's user owns the tables as before the re-pin; an explicit `role` (or `""`) applies in both paths. Proven by the chart's render tests of both value shapes and the controller's unit test of the file path. Retired once giantswarm/agent-platform#977 (the meta chart wiring `connectionStringSecretRef` and `role`) has rolled out everywhere | fork-only: upstream removed `urlFile` on purpose for the Secret reference; a compatibility carry for this line's consumer |
