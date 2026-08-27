/**
 * Session domain models.
 *
 * A session is one conversation with an agent. Its message history lives behind
 * the chat client port (see `api/chat`), not here — this is only the record.
 */

export interface Session {
  id: string;
  name: string;
  /** `namespace__NS__name` of the agent this conversation belongs to. */
  agent_id: string;
  user_id: string;
  created_at: string;
  updated_at: string;
  deleted_at: string;
  /** Set for sessions owned by another user; sent back as the share token. */
  share_token?: string | null;
  /** True when the share link that granted access is read-only. */
  share_read_only?: boolean | null;
}

/**
 * A share link for a conversation.
 *
 * Shaped from `SessionShare` in `go/api/database/models.go` rather than from the old UI's
 * own copy: the controller is what answers, and the two had drifted in casing.
 */
export interface SessionShare {
  id: number;
  /** The capability the link carries. Sent back as `x-share-token`. */
  token: string;
  session_id: string;
  /** Who created it. Only the owner may list or revoke. */
  user_id: string;
  read_only: boolean;
  created_at: string;
}

export interface CreateSessionShareRequest {
  /**
   * Omitted means read-only.
   *
   * The controller defaults it to `true` and treats `false` as a deliberate opt-in to
   * read-write, which is the right way round for something that hands out access.
   */
  read_only?: boolean;
}

export interface CreateSessionRequest {
  agent_ref?: string;
  name?: string;
  id?: string;
}
