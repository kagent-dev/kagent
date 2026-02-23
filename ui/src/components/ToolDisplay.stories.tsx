import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import ToolDisplay from "./ToolDisplay";
import { ScrollArea } from "@/components/ui/scroll-area";

const meta: Meta<typeof ToolDisplay> = {
  title: "Chat/ToolDisplay",
  component: ToolDisplay,
};

export default meta;
type Story = StoryObj<typeof ToolDisplay>;

export const BasicCompleted: Story = {
  args: {
    call: {
      id: "call_abc123",
      name: "read_file",
      args: { path: "/src/components/App.tsx" },
    },
    result: { content: "File contents here" },
    status: "completed",
  },
};

export const WideArgs: Story = {
  args: {
    call: {
      id: "call_wide_args",
      name: "exec_sandbox_command",
      args: {
        sandbox_id: "sandbox-abc123-def456-ghi789",
        command: "find /workspace/src -name '*.tsx' -exec grep -l 'ScrollArea' {} \\; | head -50 && echo '---' && cat /workspace/src/components/ui/scroll-area.tsx",
        timeout: 30000,
        working_directory: "/home/user/projects/my-very-long-project-name/packages/frontend-application/src/components",
      },
    },
    result: {
      content: JSON.stringify({
        stdout: "/workspace/src/components/chat/ChatInterface.tsx\n/workspace/src/components/sidebars/AllSessionsSidebar.tsx\n/workspace/src/components/ui/scroll-area.tsx\n---\nimport * as React from 'react'\nimport * as ScrollAreaPrimitive from '@radix-ui/react-scroll-area'",
        stderr: "",
        exit_code: 0,
      }),
    },
    status: "completed",
  },
};

export const VeryLongUrl: Story = {
  args: {
    call: {
      id: "call_long_url",
      name: "read_sandbox_files",
      args: {
        path: "/home/user/projects/kagent/ui/src/components/sidebars/AllSessionsSidebar.tsx",
        sandbox_id: "sandbox-very-long-identifier-that-goes-on-and-on-abc123def456",
      },
    },
    result: {
      content: "https://github.com/kagent-dev/kagent/blob/main/ui/src/components/sidebars/AllSessionsSidebar.tsx#L101-L137 this is a very long URL that should not cause the card to overflow its container width",
    },
    status: "completed",
  },
};

export const LongUnbreakableString: Story = {
  args: {
    call: {
      id: "call_unbreakable",
      name: "search_code",
      args: {
        query: "aaaaaaaaaaaaaaaaaaaaaaaaaaabbbbbbbbbbbbbbbbbbbbbbbbbbcccccccccccccccccccccccccdddddddddddddddddddddddddeeeeeeeeeeeeeeeeeeeeeeeee",
      },
    },
    result: {
      content: "found_in_file:/workspace/src/some/very/deeply/nested/directory/structure/that/goes/on/and/on/without/any/spaces/component.tsx:line42:aaaaaaaaaaaaaaaaaaaaaaaaaaabbbbbbbbbbbbbbbbbbbbbbbbbbcccccccccccccccccccccccccdddddddddddddddddddddddddeeeeeeeeeeeeeeeeeeeeeeeee",
    },
    status: "completed",
  },
};

const ChatLayoutDecorator = (Story: React.ComponentType) => (
  <div className="flex h-screen">
    <div className="w-64 shrink-0 bg-sidebar border-r">
      <div className="p-4 text-sm text-sidebar-foreground">Sidebar (16rem)</div>
    </div>
    <main className="flex-1 min-w-0 bg-background">
      <div className="w-full h-full flex flex-col items-center">
        <div className="flex-1 w-full overflow-hidden relative">
          <ScrollArea className="w-full h-full py-12">
            <div className="flex flex-col space-y-5 px-4">
              <div className="flex items-center gap-2 text-sm border-l-2 py-2 px-4 border-l-blue-500">
                <div className="flex flex-col gap-1 w-full">
                  <div className="text-xs font-bold">user</div>
                  <p>Review the scroll area component</p>
                </div>
              </div>
              <Story />
              <div className="flex items-center gap-2 text-sm border-l-2 py-2 px-4 border-l-violet-500">
                <div className="flex flex-col gap-1 w-full">
                  <div className="text-xs font-bold">assistant</div>
                  <p>Here are the results of the tool call above.</p>
                </div>
              </div>
            </div>
          </ScrollArea>
        </div>
      </div>
    </main>
  </div>
);

export const InChatLayout: Story = {
  decorators: [ChatLayoutDecorator],
  args: {
    ...WideArgs.args,
  },
};

export const InChatLayoutLongUrl: Story = {
  decorators: [ChatLayoutDecorator],
  args: {
    ...VeryLongUrl.args,
  },
};

export const InChatLayoutUnbreakable: Story = {
  decorators: [ChatLayoutDecorator],
  args: {
    ...LongUnbreakableString.args,
  },
};
