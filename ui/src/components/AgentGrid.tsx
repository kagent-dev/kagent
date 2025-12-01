import type { AgentResponse } from "@/types";
import { AgentCard } from "./AgentCard";
import { k8sRefUtils } from "@/lib/k8sUtils";

interface AgentGridProps {
  agentResponse: AgentResponse[];
}

export function AgentGrid({ agentResponse }: AgentGridProps) {

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
      {agentResponse.map((item) => {
        const agentRef = k8sRefUtils.toRef(
          item.agent.metadata.namespace || '',
          item.agent.metadata.name || '');

        return <AgentCard key={agentRef} agentResponse={item} />
      })}
    </div>
  );
}