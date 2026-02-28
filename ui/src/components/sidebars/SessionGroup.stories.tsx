import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import ChatGroup from "./SessionGroup";
import { SidebarProvider, Sidebar, SidebarContent } from "@/components/ui/sidebar";
import type { Session } from "@/types";

const meta: Meta<typeof ChatGroup> = {
  title: "Sidebars/SessionGroup",
  component: ChatGroup,
  decorators: [
    (Story) => (
      <SidebarProvider defaultOpen={true}>
        <div className="flex h-screen w-full">
          <Sidebar side="left" collapsible="none">
            <SidebarContent>
              <Story />
            </SidebarContent>
          </Sidebar>
          <main className="flex-1 bg-background p-8">
            <p className="text-muted-foreground">Main content area</p>
          </main>
        </div>
      </SidebarProvider>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof ChatGroup>;

const mockSessions: Session[] = [
  {
    id: "session-1",
    name: "Quick question",
    agent_id: 1,
    user_id: "user-1",
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    deleted_at: "",
  },
  {
    id: "session-2",
    name: "Review PR and provide feedback",
    agent_id: 1,
    user_id: "user-1",
    created_at: new Date(Date.now() - 3600000).toISOString(),
    updated_at: new Date(Date.now() - 3600000).toISOString(),
    deleted_at: "",
  },
  {
    id: "session-3",
    name: "Analyze performance issues",
    agent_id: 1,
    user_id: "user-1",
    created_at: new Date(Date.now() - 7200000).toISOString(),
    updated_at: new Date(Date.now() - 7200000).toISOString(),
    deleted_at: "",
  },
];

const mockLongNameSessions: Session[] = [
  {
    id: "session-long-1",
    name: "Review https://github.com/Smartest-Fly/app/pull/1234 and provide detailed feedback on the authentication implementation and security considerations",
    agent_id: 1,
    user_id: "user-1",
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    deleted_at: "",
  },
  {
    id: "session-long-2",
    name: "Can you analyze whether we can replace the video processing pipeline with a simpler approach that reduces memory usage",
    agent_id: 1,
    user_id: "user-1",
    created_at: new Date(Date.now() - 3600000).toISOString(),
    updated_at: new Date(Date.now() - 3600000).toISOString(),
    deleted_at: "",
  },
];

export const TodayGroup: Story = {
  args: {
    title: "Today",
    sessions: mockSessions,
    onDeleteSession: async () => {},
    onDownloadSession: async () => {},
    agentName: "momus-gpt",
    agentNamespace: "kagent",
  },
};

export const EmptyGroup: Story = {
  args: {
    title: "Yesterday",
    sessions: [],
    onDeleteSession: async () => {},
    onDownloadSession: async () => {},
    agentName: "momus-gpt",
    agentNamespace: "kagent",
  },
};

export const LongSessionNames: Story = {
  args: {
    title: "Older",
    sessions: mockLongNameSessions,
    onDeleteSession: async () => {},
    onDownloadSession: async () => {},
    agentName: "momus-gpt",
    agentNamespace: "kagent",
  },
};
