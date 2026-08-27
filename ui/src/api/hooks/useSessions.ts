import { apiClient } from "../client";
import type { Session } from "../domain/sessions";
import { type ApiResource, useApiResource } from "./useApiResource";

/** Conversations belonging to one agent. Holds off until the ref is known. */
export function useAgentSessions(
  namespace: string | undefined,
  name: string | undefined,
): ApiResource<Session[]> {
  return useApiResource(
    namespace && name ? ["sessions.listForAgent", namespace, name] : null,
    () => apiClient.sessions.listForAgent(namespace!, name!),
  );
}

/** One conversation's record. Its messages come from the chat client, not here. */
export function useSession(id: string | undefined): ApiResource<Session> {
  return useApiResource(id ? ["sessions.get", id] : null, () =>
    apiClient.sessions.get(id!),
  );
}
