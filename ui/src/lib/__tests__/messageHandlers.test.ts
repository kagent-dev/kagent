import { describe, test, expect } from '@jest/globals';
import { v4 as uuidv4 } from 'uuid';
import { Role, TaskState, type Artifact, type Message, type Part, type StreamResponse, type Task } from '@a2a-js/sdk';
import {
  extractMessagesFromTasks,
  extractApprovalMessagesFromTasks,
  extractTokenStatsFromTasks,
  collectTerminalTaskIds,
  collectTaskTokenStats,
  isFinishedAssistantReply,
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

function sdkArtifact(
  artifactId: string,
  parts: Part[],
  metadata?: Record<string, unknown>,
): Artifact {
  return {
    artifactId,
    name: '',
    description: '',
    parts,
    metadata,
    extensions: [],
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
  append = false,
  artifactId = 'artifact-1',
}: {
  contextId?: string;
  taskId?: string;
  parts: Part[];
  metadata?: Record<string, unknown>;
  artifactMetadata?: Record<string, unknown>;
  lastChunk?: boolean;
  append?: boolean;
  artifactId?: string;
}): StreamResponse {
  return {
    payload: {
      $case: 'artifactUpdate',
      value: {
        contextId,
        taskId,
        metadata,
        artifact: {
          artifactId,
          name: 'artifact',
          description: '',
          parts,
          metadata: artifactMetadata,
          extensions: [],
        },
        append,
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
        sdkMessage({ messageId: mId, role: Role.ROLE_USER }),
        sdkMessage({ messageId: mId, role: Role.ROLE_USER }),
      ]),
    ]);
    expect(out.length).toBe(1);
    expect(out[0].messageId).toBe(mId);
  });

  test('extractMessagesFromTasks reconstructs text and tool output from persisted artifacts', () => {
    const messages = extractMessagesFromTasks([
      sdkTask([
        sdkMessage({ messageId: 'u1', role: Role.ROLE_USER, parts: [createTextPart('calculate')] }),
      ], {
        status: { state: TaskState.TASK_STATE_COMPLETED, timestamp: new Date().toISOString(), message: undefined },
        artifacts: [
          sdkArtifact('tool-call', [
            createDataPart({ id: 'call-1', name: 'calculator', args: { expression: '2+2' } }, { adk_type: 'function_call' }),
          ]),
          sdkArtifact('tool-result', [
            createDataPart({ id: 'call-1', name: 'calculator', response: { result: 4 } }, { adk_type: 'function_response' }),
          ]),
          sdkArtifact('answer', [createTextPart('The answer is 4.')], {
            adk_author: 'calculator-agent',
            adk_usage_metadata: { totalTokenCount: 17, promptTokenCount: 12, candidatesTokenCount: 5 },
          }),
        ],
      }),
    ]);

    expect(messages).toHaveLength(4);
    expect((messages[1].metadata as ADKMetadata).originalType).toBe('ToolCallRequestEvent');
    expect((messages[2].metadata as ADKMetadata).originalType).toBe('ToolCallExecutionEvent');
    expect((messages[3].metadata as ADKMetadata).originalType).toBe('TextMessage');
    expect(messages[3].parts[0]?.content?.value).toBe('The answer is 4.');

    const task = sdkTask([], {
      id: 'task',
      status: { state: TaskState.TASK_STATE_COMPLETED, timestamp: new Date().toISOString(), message: undefined },
      artifacts: [
        sdkArtifact('answer', [createTextPart('The answer is 4.')], {
          adk_usage_metadata: { totalTokenCount: 17, promptTokenCount: 12, candidatesTokenCount: 5 },
        }),
      ],
    });
    const terminalIds = collectTerminalTaskIds([task]);
    const tokenStats = collectTaskTokenStats([task]);
    expect(isFinishedAssistantReply(messages[3], messages, terminalIds)).toBe(true);
    expect(isFinishedAssistantReply(messages[1], messages, terminalIds)).toBe(false);
    expect(tokenStats.get('task')).toEqual({ total: 17, prompt: 12, completion: 5 });
  });

  test('reloaded input-required task renders user input, artifacts, then the current approval', () => {
    const confirmation = createDataPart(
      {
        id: 'confirm-1',
        name: 'adk_request_confirmation',
        args: {
          originalFunctionCall: { id: 'call-logs', name: 'k8s_get_pod_logs', args: { pod: 'x' } },
          toolConfirmation: { confirmed: false },
        },
      },
      { adk_type: 'function_call', adk_is_long_running: true },
    );
    const task = sdkTask([
      sdkMessage({ messageId: 'u1', role: Role.ROLE_USER, parts: [createTextPart('logs')] }),
    ], {
      status: {
        state: TaskState.TASK_STATE_INPUT_REQUIRED,
        timestamp: new Date().toISOString(),
        message: sdkMessage({ messageId: 'c1', role: Role.ROLE_AGENT, parts: [confirmation] }),
      },
      artifacts: [
        sdkArtifact('mixed', [
          createTextPart('Fetching logs now.'),
          createDataPart({ id: 'call-logs', name: 'k8s_get_pod_logs', args: { pod: 'x' } }, { adk_type: 'function_call' }),
        ], { adk_usage_metadata: { promptTokenCount: 10, candidatesTokenCount: 2 } }),
        sdkArtifact('final', [createTextPart('Done.')], {
          adk_usage_metadata: { promptTokenCount: 20, candidatesTokenCount: 4 },
        }),
      ],
    });
    const artifactMessages = extractMessagesFromTasks([task]);
    const { messages: approvalMessages, hasPendingApproval } = extractApprovalMessagesFromTasks([task]);
    const messages = [...artifactMessages, ...approvalMessages];

    const types = messages.map(m => (m.metadata as ADKMetadata)?.originalType ?? m.role);
    expect(types).toEqual([
      Role.ROLE_USER,
      'TextMessage',
      'ToolCallRequestEvent',
      'TextMessage',
      'ToolApprovalRequest',
    ]);
    expect(messages[1].parts[0]?.content?.value).toBe('Fetching logs now.');
    expect(hasPendingApproval).toBe(true);
    expect(collectTaskTokenStats([task]).get('task')).toEqual({ total: 24, prompt: 20, completion: 4 });
  });

  test('reloaded completed task anchors resolved HITL before its artifact response', () => {
    const confirmation = createDataPart(
      {
        id: 'confirm-1',
        name: 'adk_request_confirmation',
        args: {
          originalFunctionCall: { id: 'call-delete', name: 'delete_pod', args: { pod: 'x' } },
          toolConfirmation: { confirmed: false },
        },
      },
      { adk_type: 'function_call', adk_is_long_running: true },
    );
    const task = sdkTask([
      sdkMessage({ messageId: 'u1', role: Role.ROLE_USER, parts: [createTextPart('delete pod x')] }),
      sdkMessage({ messageId: 'c1', role: Role.ROLE_AGENT, parts: [confirmation] }),
      sdkMessage({
        messageId: 'd1',
        role: Role.ROLE_USER,
        parts: [createDataPart({ decision_type: 'approve' })],
      }),
    ], {
      status: { state: TaskState.TASK_STATE_COMPLETED, timestamp: new Date().toISOString(), message: undefined },
      artifacts: [
        sdkArtifact('before-hitl', [createTextPart('Pod x needs approval.')]),
        sdkArtifact('tool-result', [
          createDataPart(
            { id: 'call-delete', name: 'delete_pod', response: { result: 'deleted' } },
            { adk_type: 'function_response' },
          ),
        ]),
        sdkArtifact('answer', [createTextPart('Pod x was deleted.')]),
      ],
    });

    const messages = extractMessagesFromTasks([task]);
    expect(messages.map(message => (message.metadata as ADKMetadata)?.originalType ?? message.role)).toEqual([
      Role.ROLE_USER,
      'TextMessage',
      'ToolApprovalRequest',
      'ToolCallExecutionEvent',
      'TextMessage',
    ]);
    expect((messages[2].metadata as ADKMetadata).approvalDecision).toBe('approve');
  });

  test('extractTokenStatsFromTasks uses last usage per task and falls back total', () => {
    const stats = extractTokenStatsFromTasks([
      sdkTask([], { artifacts: [
        sdkArtifact('a1', [], {
          kagent_usage_metadata: { promptTokenCount: 3, candidatesTokenCount: 7 },
        }),
        sdkArtifact('a2', [], {
          kagent_usage_metadata: { promptTokenCount: 10, candidatesTokenCount: 9 },
        }),
      ] }),
      sdkTask([], { artifacts: [sdkArtifact('b1', [], {
        kagent_usage_metadata: { totalTokenCount: 12, promptTokenCount: 1, candidatesTokenCount: 9 },
      })] }),
    ]);
    // task1 last=19 (10+9 fallback), task2=12
    expect(stats.total).toBe(31);
    expect(stats.prompt).toBe(11);
    expect(stats.completion).toBe(18);
  });

  test('extractTokenStatsFromTasks includes usage stored on artifacts', () => {
    const stats = extractTokenStatsFromTasks([
      sdkTask([], {
        artifacts: [sdkArtifact('answer', [createTextPart('answer')], {
          adk_usage_metadata: { totalTokenCount: 17, promptTokenCount: 12, candidatesTokenCount: 5 },
        })],
      }),
    ]);

    expect(stats).toEqual({ total: 17, prompt: 12, completion: 5 });
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

    handlers.handleMessageEvent(artifactUpdateEvent({
      parts: [createDataPart(
          { id: 'call_1', name: 'kagent__NS__k8s_agent', args: { request: 'list' } },
          { kagent_type: 'function_call' },
        )],
    }));

    handlers.handleMessageEvent(artifactUpdateEvent({
      parts: [createDataPart(
          { id: 'call_1', name: 'kagent__NS__k8s_agent', response: { result: 'ok' } },
          { kagent_type: 'function_response' },
        )],
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

    handlers.handleMessageEvent(artifactUpdateEvent({
      parts: [createDataPart(
          { id: 'call_2', name: 'some_tool', args: { a: 1 } },
          { kagent_type: 'function_call' },
        )],
    }));

    handlers.handleMessageEvent(artifactUpdateEvent({
      parts: [createDataPart(
          { id: 'call_2', name: 'some_tool', response: { result: 'tool ok' } },
          { kagent_type: 'function_response' },
        )],
    }));

    expect(emitted.length).toBe(2);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as ADKMetadata).originalType).toBe('ToolCallExecutionEvent');
  });



  test('non-final artifact-update converts tool parts immediately', () => {
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
      parts: [
        createDataPart({ id: 'call_3', name: 'some_tool', args: { q: 1 } }, { kagent_type: 'function_call' }),
        createDataPart({ id: 'call_3', name: 'some_tool', response: { result: 'out' } }, { kagent_type: 'function_response' }),
      ],
    }));

    expect(emitted.length).toBe(2);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as ADKMetadata).originalType).toBe('ToolCallExecutionEvent');
  });

  test('artifact-update drops a model-internal thoughtSignature data part', () => {
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

    expect(emitted).toHaveLength(1);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('TextMessage');
    expect(emitted[0].parts[0]?.content?.value).toBe('Cilium is on v1.19.5.');
  });

  test('artifact-update keeps unlabeled data parts that are not model-internal', () => {
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
        createTextPart('result: '),
        createDataPart({ rows: 2 }),
      ],
    }));

    expect(emitted).toHaveLength(1);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('TextMessage');
    expect(emitted[0].parts[0]?.content?.value).toBe('result: {"rows":2}');
  });

  test('content-bearing lastChunk replaces partials and commits the final artifact text', () => {
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

    handlers.handleMessageEvent(artifactUpdateEvent({ parts: [createTextPart('I am')] }));
    handlers.handleMessageEvent(artifactUpdateEvent({ append: true, parts: [createTextPart(' a simple agent.')] }));
    expect(streamingContent).toBe('I am a simple agent.');
    expect(emitted.length).toBe(0);

    handlers.handleMessageEvent(artifactUpdateEvent({
      lastChunk: true,
      parts: [createTextPart('I am a simple agent.')],
    }));
    expect(streamingContent).toBe('');
    expect(emitted).toHaveLength(1);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('TextMessage');
    expect(emitted[0].parts[0]?.content?.value).toBe('I am a simple agent.');
  });

  test('latest task usage wins for session total; terminal task is reported without writing message metadata', () => {
    const emitted: Message[] = [];
    let capturedSessionTotal = { total: 0, prompt: 0, completion: 0 };
    const terminalTasks: Array<{ taskId: string; stats?: TokenStats }> = [];
    const handlers = createMessageHandlers({
      setMessages: (updater: MessageUpdate) => {
        const next = applyMessageUpdate(updater, emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
      setSessionStats: (updater: TokenStatsUpdate) => { capturedSessionTotal = applyTokenStatsUpdate(updater, capturedSessionTotal); },
      onTerminalTask: (taskId, tokenStats) => { terminalTasks.push({ taskId, stats: tokenStats }); },
      agentContext: { namespace: 'kagent', agentName: 'testagent' },
    });

    handlers.handleMessageEvent(statusUpdateEvent({
      metadata: { kagent_usage_metadata: { totalTokenCount: 5, promptTokenCount: 3, candidatesTokenCount: 2 } },
    }));

    handlers.handleMessageEvent(artifactUpdateEvent({
      parts: [createDataPart({ id: 'call_1', name: 'my_tool', args: {} }, { kagent_type: 'function_call' })],
    }));

    handlers.handleMessageEvent(artifactUpdateEvent({
      parts: [createDataPart({ id: 'call_1', name: 'my_tool', response: { result: 'ok' } }, { kagent_type: 'function_response' })],
    }));

    handlers.handleMessageEvent(statusUpdateEvent({
      metadata: { kagent_usage_metadata: { totalTokenCount: 10, promptTokenCount: 7, candidatesTokenCount: 3 } },
    }));
    handlers.handleMessageEvent(artifactUpdateEvent({
      lastChunk: true,
      parts: [createTextPart('done')],
    }));
    handlers.handleMessageEvent(statusUpdateEvent({
      state: TaskState.TASK_STATE_COMPLETED,
    }));

    const toolCallMsg = emitted.find((message) => (message.metadata as ADKMetadata)?.originalType === 'ToolCallRequestEvent');
    const textMsg = emitted.find((message) => (message.metadata as ADKMetadata)?.originalType === 'TextMessage');
    expect((toolCallMsg?.metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats).toEqual({ total: 10, prompt: 7, completion: 3 });
    expect((textMsg?.metadata as ADKMetadata & { tokenStats?: TokenStats })?.tokenStats).toBeUndefined();
    expect(terminalTasks).toEqual([{ taskId: 'task', stats: { total: 10, prompt: 7, completion: 3 } }]);
    expect(isFinishedAssistantReply(textMsg!, emitted, new Set(['task']))).toBe(true);
    expect(capturedSessionTotal).toEqual({ total: 10, prompt: 7, completion: 3 });
  });

  test('mixed text+tool artifact preserves text-before-tool order', () => {
    const emitted: Message[] = [];
    const handlers = createMessageHandlers({
      setMessages: (updater: MessageUpdate) => {
        const next = applyMessageUpdate(updater, emitted);
        emitted.length = 0;
        emitted.push(...next);
      },
      setIsStreaming: () => {},
      setStreamingContent: () => {},
    });

    handlers.handleMessageEvent(artifactUpdateEvent({
      lastChunk: true,
      parts: [
        createTextPart('Fetching logs now.'),
        createDataPart({ id: 'call_1', name: 'k8s_get_pod_logs', args: {} }, { kagent_type: 'function_call' }),
      ],
    }));

    expect(emitted).toHaveLength(2);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('TextMessage');
    expect(emitted[0].parts[0]?.content?.value).toBe('Fetching logs now.');
    expect((emitted[1].metadata as ADKMetadata).originalType).toBe('ToolCallRequestEvent');
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

  test('artifact-update: agent function_response with subagent_session_id in response dict emits toolResultData with subagent_session_id', () => {
    const { emitted, handlers } = makeHandlers();

    handlers.handleMessageEvent(artifactUpdateEvent({
      parts: [createDataPart(
          {
            id: 'agent_call_1',
            name: 'kagent__NS__k8s_agent',
            response: { result: 'done', subagent_session_id: 'sess-abc-123' },
          },
          { kagent_type: 'function_response' },
        )],
    }));

    const execMsg = emitted.find((message) => (message.metadata as ADKMetadata)?.originalType === 'ToolCallExecutionEvent');
    expect(execMsg).toBeDefined();
    const resultData = (execMsg?.metadata as ADKMetadata).toolResultData ?? [];
    expect(resultData).toHaveLength(1);
    expect(resultData[0]?.subagent_session_id).toBe('sess-abc-123');
  });

  test('extractMessagesFromTasks: artifact function_response preserves subagent_session_id', () => {
    const messages = extractMessagesFromTasks([
      sdkTask([], {
        artifacts: [sdkArtifact('agent-response', [createDataPart(
            {
              id: 'agent_call_3',
              name: 'kagent__NS__k8s_agent',
              response: { result: 'nodes listed', subagent_session_id: 'sess-history-456' },
            },
            { kagent_type: 'function_response' },
          )])],
      }),
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
      sdkTask([], {
        artifacts: [sdkArtifact('adk-usage-artifact', [], {
          adk_usage_metadata: { totalTokenCount: 20, promptTokenCount: 8, candidatesTokenCount: 12 },
        })],
      }),
    ]);
    expect(stats.total).toBe(20);
    expect(stats.prompt).toBe(8);
    expect(stats.completion).toBe(12);
  });

  test('artifact-update handler works with adk_type metadata on parts', () => {
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

    handlers.handleMessageEvent(artifactUpdateEvent({
      parts: [createDataPart({ id: 'call_adk', name: 'my_tool', args: { x: 1 } }, { adk_type: 'function_call' })],
    }));

    handlers.handleMessageEvent(artifactUpdateEvent({
      parts: [createDataPart({ id: 'call_adk', name: 'my_tool', response: { result: 'done' } }, { adk_type: 'function_response' })],
    }));

    expect(emitted.length).toBe(2);
    expect((emitted[0].metadata as ADKMetadata).originalType).toBe('ToolCallRequestEvent');
    expect((emitted[1].metadata as ADKMetadata).originalType).toBe('ToolCallExecutionEvent');
  });
});
