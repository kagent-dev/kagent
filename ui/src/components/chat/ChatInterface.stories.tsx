import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { Role } from "@a2a-js/sdk";
import { mocked } from "storybook/test";
import ChatInterface from "./ChatInterface";
import { ChatAgentProvider } from "./ChatAgentContext";
import { checkSessionExists, getSessionTasks } from "@/app/actions/sessions";
import type { AgentResponse } from "@/types";
import {
  createMockTextMessage,
  createMockSession,
  createMockTask,
  createMockToolCallTask,
} from "@/mocks/factories";

// ---------------------------------------------------------------------------
// Shared mock data
// ---------------------------------------------------------------------------

const mockSession = createMockSession();

const mockAgent: AgentResponse = {
  id: 1,
  agent: {
    metadata: { name: "test-agent", namespace: "default" },
    spec: { type: "Declarative", description: "Storybook chat agent" },
  },
  model: "gpt-4o",
  modelProvider: "openai",
  modelConfigRef: "default/test-model",
  tools: [],
  deploymentReady: true,
  accepted: true,
};

const singleExchangeTask = createMockTask("task-1", "session-123", [
  createMockTextMessage("task-1-user", Role.ROLE_USER, "Hello, can you help me with Kubernetes?"),
  createMockTextMessage(
    "task-1-agent",
    Role.ROLE_AGENT,
    "Of course! I'd be happy to help you with Kubernetes. What would you like to know? I can assist with deployments, services, pods, configmaps, secrets, and much more.",
  ),
]);

const multiExchangeTasks = [
  createMockTask("task-1", "session-456", [
    createMockTextMessage("task-1-user", Role.ROLE_USER, "What is a Kubernetes Pod?"),
    createMockTextMessage(
      "task-1-agent",
      Role.ROLE_AGENT,
      "A **Pod** is the smallest deployable unit in Kubernetes. It represents a single instance of a running process in your cluster.\n\nKey characteristics:\n- A Pod can contain one or more containers\n- Containers in a Pod share the same network namespace (IP address and port space)\n- They can communicate via `localhost`\n- Pods are ephemeral — they are not designed to run forever",
    ),
  ]),
  createMockTask("task-2", "session-456", [
    createMockTextMessage("task-2-user", Role.ROLE_USER, "How do I create a deployment?"),
    createMockTextMessage("task-2-agent", Role.ROLE_AGENT, `Here's how to create a Kubernetes Deployment:

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
    ),
  ]),
  createMockTask("task-3", "session-456", [
    createMockTextMessage("task-3-user", Role.ROLE_USER, "Can you explain Services?"),
    createMockTextMessage("task-3-agent", Role.ROLE_AGENT, `A **Service** is an abstraction that defines a logical set of Pods and a policy to access them.

| Type | Description |
|------|-------------|
| ClusterIP | Internal cluster IP (default) |
| NodePort | Exposes on each node's IP |
| LoadBalancer | Cloud provider load balancer |
| ExternalName | Maps to a DNS name |

Services use **label selectors** to find their target Pods and automatically load-balance traffic across them.`,
    ),
  ]),
];

const toolCallTask = createMockToolCallTask(
  "task-tool-1",
  "session-789",
  "kubectl_get_pods",
  { namespace: "default", labelSelector: "app=nginx" },
  "NAME            READY   STATUS    RESTARTS   AGE\nnginx-abc123    1/1     Running   0          2d\nnginx-def456    1/1     Running   0          2d",
);

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
        <ChatAgentProvider
          currentAgent={mockAgent}
          agentType={mockAgent.agent.spec.type}
        >
          <Story />
        </ChatAgentProvider>
      </div>
    ),
  ],
  /** Reset server-action mocks between stories to prevent leakage. */
  beforeEach: () => {
    mocked(checkSessionExists).mockReset();
    mocked(getSessionTasks).mockReset();
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
 * No action mocks are needed because no API calls are made.
 */
export const NewChat: Story = {
  args: {
    selectedAgentName: "test-agent",
    selectedNamespace: "default",
  },
};

/**
 * An existing session loaded via its `sessionId`.
 * The session actions return a single user→agent exchange.
 */
export const ExistingSessionWithMessages: Story = {
  args: {
    selectedAgentName: "test-agent",
    selectedNamespace: "default",
    sessionId: "session-123",
  },
  beforeEach: () => {
    mocked(checkSessionExists).mockResolvedValue({ message: "Session exists", data: true });
    mocked(getSessionTasks).mockResolvedValue({ message: "Tasks fetched", data: [singleExchangeTask] });
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
    mocked(checkSessionExists).mockResolvedValue({ message: "Session exists", data: true });
    mocked(getSessionTasks).mockResolvedValue({ message: "Tasks fetched", data: multiExchangeTasks });
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
    mocked(checkSessionExists).mockResolvedValue({ message: "Session exists", data: true });
    mocked(getSessionTasks).mockResolvedValue({ message: "Tasks fetched", data: [toolCallTask] });
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
    mocked(checkSessionExists).mockResolvedValue({ message: "Session does not exist", data: false });
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
    mocked(checkSessionExists).mockResolvedValue({ message: "Session exists", data: true });
    mocked(getSessionTasks).mockResolvedValue({ message: "Tasks fetched", data: [] });
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
    mocked(checkSessionExists).mockImplementation(() => new Promise((resolve) => {
      setTimeout(() => resolve({ message: "Session exists", data: true }), 2000);
    }));
    mocked(getSessionTasks).mockImplementation(() => new Promise((resolve) => {
      setTimeout(() => resolve({ message: "Tasks fetched", data: [singleExchangeTask] }), 2000);
    }));
  },
};

/**
 * Session is pre-loaded via the `selectedSession` prop, but the component
 * still calls `checkSessionExists` when `sessionId` is present.
 */
export const PreLoadedSession: Story = {
  args: {
    selectedAgentName: "test-agent",
    selectedNamespace: "default",
    selectedSession: mockSession,
    sessionId: "session-123",
  },
  beforeEach: () => {
    mocked(checkSessionExists).mockResolvedValue({ message: "Session exists", data: true });
    mocked(getSessionTasks).mockResolvedValue({ message: "Tasks fetched", data: [singleExchangeTask] });
  },
};
