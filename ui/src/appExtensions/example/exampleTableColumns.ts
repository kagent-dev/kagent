import { defineExtensionTableColumn } from "@/appExtensions";
import type { AgentInstance } from "@/api";

/**
 * A column the application has no notion of.
 *
 * The point of a column contribution rather than a slot: this is a heading, a
 * per-row value and a position in the ordering, and a table cannot lay out
 * without all three declared together.
 */
export const exampleAgentCreatorColumn = defineExtensionTableColumn<AgentInstance>({
  id: "exampleCreator",
  tableId: "app_agents_agentsList_table",
  title: "Example creator",
  after: "namespace",
  render: (row) => row.creator || "Unknown",
});
