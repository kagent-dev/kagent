import { describe, test, expect } from '@jest/globals';
import { v4 as uuidv4 } from 'uuid';
import { Message, Task } from '@a2a-js/sdk';
import {
  extractMessagesFromTasks,
  extractTokenStatsFromTasks,
  createMessage,
  normalizeToolResultToText,
  getMetadataValue,
  type ToolResponseData,
  type ADKMetadata,
  createMessageHandlers,
} from '@/lib/messageHandlers';
import type { TokenStats } from '@/types';

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
    const { messages: out } = extractMessagesFromTasks(tasks);
    expect(out.length).toBe(1);
    expect(out[0].messageId).toBe(mId);
  });

  test('extractMessagesFromTasks detects pending task in working state', () => {
    const tasks: any = [
      { id: 'task-1', status: { state: 'working' }, history: [] },
    ];
    const { pendingTask } = extractMessagesFromTasks(tasks);
    expect(pendingTask).toBeDefined();
    expect(pendingTask?.taskId).toBe('task-1');
    expect(pendingTask?.state).toBe('working');
  });

  test('extractMessagesFromTasks detects pending task in submitted state', () => {
    const tasks: any = [
      { id: 'task-1', status: { state: 'submitted' }, history: [] },
    ];
    const { pendingTask } = extractMessagesFromTasks(tasks);
    expect(pendingTask).toBeDefined();
    expect(pendingTask?.taskId).toBe('task-1');
    expect(pendingTask?.state).toBe('submitted');
  });

  test('extractMessagesFromTasks returns no pending task for completed tasks', () => {
    const tasks: any = [
      { id: 'task-1', status: { state: 'completed' }, history: [] },
    ];
    const { pendingTask } = extractMessagesFromTasks(tasks);
    expect(pendingTask).toBeUndefined();
  });

  test('extractMessagesFromTasks injects tokenStats into non-user agent messages only', () => {
    const tasks = [
      {
        history: [
          { kind: 'message', messageId: 'a1', role: 'agent', parts: [],
            metadata: { kagent_usage_metadata: { totalTokenCount: 10, promptTokenCount: 3, candidatesTokenCount: 7 } } },
          { kind: 'message', messageId: 'u1', role: 'user', parts: [],
            metadata: { kagent_usage_metadata: { totalTokenCount: 5, promptTokenCount: 2, candidatesTokenCount: 3 } } },
          { kind: 'message', messageId: 'a2', role: 'agent', parts: [], metadata: {} },
        ],
      },
    ] as unknown as Task[];
    const { messages } = extractMessagesFromTasks(tasks);
    // Agent message with usage metadata gets tokenStats injected
    expect((messages[0].metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats)
      .toEqual({ total: 10, prompt: 3, completion: 7 });
    // User message is NOT enriched even if it carries usage metadata
    expect((messages[1].metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats)
      .toBeUndefined();
    // Agent message without usage metadata is passed through unchanged
    expect((messages[2].metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats)
      .toBeUndefined();
  });

  test('extractTokenStatsFromTasks sums usage across all history messages', () => {
    const tasks: any = [
      { history: [{ kind: 'message', metadata: { kagent_usage_metadata: { totalTokenCount: 10, promptTokenCount: 3, candidatesTokenCount: 7 } } }] },
      { history: [{ kind: 'message', metadata: { kagent_usage_metadata: { totalTokenCount: 12, promptTokenCount: 1, candidatesTokenCount: 9 } } }] },
    ];
    const stats = extractTokenStatsFromTasks(tasks);
    expect(stats.total).toBe(22);
    expect(stats.prompt).toBe(4);
    expect(stats.completion).toBe(16);
  });

  test('extractTokenStatsFromTasks skips history items without usage metadata', () => {
    const tasks = [
      { history: [{ kind: 'message', messageId: uuidv4(), role: 'agent', parts: [], metadata: { kagent_usage_metadata: { totalTokenCount: 10, promptTokenCount: 3, candidatesTokenCount: 7 } } }] },
      { history: [{ kind: 'message', messageId: uuidv4(), role: 'agent', parts: [], metadata: {} }] },
    ] as unknown as Task[];
    const stats = extractTokenStatsFromTasks(tasks);
    expect(stats.total).toBe(10);
    expect(stats.prompt).toBe(3);
    expect(stats.completion).toBe(7);
  });
});

describe('createMessageHandlers test', () => {
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

    handlers.handleMessageEvent(statusUpdateResp);

    expect(emitted.length).toBe(2);
    expect((emitted[0].metadata as any).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as any).originalType).toBe('ToolCallExecutionEvent');
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
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    const statusUpdateCall: any = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      status: { state: 'working', message: { role: 'agent', parts: [{ kind: 'data', data: { id: 'call_2', name: 'some_tool', args: { a: 1 } }, metadata: { kagent_type: 'function_call' } }] } }
    };
    handlers.handleMessageEvent(statusUpdateCall);

    const statusUpdateResp: any = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      status: { state: 'working', message: { role: 'agent', parts: [{ kind: 'data', data: { id: 'call_2', name: 'some_tool', response: { result: 'tool ok' } }, metadata: { kagent_type: 'function_response' } }] } }
    };
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
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    const statusWithText: any = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: true,
      status: { state: 'working', message: { role: 'agent', parts: [{ kind: 'text', text: 'hello' }] } }
    };
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
    handlers.handleMessageEvent(artifactEvent);

    // Expect: request, execution, summary (no text message since no text part)
    expect(emitted.length).toBe(3);
    expect((emitted[0].metadata as any).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as any).originalType).toBe('ToolCallExecutionEvent');
    expect((emitted[2].metadata as any).originalType).toBe('ToolCallSummaryMessage');
  });

  test('each invocation keeps its own token stats and session total accumulates correctly', () => {
    const emitted: Message[] = [];
    let capturedSessionTotal = { total: 0, prompt: 0, completion: 0 };
    const handlers = createMessageHandlers({
      setMessages: (updater) => {
        const next = updater(emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setSessionStats: (updater) => { capturedSessionTotal = updater(capturedSessionTotal); },
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    // Invocation 1: LLM decides to call a tool (usage arrives with the function_call)
    const toolCallUpdate = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      metadata: { kagent_usage_metadata: { totalTokenCount: 5, promptTokenCount: 3, candidatesTokenCount: 2 } },
      status: {
        state: 'working',
        message: {
          role: 'agent',
          parts: [{ kind: 'data', data: { id: 'call_1', name: 'my_tool', args: {} }, metadata: { kagent_type: 'function_call' } }]
        }
      }
    } as unknown as Message;
    handlers.handleMessageEvent(toolCallUpdate);

    // Tool executes and returns a result
    const toolResponseUpdate = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      status: {
        state: 'working',
        message: {
          role: 'agent',
          parts: [{ kind: 'data', data: { id: 'call_1', name: 'my_tool', response: { result: 'ok' } }, metadata: { kagent_type: 'function_response' } }]
        }
      }
    } as unknown as Message;
    handlers.handleMessageEvent(toolResponseUpdate);

    // Invocation 2: LLM generates the final text response
    const finalUpdate = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: true,
      metadata: { kagent_usage_metadata: { totalTokenCount: 10, promptTokenCount: 7, candidatesTokenCount: 3 } },
      status: {
        state: 'completed',
        message: { role: 'agent', parts: [{ kind: 'text', text: 'done' }] }
      }
    } as unknown as Message;
    handlers.handleMessageEvent(finalUpdate);

    const toolCallMsg = emitted.find(m => (m.metadata as ADKMetadata)?.originalType === 'ToolCallRequestEvent');
    const textMsg = emitted.find(m => (m.metadata as ADKMetadata)?.originalType === 'TextMessage');
    // Each invocation keeps its own stats — the tool call is not overwritten by the text response
    expect((toolCallMsg?.metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats).toEqual({ total: 5, prompt: 3, completion: 2 });
    expect((textMsg?.metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats).toEqual({ total: 10, prompt: 7, completion: 3 });
    // Session total accumulates both invocations
    expect(capturedSessionTotal).toEqual({ total: 15, prompt: 10, completion: 5 });
  });

  test('HITL interrupt accumulates pending turn stats and clears them', () => {
    const emitted: Message[] = [];
    let capturedSessionTotal: TokenStats = { total: 0, prompt: 0, completion: 0 };
    const handlers = createMessageHandlers({
      setMessages: (updater) => {
        const next = updater(emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setChatStatus: () => {},
      setSessionStats: (updater) => { capturedSessionTotal = updater(capturedSessionTotal); },
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    // Status update: LLM decides to call a confirmation tool (HITL), usage arrives here
    const hitlUpdate = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      metadata: { kagent_usage_metadata: { totalTokenCount: 8, promptTokenCount: 5, candidatesTokenCount: 3 } },
      status: {
        state: 'input-required',
        message: {
          role: 'agent',
          parts: [{
            kind: 'data',
            data: {
              name: 'adk_request_confirmation',
              id: 'confirm_1',
              args: { originalFunctionCall: { name: 'my_tool', args: { x: 1 }, id: 'call_1' } },
            },
            metadata: { kagent_type: 'function_call', kagent_is_long_running: true },
          }],
        },
      },
    } as unknown as Message;
    handlers.handleMessageEvent(hitlUpdate);

    // Session stats should be accumulated at the HITL boundary (not at stream end)
    expect(capturedSessionTotal).toEqual({ total: 8, prompt: 5, completion: 3 });
    // A ToolApprovalRequest message should have been emitted
    const approvalMsg = emitted.find(m => (m.metadata as ADKMetadata)?.originalType === 'ToolApprovalRequest');
    expect(approvalMsg).toBeDefined();
  });
});

describe('subagent_session_id propagation', () => {
  // Shared handler factory for status-update / artifact-update tests
  function makeHandlers() {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater) => {
        const next = updater(emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setChatStatus: () => {},
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });
    return { emitted, handlers };
  }

  test('status-update: agent function_call with kagent_subagent_session_id in DataPart metadata emits toolCallData with subagent_session_id', () => {
    const { emitted, handlers } = makeHandlers();

    const statusUpdateCall: any = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      status: {
        state: 'working',
        message: {
          role: 'agent',
          parts: [{
            kind: 'data',
            data: { id: 'agent_call_1', name: 'kagent__NS__k8s_agent', args: { request: 'list pods' } },
            metadata: { kagent_type: 'function_call', kagent_subagent_session_id: 'sess-abc-123' },
          }],
        },
      },
    };
    handlers.handleMessageEvent(statusUpdateCall);

    expect(emitted.length).toBe(1);
    const meta = emitted[0].metadata as ADKMetadata;
    expect(meta.originalType).toBe('ToolCallRequestEvent');
    expect(meta.toolCallData).toHaveLength(1);
    expect(meta.toolCallData![0].subagent_session_id).toBe('sess-abc-123');
  });

  test('status-update: agent function_response with subagent_session_id in response dict emits toolResultData with subagent_session_id', () => {
    const { emitted, handlers } = makeHandlers();

    const statusUpdateResp: any = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      status: {
        state: 'working',
        message: {
          role: 'agent',
          parts: [{
            kind: 'data',
            data: {
              id: 'agent_call_1',
              name: 'kagent__NS__k8s_agent',
              response: { result: 'done', subagent_session_id: 'sess-abc-123' },
            },
            metadata: { kagent_type: 'function_response' },
          }],
        },
      },
    };
    handlers.handleMessageEvent(statusUpdateResp);

    const execMsg = emitted.find(m => (m.metadata as ADKMetadata)?.originalType === 'ToolCallExecutionEvent');
    expect(execMsg).toBeDefined();
    const resultData = (execMsg!.metadata as ADKMetadata).toolResultData!;
    expect(resultData).toHaveLength(1);
    expect(resultData[0].subagent_session_id).toBe('sess-abc-123');
  });

  test('extractMessagesFromTasks: agent function_call DataPart with kagent_subagent_session_id emits toolCallData with subagent_session_id', () => {
    const tasks = [{
      contextId: 'ctx',
      id: 'task',
      history: [{
        kind: 'message',
        messageId: 'msg-1',
        role: 'agent',
        parts: [{
          kind: 'data',
          data: { id: 'agent_call_3', name: 'kagent__NS__k8s_agent', args: { request: 'list nodes' } },
          metadata: { kagent_type: 'function_call', kagent_subagent_session_id: 'sess-history-456' },
        }],
        metadata: {},
      }],
    }] as unknown as Task[];

    const { messages } = extractMessagesFromTasks(tasks);
    expect(messages).toHaveLength(1);
    const meta = messages[0].metadata as ADKMetadata;
    expect(meta.originalType).toBe('ToolCallRequestEvent');
    expect(meta.toolCallData).toHaveLength(1);
    expect(meta.toolCallData![0].subagent_session_id).toBe('sess-history-456');
  });

  test('extractMessagesFromTasks: agent function_response DataPart with subagent_session_id in response dict emits toolResultData with subagent_session_id', () => {
    const tasks = [{
      contextId: 'ctx',
      id: 'task',
      history: [{
        kind: 'message',
        messageId: 'msg-3',
        role: 'agent',
        parts: [{
          kind: 'data',
          data: {
            id: 'agent_call_3',
            name: 'kagent__NS__k8s_agent',
            response: { result: 'nodes listed', subagent_session_id: 'sess-history-456' },
          },
          metadata: { kagent_type: 'function_response' },
        }],
        metadata: {},
      }],
    }] as unknown as Task[];

    const { messages } = extractMessagesFromTasks(tasks);
    expect(messages).toHaveLength(1);
    const meta = messages[0].metadata as ADKMetadata;
    expect(meta.originalType).toBe('ToolCallExecutionEvent');
    expect(meta.toolResultData).toHaveLength(1);
    expect(meta.toolResultData![0].subagent_session_id).toBe('sess-history-456');
  });
});

describe('getMetadataValue', () => {
  test('reads kagent_ prefixed key', () => {
    expect(getMetadataValue({ kagent_type: 'function_call' }, 'type')).toBe('function_call');
  });

  test('reads adk_ prefixed key', () => {
    expect(getMetadataValue({ adk_type: 'function_call' }, 'type')).toBe('function_call');
  });

  test('adk_ takes priority over kagent_ when both present', () => {
    expect(getMetadataValue({ adk_type: 'adk_val', kagent_type: 'kagent_val' }, 'type')).toBe('adk_val');
  });

  test('returns undefined for missing key', () => {
    expect(getMetadataValue({ other: 'x' }, 'type')).toBeUndefined();
  });

  test('returns undefined for null/undefined metadata', () => {
    expect(getMetadataValue(null, 'type')).toBeUndefined();
    expect(getMetadataValue(undefined, 'type')).toBeUndefined();
  });

  test('returns falsy values correctly (not undefined)', () => {
    expect(getMetadataValue({ kagent_flag: false }, 'flag')).toBe(false);
    expect(getMetadataValue({ adk_count: 0 }, 'count')).toBe(0);
    expect(getMetadataValue({ kagent_text: '' }, 'text')).toBe('');
  });
});

describe('dual-prefix integration', () => {
  test('extractTokenStatsFromTasks works with adk_usage_metadata', () => {
    const tasks: any = [
      { history: [{ kind: 'message', metadata: { adk_usage_metadata: { totalTokenCount: 20, promptTokenCount: 8, candidatesTokenCount: 12 } } }] },
    ];
    const stats = extractTokenStatsFromTasks(tasks);
    expect(stats.total).toBe(20);
    expect(stats.prompt).toBe(8);
    expect(stats.completion).toBe(12);
  });

  test('status-update handler works with adk_type metadata on parts', () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater) => {
        const next = updater(emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setChatStatus: () => {},
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    const statusUpdateCall: any = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      status: {
        state: 'working',
        message: {
          role: 'agent',
          parts: [
            { kind: 'data', data: { id: 'call_adk', name: 'my_tool', args: { x: 1 } }, metadata: { adk_type: 'function_call' } },
          ],
        },
      },
    };
    handlers.handleMessageEvent(statusUpdateCall);

    const statusUpdateResp: any = {
      kind: 'status-update', contextId: 'ctx', taskId: 'task', final: false,
      status: {
        state: 'working',
        message: {
          role: 'agent',
          parts: [
            { kind: 'data', data: { id: 'call_adk', name: 'my_tool', response: { result: 'done' } }, metadata: { adk_type: 'function_response' } },
          ],
        },
      },
    };
    handlers.handleMessageEvent(statusUpdateResp);

    expect(emitted.length).toBe(2);
    expect((emitted[0].metadata as any).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as any).originalType).toBe('ToolCallExecutionEvent');
  });
});
