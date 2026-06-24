import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import ChatMessage from "./ChatMessage";
import type { Message } from "@a2a-js/sdk";

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

const makeTextPart = (text: string) => ({
  content: { $case: "text" as const, value: text },
  metadata: {},
  filename: "",
  mediaType: "text/plain",
});

const createMessage = (overrides: Record<string, unknown> = {}): Message => ({
  messageId: "msg-123",
  role: 2,
  parts: [makeTextPart("Default message content")],
  contextId: "",
  taskId: "",
  metadata: {},
  extensions: [],
  referenceTaskIds: [],
  ...overrides,
} as Message);

export const UserMessage: Story = {
  args: {
    message: createMessage({
      role: 1,
      messageId: "user-msg-1",
      parts: [makeTextPart("Hello, can you help me with this?")],
    }),
    allMessages: [],
  },
};

export const AgentMessage: Story = {
  args: {
    message: createMessage({
      role: 2,
      parts: [makeTextPart("Of course! I'd be happy to help you with that.")],
    }),
    allMessages: [],
  },
};

export const AgentMessageWithTimestamp: Story = {
  args: {
    message: createMessage({
      role: 2,
      parts: [makeTextPart("Here's the response to your question.")],
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
      role: 2,
      parts: [
        makeTextPart(`This is a much longer response that contains multiple paragraphs of information.

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
      role: 2,
      parts: [
        makeTextPart(`# Response Title

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
      role: 2,
      parts: [
        makeTextPart(`Here's how to implement this feature:

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
      role: 2,
      parts: [makeTextPart("Response from custom agent")],
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
      role: 2,
      parts: [makeTextPart("Response from context agent")],
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
      role: 1,
      messageId: "user-msg-2",
      parts: [makeTextPart("OK")],
    }),
    allMessages: [],
  },
};

export const AgentMessageWithTable: Story = {
  args: {
    message: createMessage({
      role: 2,
      parts: [
        makeTextPart(`Here's the data in table format:

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

export const MessageWithMultipleParts: Story = {
  args: {
    message: createMessage({
      role: 2,
      parts: [
        makeTextPart("First part of the message."),
        makeTextPart("Second part of the message."),
      ],
    }),
    allMessages: [],
  },
};
