import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import ChatItem from "./ChatItem";
import {
  SidebarProvider,
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarMenu,
  SidebarMenuSub,
} from "@/components/ui/sidebar";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Collapsible } from "@radix-ui/react-collapsible";
import { CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ChevronRight } from "lucide-react";

const meta: Meta<typeof ChatItem> = {
  title: "Sidebars/ChatItem",
  component: ChatItem,
  decorators: [
    (Story) => (
      <SidebarProvider defaultOpen={true}>
        <div className="flex h-screen w-full">
          <Sidebar side="left" collapsible="none">
            <SidebarContent>
              <ScrollArea className="flex-1 my-4">
                <SidebarGroup>
                  <SidebarMenu>
                    <Collapsible defaultOpen className="group/collapsible w-full">
                      <div className="w-full">
                        <CollapsibleTrigger className="flex items-center justify-between w-full rounded-md p-2 pr-[9px] text-sm hover:bg-sidebar-accent hover:text-sidebar-accent-foreground">
                          <span>Today</span>
                          <ChevronRight className="h-4 w-4 shrink-0 transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
                        </CollapsibleTrigger>
                      </div>
                      <CollapsibleContent>
                        <SidebarMenuSub className="mx-0 px-0 ml-2 pl-2">
                          <Story />
                        </SidebarMenuSub>
                      </CollapsibleContent>
                    </Collapsible>
                  </SidebarMenu>
                </SidebarGroup>
              </ScrollArea>
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
type Story = StoryObj<typeof ChatItem>;

export const ShortTitle: Story = {
  args: {
    sessionId: "session-1",
    agentName: "momus-gpt",
    agentNamespace: "kagent",
    onDelete: async () => {},
    sessionName: "Quick question",
    createdAt: new Date().toISOString(),
  },
};

export const LongTitle: Story = {
  args: {
    sessionId: "session-2",
    agentName: "momus-gpt",
    agentNamespace: "kagent",
    onDelete: async () => {},
    sessionName: "Review https://github.com/Smartest-Fly/app/pull/1234 and provide feedback on the authentication implementation",
    createdAt: new Date().toISOString(),
  },
};

export const LongTitleWithAgentName: Story = {
  args: {
    sessionId: "session-3",
    agentName: "momus-gpt",
    agentNamespace: "kagent",
    onDelete: async () => {},
    sessionName: "Review https://github.com/Smartest-Fly/app/pull/1234 and provide feedback on the authentication implementation",
    createdAt: new Date().toISOString(),
  },
};

export const MultipleLongTitles: Story = {
  render: () => (
    <>
      {[
        "Review https://github.com/Smartest-Fly/app/pull/1234 and provide detailed feedback",
        "Can you analyze whether we can replace the video processing pipeline with a simpler approach",
        "I noticed the jira-mcp-server was returning a malformed response for certain edge cases",
        "Lets improve the agent call display component to show more information about the delegated task",
        "When creating a sandbox in tartarus using create_sandbox, the timeout parameter is being ignored",
        "Short title",
        "Review https://github.com/another-repo/with-a-very-long-name/pull/5678",
      ].map((title, i) => (
        <ChatItem
          key={i}
          sessionId={`session-${i}`}
          agentName="momus-gpt"
          agentNamespace="kagent"
          onDelete={async () => {}}
          sessionName={title}
          createdAt={new Date(Date.now() - i * 3600000).toISOString()}

        />
      ))}
    </>
  ),
};
