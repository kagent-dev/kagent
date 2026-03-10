import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import ChatInterface from "./ChatInterface";
import { worker } from "@/mocks/browser";
import {
  createMockSession,
  createMockTask,
  createMockToolCallTask,
  sessionExistsHandler,
  sessionNotFoundHandler,
  sessionTasksHandler,
  emptySessionTasksHandler,
  slowSessionExistsHandler,
  slowSessionTasksHandler,
} from "@/mocks/handlers";

// ---------------------------------------------------------------------------
// Shared mock data
// ---------------------------------------------------------------------------

const mockSession = createMockSession();

const singleExchangeTask = createMockTask("task-1", "session-123", [
  {
    role: "user",
    text: "Hello, can you help me with Kubernetes?",
  },
  {
    role: "agent",
    text: "Of course! I'd be happy to help you with Kubernetes. What would you like to know? I can assist with deployments, services, pods, configmaps, secrets, and much more.",
  },
]);

const multiExchangeTasks = [
  createMockTask("task-1", "session-456", [
    {
      role: "user",
      text: "What is a Kubernetes Pod?",
    },
    {
      role: "agent",
      text: "A **Pod** is the smallest deployable unit in Kubernetes. It represents a single instance of a running process in your cluster.\n\nKey characteristics:\n- A Pod can contain one or more containers\n- Containers in a Pod share the same network namespace (IP address and port space)\n- They can communicate via `localhost`\n- Pods are ephemeral — they are not designed to run forever",
    },
  ]),
  createMockTask("task-2", "session-456", [
    {
      role: "user",
      text: "How do I create a deployment?",
    },
    {
      role: "agent",
      text: `Here's how to create a Kubernetes Deployment:

\`\`\`yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  labels:
    app: my-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
      - name: my-app
        image: my-app:1.0.0
        ports:
        - containerPort: 8080
\`\`\`

You can apply this with:

\`\`\`bash
kubectl apply -f deployment.yaml
\`\`\`

This creates a Deployment that maintains 3 replicas of your application.`,
    },
  ]),
  createMockTask("task-3", "session-456", [
    {
      role: "user",
      text: "Can you explain Services?",
    },
    {
      role: "agent",
      text: `A **Service** is an abstraction that defines a logical set of Pods and a policy to access them.

| Type | Description |
|------|-------------|
| ClusterIP | Internal cluster IP (default) |
| NodePort | Exposes on each node's IP |
| LoadBalancer | Cloud provider load balancer |
| ExternalName | Maps to a DNS name |

Services use **label selectors** to find their target Pods and automatically load-balance traffic across them.`,
    },
  ]),
];

const toolCallTask = createMockToolCallTask(
  "task-tool-1",
  "session-789",
  "kubectl_get_pods",
  { namespace: "default", labelSelector: "app=nginx" },
  "NAME            READY   STATUS    RESTARTS   AGE\nnginx-abc123    1/1     Running   0          2d\nnginx-def456    1/1     Running   0          2d",
);

const multiExchangeSession = createMockSession({
  id: "session-456",
  name: "Kubernetes Q&A",
});

const toolCallSession = createMockSession({
  id: "session-789",
  name: "Tool call demo",
});

// ---------------------------------------------------------------------------
// Meta
// ---------------------------------------------------------------------------

const meta = {
  title: "Chat/ChatInterface",
  component: ChatInterface,
  parameters: {
    layout: "fullscreen",
    nextjs: {
      appDirectory: true,
      navigation: {
        pathname: "/agents/default/test-agent/chat/session-123",
      },
    },
  },
  decorators: [
    (Story) => (
      <div style={{ height: "100vh", width: "100%" }}>
        <Story />
      </div>
    ),
  ],
  /** Reset MSW handlers between stories to prevent leakage. */
  beforeEach: () => {
    worker.resetHandlers();
  },
  tags: ["autodocs"],
} satisfies Meta<typeof ChatInterface>;

export default meta;
type Story = StoryObj<typeof meta>;

// ---------------------------------------------------------------------------
// Stories
// ---------------------------------------------------------------------------

/**
 * A brand-new chat with no session yet.
 * Shows the "Start a conversation" welcome prompt.
 * No MSW handlers needed — no API calls are made.
 */
export const NewChat: Story = {
  args: {
    selectedAgentName: "test-agent",
    selectedNamespace: "default",
  },
};

/**
 * An existing session loaded via its `sessionId`.
 * MSW intercepts `checkSessionExists` and `getSessionTasks` to return
 * a single user→agent exchange.
 */
export const ExistingSessionWithMessages: Story = {
  args: {
    selectedAgentName: "test-agent",
    selectedNamespace: "default",
    sessionId: "session-123",
  },
  beforeEach: () => {
    worker.use(
      sessionExistsHandler(mockSession),
      sessionTasksHandler([singleExchangeTask]),
    );
  },
};

/**
 * A longer conversation spanning multiple tasks / exchanges.
 */
export const LongConversation: Story = {
  args: {
    selectedAgentName: "test-agent",
    selectedNamespace: "default",
    sessionId: "session-456",
  },
  parameters: {
    nextjs: {
      navigation: {
        pathname: "/agents/default/test-agent/chat/session-456",
      },
    },
  },
  beforeEach: () => {
    worker.use(
      sessionExistsHandler(multiExchangeSession),
      sessionTasksHandler(multiExchangeTasks),
    );
  },
};

/**
 * Session contains tool-call request & execution result messages
 * alongside regular text messages.
 */
export const WithToolCalls: Story = {
  args: {
    selectedAgentName: "test-agent",
    selectedNamespace: "default",
    sessionId: "session-789",
  },
  parameters: {
    nextjs: {
      navigation: {
        pathname: "/agents/default/test-agent/chat/session-789",
      },
    },
  },
  beforeEach: () => {
    worker.use(
      sessionExistsHandler(toolCallSession),
      sessionTasksHandler([toolCallTask]),
    );
  },
};

/**
 * The requested `sessionId` does not exist on the backend.
 * Shows the "Session not found" error state.
 */
export const SessionNotFound: Story = {
  args: {
    selectedAgentName: "test-agent",
    selectedNamespace: "default",
    sessionId: "session-nonexistent",
  },
  parameters: {
    nextjs: {
      navigation: {
        pathname: "/agents/default/test-agent/chat/session-nonexistent",
      },
    },
  },
  beforeEach: () => {
    worker.use(sessionNotFoundHandler());
  },
};

/**
 * An existing session that has no messages yet.
 * Shows the "Start a conversation" prompt even though a session exists.
 */
export const EmptySession: Story = {
  args: {
    selectedAgentName: "test-agent",
    selectedNamespace: "default",
    sessionId: "session-123",
  },
  beforeEach: () => {
    worker.use(
      sessionExistsHandler(mockSession),
      emptySessionTasksHandler(),
    );
  },
};

/**
 * Simulates a slow backend — the loading spinner is visible while the
 * session and tasks endpoints respond after a 2 s delay.
 */
export const Loading: Story = {
  args: {
    selectedAgentName: "test-agent",
    selectedNamespace: "default",
    sessionId: "session-123",
  },
  beforeEach: () => {
    worker.use(
      slowSessionExistsHandler(mockSession, 2000),
      slowSessionTasksHandler([singleExchangeTask], 2000),
    );
  },
};

/**
 * Session is pre-loaded via the `selectedSession` prop, but the component
 * still calls `checkSessionExists` when `sessionId` is present, so MSW
 * handlers are required for both the session check and task history.
 */
export const PreLoadedSession: Story = {
  args: {
    selectedAgentName: "test-agent",
    selectedNamespace: "default",
    selectedSession: mockSession,
    sessionId: "session-123",
  },
  beforeEach: () => {
    worker.use(
      sessionExistsHandler(mockSession),
      sessionTasksHandler([singleExchangeTask]),
    );
  },
};
