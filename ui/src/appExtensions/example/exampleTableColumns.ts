import { defineExtensionTableColumn } from "@/appExtensions";
import type { AgentInstance } from "@/api";

/**
 * A column the application has no notion of.
 *
 * The point of a column contribution rather than a slot: this is a heading, a
 * per-row value and a position in the ordering, and a table cannot lay out
 * without all three declared together.
 */
export const exampleAgentRegionColumn = defineExtensionTableColumn<AgentInstance>({
  id: "exampleRegion",
  tableId: "app_agents_agentsList_table",
  title: "Example region",
  after: "namespace",
  // An instance's labels, not a resource's annotations: an AgentInstance is a row in
  // the controller's database rather than a custom resource, so `labels` is the only
  // place a deployment can hang its own facts.
  render: (row) => row.labels["example.com/region"] ?? "unassigned",
});
