import { describe, test, expect, beforeAll } from '@jest/globals';
import { render, screen } from '@testing-library/react';
import type { Message } from '@a2a-js/sdk';

// TruncatableText pulls in react-markdown (ESM) which jest does not transform;
// stub it (and other heavy children not exercised here) to a plain renderer.
jest.mock('@/components/chat/TruncatableText', () => ({
  TruncatableText: ({ content }: { content: string }) => <div>{content}</div>,
}));
jest.mock('@/components/chat/ToolCallDisplay', () => ({
  __esModule: true,
  default: () => null,
}));
jest.mock('@/components/chat/AskUserDisplay', () => ({
  __esModule: true,
  default: () => null,
}));
jest.mock('@/components/chat/FeedbackDialog', () => ({
  FeedbackDialog: () => null,
}));

import ChatMessage from '@/components/chat/ChatMessage';

beforeAll(() => {
  global.URL.createObjectURL = jest.fn(() => 'blob:mock-url');
  global.URL.revokeObjectURL = jest.fn();
});

const PNG_B64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==';

function fileMessage(role: 'user' | 'agent'): Message {
  return {
    kind: 'message',
    messageId: `msg-${role}`,
    role,
    parts: [
      { kind: 'text', text: role === 'user' ? 'here you go' : 'here is your file' },
      { kind: 'file', file: { name: 'pic.png', mimeType: 'image/png', bytes: PNG_B64 } },
    ],
    metadata: {},
  };
}

describe('ChatMessage file rendering', () => {
  test('renders a file attachment in the user bubble', () => {
    render(<ChatMessage message={fileMessage('user')} allMessages={[]} />);
    expect(screen.getByAltText('pic.png')).toBeInTheDocument();
  });

  test('renders a file attachment in the agent bubble', () => {
    render(<ChatMessage message={fileMessage('agent')} allMessages={[]} />);
    expect(screen.getByAltText('pic.png')).toBeInTheDocument();
  });

  test('renders a file-only message (no text) instead of skipping it', () => {
    const msg: Message = {
      kind: 'message',
      messageId: 'file-only',
      role: 'agent',
      parts: [{ kind: 'file', file: { name: 'data.csv', mimeType: 'text/csv', bytes: 'YSxiCg==' } }],
      metadata: {},
    };
    render(<ChatMessage message={msg} allMessages={[]} />);
    expect(screen.getByText('data.csv')).toBeInTheDocument();
  });
});
