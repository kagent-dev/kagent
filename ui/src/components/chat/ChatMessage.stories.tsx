import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import ChatMessage from "./ChatMessage";
import { Role, type Message } from "@a2a-js/sdk";
import { createMockMessage, createTextPart } from "@/mocks/factories";

const meta = {
  title: "Chat/ChatMessage",
  component: ChatMessage,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <div className="w-full max-w-6xl mx-auto px-4 py-8">
        <Story />
      </div>
    ),
  ],
  tags: ["autodocs"],
} satisfies Meta<typeof ChatMessage>;

export default meta;
type Story = StoryObj<typeof meta>;

const createMessage = (overrides: Partial<Message> = {}): Message =>
  createMockMessage({
    messageId: "msg-123",
    role: Role.ROLE_AGENT,
    parts: [createTextPart("Default message content")],
    contextId: "ctx-1",
    taskId: "task-1",
    ...overrides,
  });

export const UserMessage: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_USER,
      messageId: "user-msg-1",
      parts: [createTextPart("Hello, can you help me with this?")],
    }),
    allMessages: [],
  },
};

export const AgentMessage: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_AGENT,
      parts: [createTextPart("Of course! I'd be happy to help you with that.")],
    }),
    allMessages: [],
  },
};

export const AgentMessageWithTimestamp: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_AGENT,
      parts: [createTextPart("Here's the response to your question.")],
      metadata: {
        displaySource: "assistant",
        timestamp: Date.now(),
      },
    }),
    allMessages: [],
  },
};

export const MessageWithLongContent: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_AGENT,
      parts: [
        createTextPart(`This is a much longer response that contains multiple paragraphs of information.

The first paragraph explains the main concept.

The second paragraph provides additional details and examples.

The third paragraph concludes with a summary of the key points.`,
        ),
      ],
    }),
    allMessages: [],
  },
};

export const MessageWithMarkdown: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_AGENT,
      parts: [
        createTextPart(`# Response Title

Here's a **bold** statement and an *italic* one.

## Key Points
- First point
- Second point
- Third point

\`\`\`javascript
const example = () => {
  return "code block";
};
\`\`\``,
        ),
      ],
    }),
    allMessages: [],
  },
};

export const MessageWithCodeBlocks: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_AGENT,
      parts: [
        createTextPart(`Here's how to implement this feature:

\`\`\`python
def calculate_sum(numbers):
    return sum(numbers)

result = calculate_sum([1, 2, 3, 4, 5])
print(result)
\`\`\`

And here's the JavaScript equivalent:

\`\`\`javascript
const calculateSum = (numbers) => {
  return numbers.reduce((a, b) => a + b, 0);
};

const result = calculateSum([1, 2, 3, 4, 5]);
console.log(result);
\`\`\``,
        ),
      ],
    }),
    allMessages: [],
  },
};

export const MessageWithCustomDisplaySource: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_AGENT,
      parts: [createTextPart("Response from custom agent")],
      metadata: {
        displaySource: "DataAnalyzer",
      },
    }),
    allMessages: [],
  },
};

export const MessageWithAgentContext: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_AGENT,
      parts: [createTextPart("Response from context agent")],
    }),
    allMessages: [],
    agentContext: {
      namespace: "default",
      agentName: "my_agent",
    },
  },
};

export const ShortUserMessage: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_USER,
      messageId: "user-msg-2",
      parts: [createTextPart("OK")],
    }),
    allMessages: [],
  },
};

export const AgentMessageWithTable: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_AGENT,
      parts: [
        createTextPart(`Here's the data in table format:

| Name | Score | Status |
|------|-------|--------|
| Alice | 95 | Pass |
| Bob | 87 | Pass |
| Charlie | 72 | Pass |
| Diana | 65 | Fail |`,
        ),
      ],
    }),
    allMessages: [],
  },
};

/**
 * Prose alongside content that offers no break opportunities — a tool-call id
 * carrying a Gemini thought signature, and a long URL. The prose has to keep
 * its word boundaries while the two long tokens wrap.
 */
export const MessageWithUnbreakableTokens: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_AGENT,
      parts: [
        createTextPart(`The kagent controller reconciles ModelConfig resources and propagates configuration to the agent deployment automatically.

Tool call id: \`call_9f2a__thought__QmFzZTY0RW5jb2RlZFRob3VnaHRTaWduYXR1cmVCbG9iQmFzZTY0RW5jb2RlZFRob3VnaHRTaWduYXR1cmU\`

See https://github.com/kagent-dev/kagent/blob/main/ui/src/components/chat/ChatMessage.tsx for the renderer.`,
        ),
      ],
    }),
    allMessages: [],
  },
};

export const MessageWithMultipleParts: Story = {
  args: {
    message: createMessage({
      role: Role.ROLE_AGENT,
      parts: [
        createTextPart("First part of the message."),
        createTextPart("Second part of the message."),
      ],
    }),
    allMessages: [],
  },
};
