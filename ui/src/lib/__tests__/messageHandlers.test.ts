import { describe, test, expect } from '@jest/globals';
import { v4 as uuidv4 } from 'uuid';
import { Message } from '@a2a-js/sdk';
import {
  extractMessagesFromTasks,
  extractTokenStatsFromTasks,
  createMessage,
  normalizeToolResultToText,
  type ToolResponseData,
  type ADKMetadata,
  createMessageHandlers,
} from '@/lib/messageHandlers';

describe('messageHandlers helpers', () => {
  test('normalizeToolResultToText handles string result', () => {
    const data: ToolResponseData = { id: '1', name: 'tool', response: { result: 'hello' } };
    expect(normalizeToolResultToText(data)).toBe('hello');
  });

  test('normalizeToolResultToText handles content array', () => {
    const data: ToolResponseData = { id: '1', name: 'tool', response: { result: { content: [{ text: 'a' }, { text: 'b' }] } } } as any;
    expect(normalizeToolResultToText(data)).toBe('ab');
  });

  test('normalizeToolResultToText handles object fallback', () => {
    const data: ToolResponseData = { id: '1', name: 'tool', response: { result: { foo: 'bar' } } } as any;
    expect(normalizeToolResultToText(data)).toContain('foo');
  });

  test('createMessage builds a message with metadata', () => {
    const msg = createMessage('hi', 'assistant', { originalType: 'TextMessage', contextId: 'ctx', taskId: 'task' });
    expect(msg.kind).toBe('message');
    expect(msg.parts[0]).toEqual({ kind: 'text', text: 'hi' });
    expect((msg.metadata as any).originalType).toBe('TextMessage');
    expect(msg.contextId).toBe('ctx');
    expect(msg.taskId).toBe('task');
  });

  test('extractMessagesFromTasks deduplicates messageIds', () => {
    const mId = uuidv4();
    const tasks: any = [
      { history: [{ kind: 'message', messageId: mId }, { kind: 'message', messageId: mId }] },
    ];
    const out = extractMessagesFromTasks(tasks);
    expect(out.length).toBe(1);
    expect(out[0].messageId).toBe(mId);
  });

  test('extractTokenStatsFromTasks picks max token counts', () => {
    const tasks: any = [
      { metadata: { kagent_usage_metadata: { totalTokenCount: 10, promptTokenCount: 3, candidatesTokenCount: 7 } } as ADKMetadata },
      { metadata: { kagent_usage_metadata: { totalTokenCount: 12, promptTokenCount: 1, candidatesTokenCount: 9 } } as ADKMetadata },
    ];
    const stats = extractTokenStatsFromTasks(tasks);
    expect(stats.total).toBe(12);
    expect(stats.input).toBe(3);
    expect(stats.output).toBe(9);
  });
});

describe('createMessageHandlers smoke test (function_call and delegated response)', () => {
  test('emits ToolCallRequestEvent and delegated TextMessage + execution event', () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater) => {
        const next = updater(emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setTokenStats: () => {},
      setChatStatus: () => {},
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    // Simulate status-update with function_call to an agent tool
    const statusUpdateCall: any = {
      kind: 'status-update',
      contextId: 'ctx',
      taskId: 'task',
      final: false,
      status: {
        state: 'working',
        message: {
          role: 'agent',
          parts: [
            {
              kind: 'data',
              data: { id: 'call_1', name: 'kagent__NS__k8s_agent', args: { request: 'list' } },
              metadata: { kagent_type: 'function_call' },
            },
          ],
        },
      },
    };

    // @ts-expect-error - private access in tests
    handlers.handleMessageEvent(statusUpdateCall);

    // Simulate status-update with function_response from agent
    const statusUpdateResp: any = {
      kind: 'status-update',
      contextId: 'ctx',
      taskId: 'task',
      final: false,
      status: {
        state: 'working',
        message: {
          role: 'agent',
          parts: [
            {
              kind: 'data',
              data: { id: 'call_1', name: 'kagent__NS__k8s_agent', response: { result: 'ok' } },
              metadata: { kagent_type: 'function_response' },
            },
          ],
        },
      },
    };

    // @ts-expect-error
    handlers.handleMessageEvent(statusUpdateResp);

    // Expect: ToolCallRequestEvent + delegated TextMessage + ToolCallExecutionEvent
    expect(emitted.length).toBe(3);
    const [req, plain, exec] = emitted;
    expect((req.metadata as any).originalType).toBe('ToolCallRequestEvent');
    expect((plain.metadata as any).originalType).toBe('TextMessage');
    expect((plain.metadata as any).displaySource).toBe('kagent/k8s-agent');
    expect((exec.metadata as any).originalType).toBe('ToolCallExecutionEvent');
  });

  test('emits ToolCallRequestEvent + ToolCallExecutionEvent for non-agent tool', () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater) => {
        const next = updater(emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setTokenStats: () => {},
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    const statusUpdateCall: any = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      status: { state: 'working', message: { role: 'agent', parts: [{ kind: 'data', data: { id: 'call_2', name: 'some_tool', args: { a: 1 } }, metadata: { kagent_type: 'function_call' } }] } }
    };
    // @ts-expect-error
    handlers.handleMessageEvent(statusUpdateCall);

    const statusUpdateResp: any = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      status: { state: 'working', message: { role: 'agent', parts: [{ kind: 'data', data: { id: 'call_2', name: 'some_tool', response: { result: 'tool ok' } }, metadata: { kagent_type: 'function_response' } }] } }
    };
    // @ts-expect-error
    handlers.handleMessageEvent(statusUpdateResp);

    expect(emitted.length).toBe(2);
    expect((emitted[0].metadata as any).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as any).originalType).toBe('ToolCallExecutionEvent');
  });

  test('final text message on status-update with text part', () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater) => {
        const next = updater(emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setTokenStats: () => {},
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    const statusWithText: any = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: true,
      status: { state: 'working', message: { role: 'agent', parts: [{ kind: 'text', text: 'hello' }] } }
    };
    // @ts-expect-error
    handlers.handleMessageEvent(statusWithText);

    expect(emitted.length).toBe(1);
    expect((emitted[0].metadata as any).originalType).toBe('TextMessage');
    expect((emitted[0].parts[0] as any).text).toBe('hello');
  });

  test('artifact-update converts tool parts and appends summary', () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater) => {
        const next = updater(emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setTokenStats: () => {},
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    const artifactEvent: any = {
      kind: 'artifact-update', contextId: 'ctx', taskId: 'task', lastChunk: true,
      artifact: {
        parts: [
          { kind: 'data', data: { id: 'call_3', name: 'some_tool', args: { q: 1 } }, metadata: { kagent_type: 'function_call' } },
          { kind: 'data', data: { id: 'call_3', name: 'some_tool', response: { result: 'out' } }, metadata: { kagent_type: 'function_response' } },
        ]
      }
    };
    // @ts-expect-error
    handlers.handleMessageEvent(artifactEvent);

    // Expect: request, execution, summary (no text message since no text part)
    expect(emitted.length).toBe(3);
    expect((emitted[0].metadata as any).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as any).originalType).toBe('ToolCallExecutionEvent');
    expect((emitted[2].metadata as any).originalType).toBe('ToolCallSummaryMessage');
  });

  test('token usage updates on status-update metadata', () => {
    let capturedStats: any = { total: 0, input: 0, output: 0 };
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater) => {
        const next = updater(emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setTokenStats: (updater: any) => {
        capturedStats = updater(capturedStats);
      },
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    const statusWithUsage: any = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      metadata: { kagent_usage_metadata: { totalTokenCount: 5, promptTokenCount: 2, candidatesTokenCount: 3 } },
      status: { state: 'working' }
    };
    // @ts-expect-error
    handlers.handleMessageEvent(statusWithUsage);

    expect(capturedStats).toEqual({ total: 5, input: 2, output: 3 });
  });
});


