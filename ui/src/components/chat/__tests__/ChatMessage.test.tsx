import { describe, test, expect, beforeAll } from '@jest/globals';
import { render, screen } from '@testing-library/react';
import { Part, Role, type Message } from '@a2a-js/sdk';
import { createMockMessage, createTextPart } from '@/mocks/factories';

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

const imagePart = Part.fromJSON({
  raw: 'AQID',
  filename: 'pic.png',
  mediaType: 'image/png',
});

function fileMessage(role: Role): Message {
  return createMockMessage({
    messageId: `msg-${role}`,
    role,
    parts: [
      createTextPart(role === Role.ROLE_USER ? 'here you go' : 'here is your file'),
      imagePart,
    ],
    metadata: {},
  });
}

describe('ChatMessage file rendering', () => {
  test('renders a file attachment in the user bubble', () => {
    render(<ChatMessage message={fileMessage(Role.ROLE_USER)} allMessages={[]} />);
    expect(screen.getByAltText('pic.png')).toBeTruthy();
  });

  test('renders a file attachment in the agent bubble', () => {
    render(<ChatMessage message={fileMessage(Role.ROLE_AGENT)} allMessages={[]} />);
    expect(screen.getByAltText('pic.png')).toBeTruthy();
  });

  test('renders a file-only message (no text) instead of skipping it', () => {
    const msg = createMockMessage({
      messageId: 'file-only',
      role: Role.ROLE_AGENT,
      parts: [Part.fromJSON({
        raw: 'YSxiCg==',
        filename: 'data.csv',
        mediaType: 'text/csv',
      })],
      metadata: {},
    });
    render(<ChatMessage message={msg} allMessages={[]} />);
    expect(screen.getByText('data.csv')).toBeTruthy();
  });
});
