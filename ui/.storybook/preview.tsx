import type { Preview } from '@storybook/nextjs-vite'
import { worker } from '../src/mocks/browser'
import React, { ReactNode } from 'react'
import '../src/app/globals.css'
import { AgentsContext } from '../src/components/AgentsProvider'
import type { AgentsContextType } from '../src/components/AgentsProvider'
import type { Agent } from '../src/types'

const mockContextValue: AgentsContextType = {
  agents: [],
  models: [],
  loading: false,
  error: "",
  tools: [],
  refreshAgents: async () => {},
  refreshModels: async () => {},
  refreshTools: async () => {},
  createNewAgent: async () => ({ message: "mock", data: {} as Agent }),
  updateAgent: async () => ({ message: "mock", data: {} as Agent }),
  getAgent: async () => null,
  validateAgentData: () => ({}),
};

interface MockAgentsProviderProps {
  children: ReactNode;
  value?: Partial<AgentsContextType>;
}

function MockAgentsProvider({ children, value }: MockAgentsProviderProps) {
  return (
    <AgentsContext.Provider value={{ ...mockContextValue, ...value }}>
      {children}
    </AgentsContext.Provider>
  );
}

const preview: Preview = {
  beforeAll: async () => {
    await worker.start({ onUnhandledRequest: 'bypass' });
  },
  parameters: {
    nextjs: {
      appDirectory: true,
    },
    controls: {
      matchers: {
       color: /(background|color)$/i,
       date: /Date$/i,
      },
    },
    a11y: {
      test: 'todo'
    }
  },
  decorators: [
    (Story) => {
      document.documentElement.classList.add('dark');
      return (
        <MockAgentsProvider>
          <Story />
        </MockAgentsProvider>
      );
    },
  ],
};

export default preview;
