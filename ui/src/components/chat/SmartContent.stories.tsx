import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { SmartContent } from "./SmartContent";

const meta = {
  title: "Chat/SmartContent",
  component: SmartContent,
  parameters: {
    layout: "centered",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof SmartContent>;

export default meta;
type Story = StoryObj<typeof meta>;

export const PlainString: Story = {
  args: {
    data: "Hello, this is a plain text string.",
  },
};

export const MarkdownString: Story = {
  args: {
    data: `# Heading 1
## Heading 2
This is a paragraph with **bold** and *italic* text.

- Item 1
- Item 2
- Item 3

\`\`\`javascript
const x = 42;
console.log(x);
\`\`\``,
  },
};

export const SimpleJsonObject: Story = {
  args: {
    data: {
      name: "John Doe",
      age: 30,
      email: "john@example.com",
      active: true,
    },
  },
};

export const NestedJsonObject: Story = {
  args: {
    data: {
      user: {
        id: 123,
        profile: {
          firstName: "Jane",
          lastName: "Smith",
          contact: {
            email: "jane@example.com",
            phone: "+1-555-0123",
          },
        },
      },
      settings: {
        theme: "dark",
        notifications: true,
      },
    },
  },
};

export const JsonWithJsonInString: Story = {
  args: {
    data: {
      message: "User data",
      payload: '{"nested": "json", "value": 42}',
      timestamp: 1234567890,
    },
  },
};

export const ArrayOfItems: Story = {
  args: {
    data: [
      { id: 1, name: "Item 1", status: "active" },
      { id: 2, name: "Item 2", status: "inactive" },
      { id: 3, name: "Item 3", status: "pending" },
    ],
  },
};

export const NullValue: Story = {
  args: {
    data: null,
  },
};

export const UndefinedValue: Story = {
  args: {
    data: undefined,
  },
};

export const DeeplyNestedObject: Story = {
  args: {
    data: {
      level1: {
        level2: {
          level3: {
            level4: {
              level5: {
                value: "deeply nested",
                count: 5,
              },
            },
          },
        },
      },
    },
  },
};

export const EmptyObject: Story = {
  args: {
    data: {},
  },
};

export const EmptyArray: Story = {
  args: {
    data: [],
  },
};

export const WithErrorClassName: Story = {
  args: {
    data: "Error message displayed in red",
    className: "text-red-500",
  },
};

export const MixedTypes: Story = {
  args: {
    data: {
      string: "text value",
      number: 42,
      boolean: true,
      null: null,
      array: [1, 2, 3],
      object: { nested: "value" },
    },
  },
};
