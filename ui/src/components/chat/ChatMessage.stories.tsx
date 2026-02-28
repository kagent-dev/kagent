import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import ChatMessage from "./ChatMessage";
import type { Message } from "@a2a-js/sdk";

const meta = {
  title: "Chat/ChatMessage",
  component: ChatMessage,
  parameters: {
    layout: "centered",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof ChatMessage>;

export default meta;
type Story = StoryObj<typeof meta>;

const createMessage = (overrides: Partial<Message> = {}): Message => ({
  kind: "message",
  messageId: "msg-123",
  role: "agent",
  parts: [{ kind: "text", text: "Default message content" }],
  ...overrides,
});

export const UserMessage: Story = {
  args: {
    message: createMessage({
      role: "user",
      messageId: "user-msg-1",
      parts: [{ kind: "text", text: "Hello, can you help me with this?" }],
    }),
    allMessages: [],
  },
};

export const AgentMessage: Story = {
  args: {
    message: createMessage({
      role: "agent",
      parts: [{ kind: "text", text: "Of course! I'd be happy to help you with that." }],
    }),
    allMessages: [],
  },
};

export const AgentMessageWithTimestamp: Story = {
  args: {
    message: createMessage({
      role: "agent",
      parts: [{ kind: "text", text: "Here's the response to your question." }],
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
      role: "agent",
      parts: [
        {
          kind: "text",
          text: `This is a much longer response that contains multiple paragraphs of information.

The first paragraph explains the main concept.

The second paragraph provides additional details and examples.

The third paragraph concludes with a summary of the key points.`,
        },
      ],
    }),
    allMessages: [],
  },
};

export const MessageWithMarkdown: Story = {
  args: {
    message: createMessage({
      role: "agent",
      parts: [
        {
          kind: "text",
          text: `# Response Title

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
        },
      ],
    }),
    allMessages: [],
  },
};

export const MessageWithCodeBlocks: Story = {
  args: {
    message: createMessage({
      role: "agent",
      parts: [
        {
          kind: "text",
          text: `Here's how to implement this feature:

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
        },
      ],
    }),
    allMessages: [],
  },
};

export const MessageWithCustomDisplaySource: Story = {
  args: {
    message: createMessage({
      role: "agent",
      parts: [{ kind: "text", text: "Response from custom agent" }],
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
      role: "agent",
      parts: [{ kind: "text", text: "Response from context agent" }],
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
      role: "user",
      messageId: "user-msg-2",
      parts: [{ kind: "text", text: "OK" }],
    }),
    allMessages: [],
  },
};

export const AgentMessageWithTable: Story = {
  args: {
    message: createMessage({
      role: "agent",
      parts: [
        {
          kind: "text",
          text: `Here's the data in table format:

| Name | Score | Status |
|------|-------|--------|
| Alice | 95 | Pass |
| Bob | 87 | Pass |
| Charlie | 72 | Pass |
| Diana | 65 | Fail |`,
        },
      ],
    }),
    allMessages: [],
  },
};

export const MessageWithMultipleParts: Story = {
  args: {
    message: createMessage({
      role: "agent",
      parts: [
        { kind: "text", text: "First part of the message." },
        { kind: "text", text: "Second part of the message." },
      ],
    }),
    allMessages: [],
  },
};
