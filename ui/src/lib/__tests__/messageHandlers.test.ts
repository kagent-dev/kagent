import { describe, test, expect } from '@jest/globals';
import { v4 as uuidv4 } from 'uuid';
import { Role, TaskState, type Message, type Part, type StreamResponse, type Task } from '@a2a-js/sdk';
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
import { createDataPart, createMockMessage, createTextPart } from '@/mocks/factories';
import type { TokenStats } from '@/types';

type MessageUpdate = Message[] | ((messages: Message[]) => Message[]);
type StringUpdate = string | ((value: string) => string);
type TokenStatsUpdate = TokenStats | ((stats: TokenStats) => TokenStats);

function applyMessageUpdate(update: MessageUpdate, current: Message[]): Message[] {
  return typeof update === 'function' ? update(current) : update;
}

function applyStringUpdate(update: StringUpdate, current: string): string {
  return typeof update === 'function' ? update(current) : update;
}

function applyTokenStatsUpdate(update: TokenStatsUpdate, current: TokenStats): TokenStats {
  return typeof update === 'function' ? update(current) : update;
}

/** Test defaults for context/task ids; Part/Message shape comes from mocks/factories. */
function sdkMessage(overrides: Partial<Message> & Pick<Message, 'messageId'>): Message {
  return createMockMessage({
    role: Role.ROLE_AGENT,
    contextId: 'ctx',
    taskId: 'task',
    ...overrides,
  });
}

/** Minimal task fixture — unlike createMockTask, does not rewrite history metadata. */
function sdkTask(history: Message[], overrides: Partial<Task> = {}): Task {
  return {
    id: overrides.id ?? 'task',
    contextId: overrides.contextId ?? 'ctx',
    status: overrides.status,
    history,
    artifacts: overrides.artifacts ?? [],
    metadata: overrides.metadata,
  };
}

function statusUpdateEvent({
  contextId = 'ctx',
  taskId = 'task',
  state = TaskState.TASK_STATE_WORKING,
  message,
  metadata,
}: {
  contextId?: string;
  taskId?: string;
  state?: TaskState;
  message?: Message;
  metadata?: Record<string, unknown>;
}): StreamResponse {
  return {
    payload: {
      $case: 'statusUpdate',
      value: {
        contextId,
        taskId,
        status: {
          state,
          message,
          timestamp: new Date().toISOString(),
        },
        metadata,
      },
    },
  } as StreamResponse;
}

function artifactUpdateEvent({
  contextId = 'ctx',
  taskId = 'task',
  parts,
  metadata,
  artifactMetadata,
  lastChunk,
}: {
  contextId?: string;
  taskId?: string;
  parts: Part[];
  metadata?: Record<string, unknown>;
  artifactMetadata?: Record<string, unknown>;
  lastChunk?: boolean;
}): StreamResponse {
  return {
    payload: {
      $case: 'artifactUpdate',
      value: {
        contextId,
        taskId,
        metadata,
        artifact: {
          artifactId: 'artifact-1',
          name: 'artifact',
          description: '',
          parts,
          metadata: artifactMetadata,
          extensions: [],
        },
        append: false,
        ...(lastChunk !== undefined ? { lastChunk } : {}),
      },
    },
  } as unknown as StreamResponse;
}

describe('messageHandlers helpers', () => {
  test('normalizeToolResultToText handles string result', () => {
    const data: ToolResponseData = { id: '1', name: 'tool', response: { result: 'hello' } };
    expect(normalizeToolResultToText(data)).toBe('hello');
  });

  test('normalizeToolResultToText handles content array', () => {
    const data = {
      id: '1',
      name: 'tool',
      response: { result: { content: [{ text: 'a' }, { text: 'b' }] } },
    } as ToolResponseData;
    expect(normalizeToolResultToText(data)).toBe('ab');
  });

  test('normalizeToolResultToText handles object fallback', () => {
    const data = { id: '1', name: 'tool', response: { result: { foo: 'bar' } } } as ToolResponseData;
    expect(normalizeToolResultToText(data)).toContain('foo');
  });

  test('createMessage builds a message with metadata', () => {
    const msg = createMessage('hi', 'assistant', { originalType: 'TextMessage', contextId: 'ctx', taskId: 'task' });
    expect(msg.messageId).toBeTruthy();
    expect(msg.parts[0]?.content).toEqual({ $case: 'text', value: 'hi' });
    expect((msg.metadata as ADKMetadata).originalType).toBe('TextMessage');
    expect(msg.contextId).toBe('ctx');
    expect(msg.taskId).toBe('task');
  });

  test('extractMessagesFromTasks deduplicates messageIds', () => {
    const mId = uuidv4();
    const out = extractMessagesFromTasks([
      sdkTask([
        sdkMessage({ messageId: mId }),
        sdkMessage({ messageId: mId }),
      ]),
    ]);
    expect(out.length).toBe(1);
    expect(out[0].messageId).toBe(mId);
  });

  test('extractMessagesFromTasks injects tokenStats into non-user agent messages only', () => {
    const messages = extractMessagesFromTasks([
      sdkTask([
        sdkMessage({
          messageId: 'a1',
          metadata: {
            kagent_usage_metadata: { totalTokenCount: 10, promptTokenCount: 3, candidatesTokenCount: 7 },
          },
        }),
        sdkMessage({
          messageId: 'u1',
          role: Role.ROLE_USER,
          metadata: {
            kagent_usage_metadata: { totalTokenCount: 5, promptTokenCount: 2, candidatesTokenCount: 3 },
          },
        }),
        sdkMessage({ messageId: 'a2', metadata: {} }),
      ]),
    ]);
    expect((messages[0].metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats)
      .toEqual({ total: 10, prompt: 3, completion: 7 });
    expect((messages[1].metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats)
      .toBeUndefined();
    expect((messages[2].metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats)
      .toBeUndefined();
  });

  test('extractTokenStatsFromTasks sums usage across all history messages', () => {
    const stats = extractTokenStatsFromTasks([
      sdkTask([
        sdkMessage({
          messageId: 'm1',
          metadata: { kagent_usage_metadata: { totalTokenCount: 10, promptTokenCount: 3, candidatesTokenCount: 7 } },
        }),
      ]),
      sdkTask([
        sdkMessage({
          messageId: 'm2',
          metadata: { kagent_usage_metadata: { totalTokenCount: 12, promptTokenCount: 1, candidatesTokenCount: 9 } },
        }),
      ]),
    ]);
    expect(stats.total).toBe(22);
    expect(stats.prompt).toBe(4);
    expect(stats.completion).toBe(16);
  });

  test('extractTokenStatsFromTasks skips history items without usage metadata', () => {
    const stats = extractTokenStatsFromTasks([
      sdkTask([
        sdkMessage({
          messageId: uuidv4(),
          metadata: { kagent_usage_metadata: { totalTokenCount: 10, promptTokenCount: 3, candidatesTokenCount: 7 } },
        }),
      ]),
      sdkTask([
        sdkMessage({ messageId: uuidv4(), metadata: {} }),
      ]),
    ]);
    expect(stats.total).toBe(10);
    expect(stats.prompt).toBe(3);
    expect(stats.completion).toBe(7);
  });
});

describe('createMessageHandlers test', () => {
  test('emits ToolCallRequestEvent + ToolCallExecutionEvent for agent tool', () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater: MessageUpdate) => {
        const next = applyMessageUpdate(updater, emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setChatStatus: () => {},
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    handlers.handleMessageEvent(statusUpdateEvent({
      message: sdkMessage({
        messageId: 'call-msg',
        parts: [createDataPart(
          { id: 'call_1', name: 'kagent__NS__k8s_agent', args: { request: 'list' } },
          { kagent_type: 'function_call' },
        )],
      }),
    }));

    handlers.handleMessageEvent(statusUpdateEvent({
      message: sdkMessage({
        messageId: 'resp-msg',
        parts: [createDataPart(
          { id: 'call_1', name: 'kagent__NS__k8s_agent', response: { result: 'ok' } },
          { kagent_type: 'function_response' },
        )],
      }),
    }));

    expect(emitted.length).toBe(2);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as ADKMetadata).originalType).toBe('ToolCallExecutionEvent');
  });

  test('emits ToolCallRequestEvent + ToolCallExecutionEvent for non-agent tool', () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater: MessageUpdate) => {
        const next = applyMessageUpdate(updater, emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    handlers.handleMessageEvent(statusUpdateEvent({
      message: sdkMessage({
        messageId: 'call-2-msg',
        parts: [createDataPart(
          { id: 'call_2', name: 'some_tool', args: { a: 1 } },
          { kagent_type: 'function_call' },
        )],
      }),
    }));

    handlers.handleMessageEvent(statusUpdateEvent({
      message: sdkMessage({
        messageId: 'resp-2-msg',
        parts: [createDataPart(
          { id: 'call_2', name: 'some_tool', response: { result: 'tool ok' } },
          { kagent_type: 'function_response' },
        )],
      }),
    }));

    expect(emitted.length).toBe(2);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as ADKMetadata).originalType).toBe('ToolCallExecutionEvent');
  });

  test('terminal status-update with text part commits TextMessage', () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater: MessageUpdate) => {
        const next = applyMessageUpdate(updater, emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    handlers.handleMessageEvent(statusUpdateEvent({
      state: TaskState.TASK_STATE_COMPLETED,
      message: sdkMessage({
        messageId: 'final-msg',
        parts: [createTextPart('hello')],
      }),
    }));

    expect(emitted.length).toBe(1);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('TextMessage');
    expect(emitted[0].parts[0]?.content?.value).toBe('hello');
  });

  test('artifact-update converts tool parts and appends summary', () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater: MessageUpdate) => {
        const next = applyMessageUpdate(updater, emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    handlers.handleMessageEvent(artifactUpdateEvent({
      lastChunk: true,
      parts: [
        createDataPart({ id: 'call_3', name: 'some_tool', args: { q: 1 } }, { kagent_type: 'function_call' }),
        createDataPart({ id: 'call_3', name: 'some_tool', response: { result: 'out' } }, { kagent_type: 'function_response' }),
      ],
    }));

    expect(emitted.length).toBe(3);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as ADKMetadata).originalType).toBe('ToolCallExecutionEvent');
    expect((emitted[2].metadata as ADKMetadata).originalType).toBe('ToolCallSummaryMessage');
  });

  test('artifact-update drops a model-internal thoughtSignature data part', () => {
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

    // Gemini returns the answer text and its encrypted reasoning handle as two
    // parts. The signature part is unlabeled, so it used to fall through to the
    // JSON.stringify fallback and get concatenated onto the answer.
    handlers.handleMessageEvent(artifactUpdateEvent({
      lastChunk: true,
      parts: [
        createTextPart('Cilium is on v1.19.5.'),
        createDataPart({ thoughtSignature: 'EjQKMgERTTIPW1Dx9s3NlDDSMzmWhbt5' }),
      ],
    }));

    // TextMessage + the summary that lastChunk always appends
    expect(emitted.length).toBe(2);
    expect((emitted[0].metadata as any).originalType).toBe('TextMessage');
    expect((emitted[0].parts[0] as any).content.value).toBe('Cilium is on v1.19.5.');
  });

  test('artifact-update keeps unlabeled data parts that are not model-internal', () => {
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

    handlers.handleMessageEvent(artifactUpdateEvent({
      lastChunk: true,
      parts: [
        createTextPart('result: '),
        createDataPart({ rows: 2 }),
      ],
    }));

    expect(emitted.length).toBe(2);
    expect((emitted[0].metadata as any).originalType).toBe('TextMessage');
    expect((emitted[0].parts[0] as any).content.value).toBe('result: {"rows":2}');
  });

  test('Go ADK streaming flow: partial chunks stream, non-partial artifact emits message, empty sentinel ignored', () => {
    const emitted: Message[] = [];
    let streamingContent = '';
    const handlers = createMessageHandlers({
      setMessages: (updater: MessageUpdate) => {
        const next = applyMessageUpdate(updater, emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: (updater: StringUpdate) => { streamingContent = applyStringUpdate(updater, streamingContent); },
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    for (const text of ['I am', ' a simple agent.']) {
      handlers.handleMessageEvent(artifactUpdateEvent({
        metadata: { adk_partial: true },
        artifactMetadata: { adk_partial: true },
        parts: [createTextPart(text, { adk_partial: true })],
      }));
    }
    expect(streamingContent).toBe('I am a simple agent.');
    expect(emitted.length).toBe(0);

    handlers.handleMessageEvent(artifactUpdateEvent({
      metadata: { adk_partial: false },
      parts: [createTextPart('I am a simple agent.')],
    }));
    expect(streamingContent).toBe('');
    expect(emitted.length).toBe(1);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('TextMessage');
    expect(emitted[0].parts[0]?.content?.value).toBe('I am a simple agent.');

    handlers.handleMessageEvent(artifactUpdateEvent({
      lastChunk: true,
      metadata: { adk_partial: true },
      artifactMetadata: { adk_partial: true },
      parts: [createDataPart({}, { adk_partial: true })],
    }));
    const textMessages = emitted.filter((message) => (message.metadata as ADKMetadata)?.originalType === 'TextMessage');
    expect(textMessages.length).toBe(1);
    expect(textMessages.some((message) => message.parts[0]?.content?.value === '{}')).toBe(false);
  });

  test('each invocation keeps its own token stats and session total accumulates correctly', () => {
    const emitted: Message[] = [];
    let capturedSessionTotal = { total: 0, prompt: 0, completion: 0 };
    const handlers = createMessageHandlers({
      setMessages: (updater: MessageUpdate) => {
        const next = applyMessageUpdate(updater, emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setSessionStats: (updater: TokenStatsUpdate) => { capturedSessionTotal = applyTokenStatsUpdate(updater, capturedSessionTotal); },
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    handlers.handleMessageEvent(statusUpdateEvent({
      metadata: { kagent_usage_metadata: { totalTokenCount: 5, promptTokenCount: 3, candidatesTokenCount: 2 } },
      message: sdkMessage({
        messageId: 'tool-call-msg',
        parts: [createDataPart({ id: 'call_1', name: 'my_tool', args: {} }, { kagent_type: 'function_call' })],
      }),
    }));

    handlers.handleMessageEvent(statusUpdateEvent({
      message: sdkMessage({
        messageId: 'tool-response-msg',
        parts: [createDataPart({ id: 'call_1', name: 'my_tool', response: { result: 'ok' } }, { kagent_type: 'function_response' })],
      }),
    }));

    handlers.handleMessageEvent(statusUpdateEvent({
      state: TaskState.TASK_STATE_COMPLETED,
      metadata: { kagent_usage_metadata: { totalTokenCount: 10, promptTokenCount: 7, candidatesTokenCount: 3 } },
      message: sdkMessage({
        messageId: 'final-text-msg',
        parts: [createTextPart('done')],
      }),
    }));

    const toolCallMsg = emitted.find((message) => (message.metadata as ADKMetadata)?.originalType === 'ToolCallRequestEvent');
    const textMsg = emitted.find((message) => (message.metadata as ADKMetadata)?.originalType === 'TextMessage');
    expect((toolCallMsg?.metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats).toEqual({ total: 5, prompt: 3, completion: 2 });
    expect((textMsg?.metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats).toEqual({ total: 10, prompt: 7, completion: 3 });
    expect(capturedSessionTotal).toEqual({ total: 15, prompt: 10, completion: 5 });
  });

  test('HITL interrupt accumulates pending turn stats and clears them', () => {
    const emitted: Message[] = [];
    let capturedSessionTotal: TokenStats = { total: 0, prompt: 0, completion: 0 };
    const handlers = createMessageHandlers({
      setMessages: (updater: MessageUpdate) => {
        const next = applyMessageUpdate(updater, emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setChatStatus: () => {},
      setSessionStats: (updater: TokenStatsUpdate) => { capturedSessionTotal = applyTokenStatsUpdate(updater, capturedSessionTotal); },
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    handlers.handleMessageEvent(statusUpdateEvent({
      state: TaskState.TASK_STATE_INPUT_REQUIRED,
      metadata: { kagent_usage_metadata: { totalTokenCount: 8, promptTokenCount: 5, candidatesTokenCount: 3 } },
      message: sdkMessage({
        messageId: 'hitl-msg',
        parts: [createDataPart(
          {
            name: 'adk_request_confirmation',
            id: 'confirm_1',
            args: { originalFunctionCall: { name: 'my_tool', args: { x: 1 }, id: 'call_1' } },
          },
          { kagent_type: 'function_call', kagent_is_long_running: true },
        )],
      }),
    }));

    expect(capturedSessionTotal).toEqual({ total: 8, prompt: 5, completion: 3 });
    const approvalMsg = emitted.find((message) => (message.metadata as ADKMetadata)?.originalType === 'ToolApprovalRequest');
    expect(approvalMsg).toBeDefined();
  });
});

describe('subagent_session_id propagation', () => {
  function makeHandlers() {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater: MessageUpdate) => {
        const next = applyMessageUpdate(updater, emitted);
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

    handlers.handleMessageEvent(statusUpdateEvent({
      message: sdkMessage({
        messageId: 'agent-call-msg',
        parts: [createDataPart(
          { id: 'agent_call_1', name: 'kagent__NS__k8s_agent', args: { request: 'list pods' } },
          { kagent_type: 'function_call', kagent_subagent_session_id: 'sess-abc-123' },
        )],
      }),
    }));

    expect(emitted.length).toBe(1);
    const meta = emitted[0].metadata as ADKMetadata;
    expect(meta.originalType).toBe('ToolCallRequestEvent');
    expect(meta.toolCallData).toHaveLength(1);
    expect(meta.toolCallData?.[0]?.subagent_session_id).toBe('sess-abc-123');
  });

  test('status-update: agent function_response with subagent_session_id in response dict emits toolResultData with subagent_session_id', () => {
    const { emitted, handlers } = makeHandlers();

    handlers.handleMessageEvent(statusUpdateEvent({
      message: sdkMessage({
        messageId: 'agent-response-msg',
        parts: [createDataPart(
          {
            id: 'agent_call_1',
            name: 'kagent__NS__k8s_agent',
            response: { result: 'done', subagent_session_id: 'sess-abc-123' },
          },
          { kagent_type: 'function_response' },
        )],
      }),
    }));

    const execMsg = emitted.find((message) => (message.metadata as ADKMetadata)?.originalType === 'ToolCallExecutionEvent');
    expect(execMsg).toBeDefined();
    const resultData = (execMsg?.metadata as ADKMetadata).toolResultData ?? [];
    expect(resultData).toHaveLength(1);
    expect(resultData[0]?.subagent_session_id).toBe('sess-abc-123');
  });

  test('extractMessagesFromTasks: agent function_call DataPart with kagent_subagent_session_id emits toolCallData with subagent_session_id', () => {
    const messages = extractMessagesFromTasks([
      sdkTask([
        sdkMessage({
          messageId: 'msg-1',
          parts: [createDataPart(
            { id: 'agent_call_3', name: 'kagent__NS__k8s_agent', args: { request: 'list nodes' } },
            { kagent_type: 'function_call', kagent_subagent_session_id: 'sess-history-456' },
          )],
          metadata: {},
        }),
      ]),
    ]);

    expect(messages).toHaveLength(1);
    const meta = messages[0].metadata as ADKMetadata;
    expect(meta.originalType).toBe('ToolCallRequestEvent');
    expect(meta.toolCallData).toHaveLength(1);
    expect(meta.toolCallData?.[0]?.subagent_session_id).toBe('sess-history-456');
  });

  test('extractMessagesFromTasks: agent function_response DataPart with subagent_session_id in response dict emits toolResultData with subagent_session_id', () => {
    const messages = extractMessagesFromTasks([
      sdkTask([
        sdkMessage({
          messageId: 'msg-3',
          parts: [createDataPart(
            {
              id: 'agent_call_3',
              name: 'kagent__NS__k8s_agent',
              response: { result: 'nodes listed', subagent_session_id: 'sess-history-456' },
            },
            { kagent_type: 'function_response' },
          )],
          metadata: {},
        }),
      ]),
    ]);

    expect(messages).toHaveLength(1);
    const meta = messages[0].metadata as ADKMetadata;
    expect(meta.originalType).toBe('ToolCallExecutionEvent');
    expect(meta.toolResultData).toHaveLength(1);
    expect(meta.toolResultData?.[0]?.subagent_session_id).toBe('sess-history-456');
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
    const stats = extractTokenStatsFromTasks([
      sdkTask([
        sdkMessage({
          messageId: 'adk-usage-msg',
          metadata: { adk_usage_metadata: { totalTokenCount: 20, promptTokenCount: 8, candidatesTokenCount: 12 } },
        }),
      ]),
    ]);
    expect(stats.total).toBe(20);
    expect(stats.prompt).toBe(8);
    expect(stats.completion).toBe(12);
  });

  test('status-update handler works with adk_type metadata on parts', () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater: MessageUpdate) => {
        const next = applyMessageUpdate(updater, emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setChatStatus: () => {},
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    handlers.handleMessageEvent(statusUpdateEvent({
      message: sdkMessage({
        messageId: 'adk-call-msg',
        parts: [createDataPart({ id: 'call_adk', name: 'my_tool', args: { x: 1 } }, { adk_type: 'function_call' })],
      }),
    }));

    handlers.handleMessageEvent(statusUpdateEvent({
      message: sdkMessage({
        messageId: 'adk-response-msg',
        parts: [createDataPart({ id: 'call_adk', name: 'my_tool', response: { result: 'done' } }, { adk_type: 'function_response' })],
      }),
    }));

    expect(emitted.length).toBe(2);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as ADKMetadata).originalType).toBe('ToolCallExecutionEvent');
  });
});
