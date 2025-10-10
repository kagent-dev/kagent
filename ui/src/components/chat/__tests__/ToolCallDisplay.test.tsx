import { describe, test, expect } from '@jest/globals';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { Message } from '@a2a-js/sdk';
import ToolCallDisplay from '../ToolCallDisplay';

// Mock the child components
jest.mock('../AgentCallDisplay', () => ({
  __esModule: true,
  default: ({ call, nestedCalls, depth }: any) => (
    <div data-testid={`agent-call-${call.id}`} data-depth={depth}>
      Agent: {call.name}
      {nestedCalls && nestedCalls.length > 0 && (
        <div data-testid={`nested-calls-${call.id}`}>
          Nested: {nestedCalls.length}
        </div>
      )}
    </div>
  ),
}));

jest.mock('@/components/ToolDisplay', () => ({
  __esModule: true,
  default: ({ call }: any) => <div data-testid={`tool-call-${call.id}`}>Tool: {call.name}</div>,
}));

describe('ToolCallDisplay - Nested Agent Calls', () => {
  test('displays simple agent call without nesting', () => {
    const currentMessage: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'call-1',
            name: 'kagent__NS__test-agent',
            args: { query: 'test' },
          },
          metadata: {
            kagent_type: 'function_call',
          },
        },
      ],
    };

    const allMessages = [currentMessage];

    render(<ToolCallDisplay currentMessage={currentMessage} allMessages={allMessages} />);

    expect(screen.getByTestId('agent-call-call-1')).toBeInTheDocument();
    expect(screen.getByText(/Agent: kagent__NS__test-agent/)).toBeInTheDocument();
  });

  test('builds nested call hierarchy for agent->subagent calls', () => {
    const parentCall: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'parent-1',
            name: 'kagent__NS__main-agent',
            args: {},
          },
          metadata: {
            kagent_type: 'function_call',
          },
        },
      ],
    };

    const nestedCall: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'nested-1',
            name: 'kagent__NS__sub-agent',
            args: {},
          },
          metadata: {
            kagent_type: 'function_call',
          },
        },
      ],
    };

    const parentResult: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'parent-1',
            name: 'kagent__NS__main-agent',
            response: { result: 'done' },
          },
          metadata: {
            kagent_type: 'function_response',
          },
        },
      ],
    };

    const allMessages = [parentCall, nestedCall, parentResult];

    render(<ToolCallDisplay currentMessage={parentCall} allMessages={allMessages} />);

    // Parent call should be displayed
    expect(screen.getByTestId('agent-call-parent-1')).toBeInTheDocument();

    // Should have nested calls indicator
    expect(screen.getByTestId('nested-calls-parent-1')).toBeInTheDocument();
    expect(screen.getByText(/Nested: 1/)).toBeInTheDocument();
  });

  test('handles multi-level nesting (agent->subagent->subagent)', () => {
    const level1Call: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'level1',
            name: 'kagent__NS__main',
            args: {},
          },
          metadata: {
            kagent_type: 'function_call',
          },
        },
      ],
    };

    const level2Call: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'level2',
            name: 'kagent__NS__research',
            args: {},
          },
          metadata: {
            kagent_type: 'function_call',
          },
        },
      ],
    };

    const level3Call: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'level3',
            name: 'kagent__NS__data',
            args: {},
          },
          metadata: {
            kagent_type: 'function_call',
          },
        },
      ],
    };

    const level1Result: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'level1',
            name: 'kagent__NS__main',
            response: { result: 'complete' },
          },
          metadata: {
            kagent_type: 'function_response',
          },
        },
      ],
    };

    const allMessages = [level1Call, level2Call, level3Call, level1Result];

    render(<ToolCallDisplay currentMessage={level1Call} allMessages={allMessages} />);

    // Level 1 should be displayed with nested calls
    expect(screen.getByTestId('agent-call-level1')).toBeInTheDocument();
    expect(screen.getByTestId('nested-calls-level1')).toBeInTheDocument();
  });

  test('handles regular tool calls within agent calls', () => {
    const agentCall: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'agent-1',
            name: 'kagent__NS__agent',
            args: {},
          },
          metadata: {
            kagent_type: 'function_call',
          },
        },
      ],
    };

    const toolCall: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'tool-1',
            name: 'read_file', // No __NS__ = regular tool
            args: { path: '/test' },
          },
          metadata: {
            kagent_type: 'function_call',
          },
        },
      ],
    };

    const agentResult: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'agent-1',
            name: 'kagent__NS__agent',
            response: { result: 'done' },
          },
          metadata: {
            kagent_type: 'function_response',
          },
        },
      ],
    };

    const allMessages = [agentCall, toolCall, agentResult];

    render(<ToolCallDisplay currentMessage={agentCall} allMessages={allMessages} />);

    // Should display agent call with nested tool
    expect(screen.getByTestId('agent-call-agent-1')).toBeInTheDocument();
  });

  test('does not nest calls that appear after parent completion', () => {
    const parentCall: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'parent',
            name: 'kagent__NS__parent',
            args: {},
          },
          metadata: {
            kagent_type: 'function_call',
          },
        },
      ],
    };

    const parentResult: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'parent',
            name: 'kagent__NS__parent',
            response: { result: 'done' },
          },
          metadata: {
            kagent_type: 'function_response',
          },
        },
      ],
    };

    const afterCall: Message = {
      kind: 'message',
      role: 'assistant',
      parts: [
        {
          kind: 'data',
          data: {
            id: 'after',
            name: 'kagent__NS__after',
            args: {},
          },
          metadata: {
            kagent_type: 'function_call',
          },
        },
      ],
    };

    const allMessages = [parentCall, parentResult, afterCall];

    render(<ToolCallDisplay currentMessage={parentCall} allMessages={allMessages} />);

    // Parent should not have nested calls (call came after completion)
    expect(screen.getByTestId('agent-call-parent')).toBeInTheDocument();
    expect(screen.queryByTestId('nested-calls-parent')).not.toBeInTheDocument();
  });
});
