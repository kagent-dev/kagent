import { describe, test, expect } from '@jest/globals';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import AgentCallDisplay from '../AgentCallDisplay';
import { FunctionCall } from '@/types';

// Mock the ToolDisplay component
jest.mock('@/components/ToolDisplay', () => ({
  __esModule: true,
  default: ({ call }: any) => <div data-testid={`tool-${call.id}`}>Tool: {call.name}</div>,
}));

describe('AgentCallDisplay - Nested Rendering', () => {
  const basicCall: FunctionCall = {
    id: 'test-1',
    name: 'kagent__NS__test-agent',
    args: { query: 'test' },
  };

  test('renders agent call without nesting at depth 0', () => {
    render(<AgentCallDisplay call={basicCall} status="requested" />);

    expect(screen.getByText(/kagent\/test-agent/)).toBeInTheDocument();
    expect(screen.queryByText(/nested level/)).not.toBeInTheDocument();
  });

  test('renders agent call with nested level indicator at depth 1', () => {
    render(<AgentCallDisplay call={basicCall} status="requested" depth={1} />);

    expect(screen.getByText(/nested level 1/)).toBeInTheDocument();
  });

  test('renders agent call with nested level indicator at depth 2', () => {
    render(<AgentCallDisplay call={basicCall} status="requested" depth={2} />);

    expect(screen.getByText(/nested level 2/)).toBeInTheDocument();
  });

  test('displays "Delegated Calls" section when nestedCalls provided', () => {
    const nestedCalls = [
      {
        id: 'nested-1',
        call: {
          id: 'nested-1',
          name: 'kagent__NS__sub-agent',
          args: {},
        },
        status: 'completed' as const,
      },
    ];

    render(<AgentCallDisplay call={basicCall} status="completed" nestedCalls={nestedCalls} />);

    expect(screen.getByText(/Delegated Calls \(1\)/)).toBeInTheDocument();
  });

  test('displays correct count for multiple nested calls', () => {
    const nestedCalls = [
      {
        id: 'nested-1',
        call: {
          id: 'nested-1',
          name: 'kagent__NS__sub-agent-1',
          args: {},
        },
        status: 'completed' as const,
      },
      {
        id: 'nested-2',
        call: {
          id: 'nested-2',
          name: 'kagent__NS__sub-agent-2',
          args: {},
        },
        status: 'completed' as const,
      },
      {
        id: 'nested-3',
        call: {
          id: 'nested-3',
          name: 'read_file', // Tool call
          args: {},
        },
        status: 'completed' as const,
      },
    ];

    render(<AgentCallDisplay call={basicCall} status="completed" nestedCalls={nestedCalls} />);

    expect(screen.getByText(/Delegated Calls \(3\)/)).toBeInTheDocument();
  });

  test('does not display "Delegated Calls" section when no nested calls', () => {
    render(<AgentCallDisplay call={basicCall} status="completed" nestedCalls={[]} />);

    expect(screen.queryByText(/Delegated Calls/)).not.toBeInTheDocument();
  });

  test('renders nested tool calls within delegated section', () => {
    const nestedCalls = [
      {
        id: 'tool-1',
        call: {
          id: 'tool-1',
          name: 'read_file',
          args: { path: '/test' },
        },
        status: 'completed' as const,
      },
    ];

    render(<AgentCallDisplay call={basicCall} status="completed" nestedCalls={nestedCalls} />);

    // Should render ToolDisplay component for non-agent calls
    expect(screen.getByTestId('tool-tool-1')).toBeInTheDocument();
  });

  test('displays different status indicators correctly', () => {
    const { rerender } = render(<AgentCallDisplay call={basicCall} status="requested" />);
    expect(screen.getByText(/Delegating/)).toBeInTheDocument();

    rerender(<AgentCallDisplay call={basicCall} status="executing" />);
    expect(screen.getByText(/Awaiting response/)).toBeInTheDocument();

    rerender(<AgentCallDisplay call={basicCall} status="completed" />);
    expect(screen.getByText(/Completed/)).toBeInTheDocument();
  });

  test('displays error status correctly', () => {
    const result = {
      content: 'Error occurred',
      is_error: true,
    };

    render(<AgentCallDisplay call={basicCall} status="completed" result={result} isError={true} />);

    expect(screen.getByText(/Failed/)).toBeInTheDocument();
  });

  test('applies correct styling for nested depth', () => {
    const { container } = render(
      <AgentCallDisplay call={basicCall} status="requested" depth={1} />
    );

    // Check for border styling on nested calls
    const card = container.querySelector('.border-l-4');
    expect(card).toBeInTheDocument();
  });

  test('recursive nesting - nested call can have its own nested calls', () => {
    const deeplyNestedCalls = [
      {
        id: 'level2',
        call: {
          id: 'level2',
          name: 'kagent__NS__level2-agent',
          args: {},
        },
        status: 'completed' as const,
        nestedCalls: [
          {
            id: 'level3',
            call: {
              id: 'level3',
              name: 'kagent__NS__level3-agent',
              args: {},
            },
            status: 'completed' as const,
          },
        ],
      },
    ];

    render(
      <AgentCallDisplay call={basicCall} status="completed" nestedCalls={deeplyNestedCalls} />
    );

    // Should have delegated calls sections (multiple due to recursion)
    const delegatedSections = screen.getAllByText(/Delegated Calls \(1\)/);
    expect(delegatedSections.length).toBeGreaterThan(0);
  });

  test('displays input/output sections', () => {
    const callWithArgs: FunctionCall = {
      id: 'test-1',
      name: 'kagent__NS__test-agent',
      args: { query: 'search term', limit: 10 },
    };

    const result = {
      content: 'Search completed successfully',
      is_error: false,
    };

    render(<AgentCallDisplay call={callWithArgs} status="completed" result={result} />);

    // Input and Output sections should be present
    expect(screen.getByText(/Input/)).toBeInTheDocument();
    expect(screen.getByText(/Output/)).toBeInTheDocument();
  });

  test('enforces maximum nesting depth limit', () => {
    const MAX_DEPTH = 10;

    render(<AgentCallDisplay call={basicCall} status="requested" depth={MAX_DEPTH + 1} />);

    // Should display warning instead of rendering normally
    expect(screen.getByText(/Maximum nesting depth reached/)).toBeInTheDocument();
    expect(screen.queryByText(/kagent\/test-agent/)).not.toBeInTheDocument();
  });

  test('renders normally at maximum allowed depth', () => {
    const MAX_DEPTH = 10;

    render(<AgentCallDisplay call={basicCall} status="requested" depth={MAX_DEPTH} />);

    // Should render normally at exactly max depth
    expect(screen.getByText(/kagent\/test-agent/)).toBeInTheDocument();
    expect(screen.queryByText(/Maximum nesting depth reached/)).not.toBeInTheDocument();
  });
});
