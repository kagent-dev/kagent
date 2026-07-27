import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import type { Message } from "@a2a-js/sdk";
import ToolCallGroup, { buildToolCallResultsIndex } from "./ToolCallGroup";
import ToolCallDisplay from "./ToolCallDisplay";

const meta = {
  title: "Chat/ToolCallGroup",
  component: ToolCallGroup,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [
    (Story) => (
      <div className="w-full max-w-4xl mx-auto px-4 py-8">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof ToolCallGroup>;

export default meta;
type Story = StoryObj<typeof meta>;

const requestMessage = (id: string, name: string, args: Record<string, unknown>): Message => ({
  kind: "message",
  messageId: `req-${id}`,
  role: "agent",
  parts: [
    {
      kind: "data",
      data: { id, name, args },
      metadata: { adk_type: "function_call" },
    },
  ],
});

const responseMessage = (id: string, name: string, result: string, isError = false): Message => ({
  kind: "message",
  messageId: `res-${id}`,
  role: "agent",
  parts: [
    {
      kind: "data",
      data: { id, name, response: { result, isError } },
      metadata: { adk_type: "function_response" },
    },
  ],
});

const buildTranscript = (calls: Array<{ id: string; name: string; args: Record<string, unknown>; result?: string; isError?: boolean }>) => {
  const messages: Message[] = [];
  for (const c of calls) {
    messages.push(requestMessage(c.id, c.name, c.args));
    if (c.result !== undefined) {
      messages.push(responseMessage(c.id, c.name, c.result, c.isError));
    }
  }
  return messages;
};

const renderGroup = (messages: Message[]) => (
  <ToolCallGroup messages={messages} resultsByCallId={buildToolCallResultsIndex(messages)}>
    {messages.map((m, i) => (
      <ToolCallDisplay key={i} currentMessage={m} allMessages={messages} />
    ))}
  </ToolCallGroup>
);

const allPassing = buildTranscript([
  { id: "c1", name: "k8s_get_resources", args: { kind: "Pod", namespace: "kagent" }, result: "3 pods running" },
  { id: "c2", name: "k8s_get_events", args: { namespace: "kagent" }, result: "No warning events" },
  { id: "c3", name: "k8s_describe_resource", args: { name: "kagent-controller" }, result: "Deployment healthy" },
]);

export const AllPassing: Story = {
  args: { messages: allPassing, resultsByCallId: buildToolCallResultsIndex(allPassing), children: null },
  render: (args) => renderGroup(args.messages),
};

const withFailures = buildTranscript([
  { id: "c1", name: "k8s_get_resources", args: { kind: "Pod" }, result: "3 pods running" },
  { id: "c2", name: "k8s_apply_manifest", args: { manifest: "..." }, result: "error: forbidden", isError: true },
  { id: "c3", name: "k8s_get_events", args: {}, result: "ok" },
  { id: "c4", name: "helm_list", args: {}, result: "connection refused", isError: true },
]);

export const WithFailures: Story = {
  args: { messages: withFailures, resultsByCallId: buildToolCallResultsIndex(withFailures), children: null },
  render: (args) => renderGroup(args.messages),
};

const inFlight = buildTranscript([
  { id: "c1", name: "k8s_get_resources", args: { kind: "Pod" }, result: "3 pods running" },
  { id: "c2", name: "k8s_get_events", args: {}, result: "ok" },
  { id: "c3", name: "helm_list", args: {} }, // no result yet
]);

export const Running: Story = {
  args: { messages: inFlight, resultsByCallId: buildToolCallResultsIndex(inFlight), children: null },
  render: (args) => renderGroup(args.messages),
};

const single = buildTranscript([
  { id: "c1", name: "k8s_get_resources", args: { kind: "Pod", namespace: "default" }, result: "12 pods" },
]);

export const SingleCall: Story = {
  args: { messages: single, resultsByCallId: buildToolCallResultsIndex(single), children: null },
  render: (args) => renderGroup(args.messages),
};

const many = buildTranscript(
  Array.from({ length: 9 }, (_, i) => ({
    id: `c${i}`,
    name: ["k8s_get_resources", "k8s_get_events", "k8s_describe_resource", "helm_list", "helm_get_values", "k8s_get_logs"][i % 6],
    args: { attempt: i },
    result: i === 4 ? "error: timeout" : "ok",
    isError: i === 4,
  })),
);

export const ManyCalls: Story = {
  args: { messages: many, resultsByCallId: buildToolCallResultsIndex(many), children: null },
  render: (args) => renderGroup(args.messages),
};
