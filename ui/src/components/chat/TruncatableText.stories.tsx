import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { TruncatableText } from "./TruncatableText";

const meta = {
  title: "Chat/TruncatableText",
  component: TruncatableText,
  parameters: {
    layout: "centered",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof TruncatableText>;

export default meta;
type Story = StoryObj<typeof meta>;

export const ShortText: Story = {
  args: {
    content: "This is a short text message.",
  },
};

export const LongMarkdownWithHeadings: Story = {
  args: {
    content: `# Main Title
## Section 1
This is the first section with some content.

## Section 2
Here's another section with more details.

### Subsection 2.1
Even more detailed information goes here.`,
  },
};

export const MarkdownWithLists: Story = {
  args: {
    content: `# Shopping List

## Groceries
- Apples
- Bananas
- Milk
- Bread

## Hardware
1. Nails
2. Screws
3. Hammer
4. Wrench`,
  },
};

export const JsonContent: Story = {
  args: {
    content: JSON.stringify(
      {
        id: 123,
        name: "Product",
        price: 99.99,
        inStock: true,
      },
      null,
      2
    ),
    isJson: true,
  },
};

export const StreamingContent: Story = {
  args: {
    content: "This is content being streamed in real-time...",
    isStreaming: true,
  },
};

export const ContentWithCodeBlocks: Story = {
  args: {
    content: `# Code Example

Here's some Python code:

\`\`\`python
def hello_world():
    print("Hello, World!")
    return 42
\`\`\`

And here's JavaScript:

\`\`\`javascript
const greeting = () => {
  console.log("Hello, World!");
  return 42;
};
\`\`\``,
  },
};

export const ContentWithTable: Story = {
  args: {
    content: `# Data Table

| Name | Age | City |
|------|-----|------|
| Alice | 28 | NYC |
| Bob | 35 | LA |
| Charlie | 42 | Chicago |`,
  },
};

export const ContentWithHtmlPreview: Story = {
  args: {
    content: `# HTML Preview

\`\`\`html
<!DOCTYPE html>
<html>
<head>
  <title>Sample Page</title>
</head>
<body>
  <h1>Hello World</h1>
  <p>This is a sample HTML page.</p>
</body>
</html>
\`\`\``,
  },
};

export const LongMarkdownContent: Story = {
  args: {
    content: `# Complete Documentation

## Introduction
This is a comprehensive guide covering multiple topics and sections.

## Getting Started
To get started, follow these steps:
1. Install dependencies
2. Configure settings
3. Run the application

## Features
- Feature A with detailed description
- Feature B with detailed description
- Feature C with detailed description

## Code Examples

\`\`\`python
# Python example
def process_data(data):
    return [x * 2 for x in data]
\`\`\`

## Conclusion
This concludes the documentation.`,
  },
};

export const StreamingLongContent: Story = {
  args: {
    content: `# Streaming Response

This is a long response that is being streamed in real-time. The content continues to arrive...

## Section 1
More content is arriving as the stream progresses.

## Section 2
Additional information being delivered...`,
    isStreaming: true,
  },
};

export const MarkdownWithLinks: Story = {
  args: {
    content: `# Links Example

Check out [OpenAI](https://openai.com) for more information.

Visit [GitHub](https://github.com) to explore code repositories.

Learn more at [Documentation](https://docs.example.com).`,
  },
};

export const MarkdownWithQuotes: Story = {
  args: {
    content: `# Quotes

> This is a blockquote with important information.
> It can span multiple lines.

> Another quote here.`,
  },
};
