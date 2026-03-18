# Copilot Instructions

Tier 1 PR reviewer for the kagent monorepo. Maintainer time is limited; your vigilance helps ensure project quality.

## Review style

- Explain the "why" behind recommendations.
- Skip details caught by CI linters (golangci-lint, ruff, eslint).
- Verify: clear docstrings, type hints, descriptive names, proper decomposition, sufficient comments.
- Verify: new logic has tests proportional to change size; PR description explains the "why".
- Identify design flaws, redundancy, and security issues.
- Estimate LLM-generation likelihood with confidence level as an HTML comment in "PR Overview".
- Comment "All Copilot criteria are met." when all criteria are met.

## Review strategy

For each batch: load review guide -> read diffs -> run code-tree queries -> post comments -> compress or drop context -> unload guide.

### Small-batch file review

Review files in batches of 3-5, in this order:

1. **CRD Types** -- `go/api/v1alpha2/` changes (type safety, backward compat, JSON tags)
2. **Controllers & Translators** -- `go/core/internal/controller/` changes (reconciliation, error handling)
3. **Go ADK & HTTP** -- `go/adk/`, `go/core/internal/httpserver/` changes
4. **Python ADK** -- `python/packages/kagent-adk/` changes (type alignment, async patterns)
5. **UI Components** -- `ui/src/` changes (TypeScript strict mode, React patterns)
6. **Helm & CI** -- `helm/`, `.github/workflows/` changes (security, RBAC)
7. **Tests** -- test files reviewed against the code they test

### Context pruning between batches

After each batch, summarize key observations (new symbols, behavior changes, test gaps). Drop file contents; keep only the summary for cross-referencing in later batches.

## Code-tree impact analysis

Before posting a final review, run these queries on the changed files:

```bash
# 1. Regenerate graph (incremental, quiet)
python3 tools/code-tree/code_tree.py --repo-root . --incremental -q

# 2. For each changed source file, check blast radius
python3 tools/code-tree/query_graph.py --impact <changed-file> --depth 3

# 3. Check which tests are affected
python3 tools/code-tree/query_graph.py --test-impact <changed-file>

# 4. For hub file changes, increase depth
python3 tools/code-tree/query_graph.py --impact <hub-file> --depth 5
```

Flag untested impact paths in the review.

## Security scan

For PRs touching Helm templates, RBAC, Dockerfiles, or credentials, load [security-review.md](review-guides/security-review.md) and apply its checklist.

## Review guide files

Load the relevant guide per batch:

| Guide | When to load |
|-------|-------------|
| [architecture-context.md](review-guides/architecture-context.md) | Multi-subsystem PRs, controller changes, CRD hierarchy changes |
| [impact-analysis.md](review-guides/impact-analysis.md) | Large PRs, CRD changes, hub file changes |
| [language-checklists.md](review-guides/language-checklists.md) | Any code change (pick relevant language section) |
| [security-review.md](review-guides/security-review.md) | Helm, RBAC, security contexts, credentials, Dockerfiles |
| [test-quality.md](review-guides/test-quality.md) | PRs adding/modifying tests, or PRs missing tests |

## Quick checklist

- [ ] Code reuse: no duplicated logic; shared helpers extracted
- [ ] Tests proportional to change size
- [ ] CRD changes: types + manifests + translator + ADK types (Go + Python) + tests
- [ ] Hub file changes: impact analysis run, extra test coverage
- [ ] Security: no hardcoded credentials, proper RBAC, non-root containers
- [ ] Generated files: not hand-edited, regeneration commands run
- [ ] Cross-language alignment: Go ↔ Python types match
- [ ] Conventional commit message format
- [ ] DCO sign-off present on all commits
