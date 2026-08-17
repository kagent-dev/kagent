import type { StorybookConfig } from '@storybook/nextjs-vite';
import { fileURLToPath } from 'node:url';

const config: StorybookConfig = {
  stories: [
    "../src/**/*.mdx",
    "../src/**/*.stories.@(js|jsx|mjs|ts|tsx)"
  ],
  addons: [
    "@chromatic-com/storybook",
    "@storybook/addon-vitest",
    "@storybook/addon-a11y",
    "@storybook/addon-docs",
    "@storybook/addon-onboarding"
  ],
  framework: "@storybook/nextjs-vite",
  staticDirs: ["../public"],
  viteFinal: async (config) => {
    config.resolve ??= {};
    const aliases = Array.isArray(config.resolve.alias)
      ? config.resolve.alias
      : Object.entries(config.resolve.alias ?? {}).map(([find, replacement]) => ({ find, replacement }));
    config.resolve.alias = [
      {
        find: "@/lib/grpc/client",
        replacement: fileURLToPath(new URL("./mocks/grpc-client.ts", import.meta.url)),
      },
      {
        find: "@/app/actions/sessions",
        replacement: fileURLToPath(new URL("./mocks/sessions.ts", import.meta.url)),
      },
      {
        find: "@/app/actions/mcp-apps",
        replacement: fileURLToPath(new URL("./mocks/mcp-apps.ts", import.meta.url)),
      },
      {
        find: "@/app/actions/agents",
        replacement: fileURLToPath(new URL("./mocks/agents.ts", import.meta.url)),
      },
      {
        find: "@/app/actions/sessionShares",
        replacement: fileURLToPath(new URL("./mocks/session-shares.ts", import.meta.url)),
      },
      {
        find: "@/app/actions/namespaces",
        replacement: fileURLToPath(new URL("./mocks/namespaces.ts", import.meta.url)),
      },
      ...aliases,
    ];
    return config;
  },
};
export default config;
