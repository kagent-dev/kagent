import { Tag, Tooltip } from "antd";
import { agentNotReadyReason, agentRevisionState, type Agent } from "@/api";

const APPEARANCE = {
  ready: { label: "Ready", color: "success" },
  updating: { label: "Updating", color: "processing" },
  updateFailed: { label: "Update failed", color: "warning" },
  preparing: { label: "Preparing", color: "processing" },
  failed: { label: "Failed", color: "error" },
  notReported: { label: "Not reported", color: "default" },
} as const;

/** The agent's status, with the controller's reason on hover. */
export function AgentStatusTag({ agent, testId }: { agent: Agent; testId?: string }) {
  const state = agentRevisionState(agent);
  const { label, color } = APPEARANCE[state];
  const reason = agentNotReadyReason(agent);
  const explanation = {
    ready: agent.resource.status?.latestSuccessfulRevision,
    updating: "The controller is preparing the latest change. Conversations use the last good revision until then.",
    updateFailed: `${reason ?? "The latest change failed."} Conversations still use the last good revision.`,
    preparing: reason ?? "A revision is desired and none has succeeded yet.",
    failed: reason,
    notReported: "The controller has not reported a revision for this agent.",
  }[state];

  return (
    <Tooltip title={explanation}>
      <Tag color={color} data-testid={testId} data-revision-state={state} css={{ marginInlineEnd: 0 }}>
        {label}
      </Tag>
    </Tooltip>
  );
}
