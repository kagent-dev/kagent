// Empty-state card shown before any messages exist. Communicates the three
// pre-conversation states: connecting, disconnected, or ready to start.

interface AcpChatEmptyStateProps {
  /** A connection/handshake is in flight. */
  connecting: boolean;
  /** The socket dropped and auto-reconnect gave up. */
  disconnected: boolean;
}

export default function AcpChatEmptyState({ connecting, disconnected }: AcpChatEmptyStateProps) {
  return (
    <div className="flex items-center justify-center h-full min-h-[50vh]">
      <div className="max-w-md rounded-lg border bg-card p-6 text-center shadow-sm">
        <h2 className="mb-2 text-lg font-medium">
          {connecting ? "Connecting to the agent…" : disconnected ? "Disconnected" : "Start a conversation"}
        </h2>
        <p className="text-muted-foreground">
          {connecting
            ? "Reaching the agent and loading this conversation. This is usually quick, but can take a little longer if the agent needs to resume."
            : disconnected
              ? "Couldn't reach the agent. Reload the page to try again."
              : "To begin chatting with the agent, type your message in the input box below."}
        </p>
      </div>
    </div>
  );
}
