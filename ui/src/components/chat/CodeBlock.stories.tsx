import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import CodeBlock from "./CodeBlock";

const meta = {
  title: "Chat/CodeBlock",
  component: CodeBlock,
  parameters: {
    layout: "centered",
  },
  tags: ["autodocs"],
} satisfies Meta<typeof CodeBlock>;

export default meta;
type Story = StoryObj<typeof meta>;

export const JavaScriptCode: Story = {
  args: {
    className: "language-javascript",
    children: [
      `const greeting = (name) => {
  console.log(\`Hello, \${name}!\`);
  return name.toUpperCase();
};

greeting("World");`,
    ],
  },
};

export const PythonCode: Story = {
  args: {
    className: "language-python",
    children: [
      `def fibonacci(n):
    if n <= 1:
        return n
    return fibonacci(n - 1) + fibonacci(n - 2)

result = fibonacci(10)
print(f"Result: {result}")`,
    ],
  },
};

export const TypeScriptCode: Story = {
  args: {
    className: "language-typescript",
    children: [
      `interface User {
  id: number;
  name: string;
  email: string;
}

const createUser = (user: User): void => {
  console.log(\`User \${user.name} created\`);
};`,
    ],
  },
};

export const ShortSnippet: Story = {
  args: {
    className: "language-javascript",
    children: ["const x = 42;"],
  },
};

export const LongCodeBlock: Story = {
  args: {
    className: "language-python",
    children: [
      `class DataProcessor:
    def __init__(self, data):
        self.data = data
        self.results = []
    
    def process(self):
        for item in self.data:
            processed = self._transform(item)
            self.results.append(processed)
        return self.results
    
    def _transform(self, item):
        return {
            'id': item.get('id'),
            'value': item.get('value', 0) * 2,
            'timestamp': datetime.now().isoformat()
        }
    
    def get_summary(self):
        return {
            'total_items': len(self.results),
            'average_value': sum(r['value'] for r in self.results) / len(self.results)
        }

processor = DataProcessor([
    {'id': 1, 'value': 10},
    {'id': 2, 'value': 20},
    {'id': 3, 'value': 30}
])
results = processor.process()
summary = processor.get_summary()
print(summary)`,
    ],
  },
};

export const SqlCode: Story = {
  args: {
    className: "language-sql",
    children: [
      `SELECT 
    u.id,
    u.name,
    COUNT(o.id) as order_count,
    SUM(o.total) as total_spent
FROM users u
LEFT JOIN orders o ON u.id = o.user_id
WHERE u.created_at > '2024-01-01'
GROUP BY u.id, u.name
HAVING COUNT(o.id) > 0
ORDER BY total_spent DESC
LIMIT 10;`,
    ],
  },
};

export const HtmlCode: Story = {
  args: {
    className: "language-html",
    children: [
      `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Sample Page</title>
</head>
<body>
    <header>
        <h1>Welcome</h1>
        <nav>
            <ul>
                <li><a href="/">Home</a></li>
                <li><a href="/about">About</a></li>
            </ul>
        </nav>
    </header>
    <main>
        <p>This is sample HTML content.</p>
    </main>
</body>
</html>`,
    ],
  },
};

export const JsonCode: Story = {
  args: {
    className: "language-json",
    children: [
      `{
  "name": "example-app",
  "version": "1.0.0",
  "description": "An example application",
  "dependencies": {
    "react": "^18.0.0",
    "typescript": "^5.0.0"
  },
  "scripts": {
    "build": "tsc",
    "start": "node dist/index.js"
  }
}`,
    ],
  },
};

export const BashCode: Story = {
  args: {
    className: "language-bash",
    children: [
      `#!/bin/bash

# Install dependencies
npm install

# Build the project
npm run build

# Run tests
npm test

# Deploy
npm run deploy`,
    ],
  },
};
