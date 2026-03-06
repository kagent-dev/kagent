# Requirements

<!-- Q&A record for requirements clarification -->

## Q1: Plugin approach — native page or CRD-driven plugin?

**Q:** Should the workflows page be a native UI page or use the KAgent plugin system (like Kanban MCP)?

**A:** Use the KAgent plugin system — same as Kanban MCP. Navigation and menu managed by CRD (RemoteMCPServer).

## Q2: Stock Temporal UI vs. custom kagent-aware plugin?

**Q:** Should we (A) rely on the existing stock Temporal UI plugin, (B) build a custom kagent-aware workflows plugin, or (C) something else?

**A:** (B) — Build a custom workflows plugin that's kagent-aware, showing agent names, session links, status. The stock Temporal UI plugin is a temporary placeholder until the custom plugin replaces it.

## Q3: Relationship to stock Temporal UI

**Q:** What happens to the stock Temporal UI once the custom plugin is built?

**A:** Stock UI is just there until we have the custom replacement. It will be superseded.

## Q4: What does the custom plugin provide?

**Q:** What should the custom workflows plugin display and what capabilities should it have?

**A:** It's an MCP server (like Kanban MCP) with:
- **MCP tools** for Temporal workflow administration (AI agents can query/manage workflows)
- **Embedded UI** showing workflow list with status filters: running, completed, failed
- **Workflow detail** view for a specific workflow
- Replaces the stock Temporal UI plugin in the sidebar

## Q5: MCP tools for v1?

**Q:** Which MCP tools should the plugin expose?

**A:** All of these for v1:
- `list_workflows` — filter by status (running/completed/failed), agent name, time range
- `get_workflow` — detail for a specific workflow (history, activities)
- `cancel_workflow` — terminate a running workflow
- `signal_workflow` — send signals (e.g., HITL approval)

## Q6: Navigation placement

**Q:** Where does each thing go in the sidebar?

**A:**
- **AGENTS/Workflows** — keep this nav item, but point it to the stock Temporal MCP UI (replace the stub page)
- **PLUGINS/temporal-workflows** — the new custom MCP server plugin with tools + embedded UI

## Q7: Embedded UI approach

**Q:** Same pattern as Kanban MCP for the embedded UI?

**A:** Yes — single embedded HTML file, vanilla JS, no build step, SSE for live workflow status updates. Implements kagent plugin bridge protocol.

## Q8: Data flow

**Q:** Where does the custom plugin get workflow data?

**A:** UI (embedded HTML) → Temporal MCP server (Go binary, REST API) → Temporal Server (gRPC :7233). The MCP server connects directly to Temporal, not through kagent backend.

## Q9: Plugin state

**Q:** Does the plugin need local storage (SQLite etc.) for its own state?

**A:** No — stateless. Purely proxies to Temporal server. No local DB needed.

## Q10: Code location

**Q:** Where does the plugin code and Helm chart live?

**A:** `go/plugins/temporal-mcp/` for the Go binary (same as kanban-mcp), `helm/tools/temporal-mcp/` for the Helm chart.

## Q11: Workflow list columns

**Q:** What info per workflow row in the UI?

**A:** Workflow ID, agent name (parsed from ID), status badge (running/completed/failed/canceled), start time, duration. Clickable to expand/drill into activity detail.

## Q12: Temporal namespace and connection

**Q:** Configurable namespace and Temporal server address?

**A:** Namespace hardcoded to "kagent". Temporal server address via env var (e.g., `TEMPORAL_HOST_PORT`).
