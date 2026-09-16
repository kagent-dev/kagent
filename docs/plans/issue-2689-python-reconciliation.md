# Issue #2689 Python Reconciliation

## Purpose

This document is the closing ledger for the Python API v2 audit in #2689. It is
intentionally separate from the narrow documentation and startup PRs so that
the final conclusion is based on their merged state rather than a partial
branch.

It does not declare #2689 complete. A retained item is supported only after its
execution path, documentation, and API v2 behavior satisfy the gates below.

## Inventory

| Kind | Item | Current reconciliation status |
| --- | --- | --- |
| Package | `agentsts-adk` | Requires API v2 audit and documented support decision. |
| Package | `agentsts-core` | Requires API v2 audit and documented support decision. |
| Package | `kagent-adk` | Requires API v2 audit; the `adk/basic` image command must be exercised against its packaged filesystem layout. |
| Package | `kagent-core` | Requires API v2 audit and documented support decision. |
| Package | `kagent-crewai` | README contains unverified Kagent-backed memory and Flow-persistence claims that must be corrected or the backing behavior restored. |
| Package | `kagent-langgraph` | Documentation correction is owned by #2821; runtime support still requires audit evidence. |
| Package | `kagent-openai` | Documentation correction is owned by #2821; runtime support still requires audit evidence. |
| Package | `kagent-proto` | Requires API v2 audit and documented support decision. |
| Package | `kagent-skills` | Requires API v2 audit and documented support decision. |
| Sample | `adk/basic` | Startup slice is owned by #2832; validate the built image's agent-loader path before retaining it. |
| Sample | `crewai/poem_flow` | Startup slice is owned by #2834; requires supported-path and documentation audit. |
| Sample | `crewai/research-crew` | Startup slice is owned by #2834; requires supported-path and documentation audit. |
| Sample | `langgraph/currency` | Documentation and startup slices are owned by #2821 and #2829; requires end-to-end A2A, deployment, and checkpoint-restart validation. |
| Sample | `langgraph/hitl-tools` | Startup slice is owned by #2831; requires supported-path and documentation audit. |
| Sample | `langgraph/kebab` | Startup slice is owned by #2831; requires supported-path and documentation audit. |
| Sample | `openai/basic_agent` | Startup slice is owned by #2830; requires supported-path and documentation audit. |

## Reconciliation Gates

1. Rebase this work after the preceding #2689 slices merge. Do not duplicate
   files owned by those PRs unless their merged state still needs correction.
2. For every retained sample, record one API v2 execution path and validate the
   guarantees its README and deployment material claim. A mocked Uvicorn call
   and a health response alone do not prove container startup, A2A execution,
   deployment, or durable restart behavior.
3. Correct or remove every package/sample statement that promises deleted
   Kagent-owned session persistence, removed REST services, or unsupported
   legacy Agent APIs. Keep framework-local session terminology only where the
   source and public API demonstrate that it remains valid.
4. Run and retain the final classified search result:

   ```bash
   rg -n -i 'KAgentSession|_session_service|session service|controller.*8083|agent.*sessions' python --glob '*.py' --glob '*.md' --glob '*.json' --glob '*.yaml' --glob '*.yml'
   ```

   Each remaining match must be either removed or listed as explicitly
   framework-local or historical material with source evidence.
5. Add a focused automated guard for the forbidden legacy surface, with
   documented exclusions for the classified valid matches. Run that guard and
   the affected Python tests in CI.

## Known Blockers Before Closure

- #2821's Currency README explicitly states that it has no documented
  end-to-end runtime command yet.
- The #2832 image copies the `basic` agent directory to `/app/basic`, while
  `kagent-adk run basic --working-dir /app/basic` resolves the agent beneath
  `/app/basic/basic`.
- `kagent-crewai` currently documents Kagent-backed session memory and Flow
  state persistence without evidence of a current backing API.

## Completion Record

The reconciliation PR may close #2689 only after every inventory row has a
recorded disposition, all retained samples satisfy the execution/documentation
gates, and the final classified search plus CI guard pass on the merged base.