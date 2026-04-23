import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import GroupedChats from "./GroupedChats";
import { SidebarProvider, Sidebar, SidebarContent } from "@/components/ui/sidebar";
import type { Session } from "@/types";

const meta: Meta<typeof GroupedChats> = {
  title: "Sidebars/GroupedChats",
  component: GroupedChats,
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
type Story = StoryObj<typeof GroupedChats>;

const createSession = (id: string, name: string, createdDaysAgo: number, updatedDaysAgo = createdDaysAgo): Session => ({
  id,
  name,
  agent_id: 'kgent__NS__k8s',
  user_id: "user-1",
  created_at: new Date(Date.now() - createdDaysAgo * 24 * 3600000).toISOString(),
  updated_at: new Date(Date.now() - updatedDaysAgo * 24 * 3600000).toISOString(),
  deleted_at: "",
});

const todaySession = createSession("session-today-1", "Quick question", 0);
const yesterdaySession = createSession("session-yesterday-1", "Review PR feedback", 1);
const olderSession = createSession("session-older-1", "Analyze performance", 5);

export const WithSessionsAcrossTimeframes: Story = {
  args: {
    agentName: "k8s",
    agentNamespace: "kagent",
    sessions: [
      todaySession,
      createSession("session-today-2", "Another today chat", 0),
      yesterdaySession,
      olderSession,
      createSession("session-older-2", "Old conversation", 10),
    ],
  },
};

export const EmptySessions: Story = {
  args: {
    agentName: "k8s",
    agentNamespace: "kagent",
    sessions: [],
  },
};

export const ManySessions: Story = {
  args: {
    agentName: "k8s",
    agentNamespace: "kagent",
    sessions: [
      createSession("session-1", "First chat today", 0),
      createSession("session-2", "Second chat today", 0),
      createSession("session-3", "Third chat today", 0),
      createSession("session-4", "Fourth chat today", 0),
      createSession("session-5", "Yesterday chat 1", 1),
      createSession("session-6", "Yesterday chat 2", 1),
      createSession("session-7", "Older chat 1", 3),
      createSession("session-8", "Older chat 2", 5),
      createSession("session-9", "Older chat 3", 7),
      createSession("session-10", "Older chat 4", 14),
    ],
  },
};

export const RecentlyUpdatedOlderSession: Story = {
  args: {
    agentName: "k8s",
    agentNamespace: "kagent",
    sessions: [
      createSession("session-old-active", "Created last week, active today", 7, 0),
      createSession("session-new-inactive", "Created today, inactive", 0, 0.2),
      createSession("session-yesterday", "Yesterday chat", 1),
    ],
  },
};
