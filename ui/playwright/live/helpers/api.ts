import { createClient } from "@connectrpc/connect";
import { createGrpcWebTransport } from "@connectrpc/connect-web";
import { AgentService } from "../../../src/generated/kagent/api/v1alpha1/agents_pb";
import { AgentInstanceService } from "../../../src/generated/kagent/api/v1alpha1/agent_instances_pb";
import { HarnessService } from "../../../src/generated/kagent/api/v1alpha1/harnesses_pb";

/** The controller's gRPC API through the dev server's `/api` proxy, for setup reads and teardown. */
export function liveApi(baseURL: string) {
  const transport = createGrpcWebTransport({ baseUrl: `${baseURL}/api` });
  const agents = createClient(AgentService, transport);
  const instances = createClient(AgentInstanceService, transport);
  const harnesses = createClient(HarnessService, transport);

  return {
    /** A harness spec, so inline harnesses reuse the cluster's image, pool and snapshot store. */
    async harnessSpec(namespace: string, name: string) {
      const { harnesses: rows } = await harnesses.listHarnesses({ namespace });
      const row = rows.find((entry) => entry.ref?.name === name);
      const spec = (row?.resource?.value as { spec?: HarnessSpecShape } | undefined)?.spec;
      if (!spec) throw new Error(`Harness ${namespace}/${name} not found`);
      return spec;
    },

    /** The Agent's last successful revision, as the controller reports it. */
    async latestRevision(namespace: string, name: string): Promise<string | undefined> {
      const { agent } = await agents.getAgent({ ref: { namespace, name } });
      return (agent?.resource?.value as { status?: { latestSuccessfulRevision?: string } } | undefined)?.status
        ?.latestSuccessfulRevision;
    },

    /** Deletes an Agent and every conversation started from it. Missing is fine. */
    async removeAgent(namespace: string, name: string) {
      const { agentInstances } = await instances.listAgentInstances({ allCreators: true, agent: { namespace, name } });
      for (const instance of agentInstances) {
        await instances.deleteAgentInstance({ agentInstanceId: instance.id }).catch(() => {});
      }
      await agents.deleteAgent({ ref: { namespace, name } }).catch(() => {});
    },
  };
}

export interface HarnessSpecShape {
  workload: { image: string };
  substrate: { workerPoolRef: { name: string }; snapshotPolicy: { location: string } };
}
