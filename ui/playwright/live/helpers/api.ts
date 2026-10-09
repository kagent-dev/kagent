import { createClient } from "@connectrpc/connect";
import { createGrpcWebTransport } from "@connectrpc/connect-web";
import { AgentService } from "../../../src/generated/kagent/api/v1alpha1/agents_pb";
import { SessionService } from "../../../src/generated/kagent/api/v1alpha1/sessions_pb";

/** The controller's gRPC API through the dev server's `/api` proxy, for setup reads and teardown. */
export function liveApi(baseURL: string) {
  const transport = createGrpcWebTransport({ baseUrl: `${baseURL}/api` });
  const agents = createClient(AgentService, transport);
  const sessions = createClient(SessionService, transport);

  return {
    /** The Agent's last successful revision, as the controller reports it. */
    async latestRevision(namespace: string, name: string): Promise<string | undefined> {
      const { agent } = await agents.getAgent({ ref: { namespace, name } });
      return (agent?.resource?.value as { status?: { latestSuccessfulRevision?: string } } | undefined)?.status
        ?.latestSuccessfulRevision;
    },

    /** Deletes an Agent and every conversation started from it. Missing is fine. */
    async removeAgent(namespace: string, name: string) {
      const { sessions: rows } = await sessions.listSessions({ allCreators: true, agent: { namespace, name } });
      for (const session of rows) {
        await sessions.deleteSession({ sessionId: session.id }).catch(() => {});
      }
      await agents.deleteAgent({ ref: { namespace, name } }).catch(() => {});
    },
  };
}
