import { describe, expect, it, jest, beforeEach, afterEach, afterAll } from '@jest/globals';
import { getWsUrl, getBackendUrl, getWebSocketUrl, getRelativeTimeString, isResourceNameValid, messageUtils } from '../utils';

describe('URL Generation Utilities', () => {
  const originalEnv = process.env;

  beforeEach(() => {
    jest.resetModules();
    process.env = { ...originalEnv };
  });

  afterAll(() => {
    process.env = originalEnv;
  });

  describe('getWsUrl', () => {
    it('should use NEXT_PUBLIC_WS_URL if provided', () => {
      process.env.NEXT_PUBLIC_WS_URL = 'ws://custom-url';
      expect(getWsUrl()).toBe('ws://custom-url');
    });

    it('should use wss in production with https', () => {
      Object.defineProperty(process.env, 'NODE_ENV', {
        value: 'production',
        configurable: true
      });
      Object.defineProperty(window, 'location', {
        value: { protocol: 'https:', host: 'example.com' },
        writable: true
      });
      expect(getWsUrl()).toBe('wss://example.com/api/ws');
    });

    it('should use ws in development', () => {
      Object.defineProperty(process.env, 'NODE_ENV', {
        value: 'development',
        configurable: true
      });
      expect(getWsUrl()).toBe('ws://localhost:8081/api/ws');
    });
  });

  describe('getBackendUrl', () => {
    it('should use NEXT_PUBLIC_BACKEND_URL if provided', () => {
      process.env.NEXT_PUBLIC_BACKEND_URL = 'http://custom-backend';
      expect(getBackendUrl()).toBe('http://custom-backend');
    });

    it('should use default production URL when NEXT_PUBLIC_BACKEND_URL is not set', () => {
      process.env.NEXT_PUBLIC_BACKEND_URL = undefined;
      Object.defineProperty(process.env, 'NODE_ENV', {
        value: 'production',
        configurable: true
      });
      expect(getBackendUrl()).toBe('http://kagent.kagent.svc.cluster.local/api');
    });

    it('should use default development URL', () => {
      process.env.NEXT_PUBLIC_BACKEND_URL = undefined;
      Object.defineProperty(process.env, 'NODE_ENV', {
        value: 'development',
        configurable: true
      });
      expect(getBackendUrl()).toBe('http://localhost:8083/api');
    });
  });

  describe('getWebSocketUrl', () => {
    it('should convert http to ws', () => {
      process.env.NEXT_PUBLIC_BACKEND_URL = 'http://example.com/api';
      expect(getWebSocketUrl()).toBe('ws://example.com/api');
    });

    it('should convert https to wss', () => {
      process.env.NEXT_PUBLIC_BACKEND_URL = 'https://example.com/api';
      expect(getWebSocketUrl()).toBe('wss://example.com/api');
    });
  });
});

describe('Time Utilities', () => {
  describe('getRelativeTimeString', () => {
    beforeEach(() => {
      jest.useFakeTimers();
      jest.setSystemTime(new Date('2024-01-01T12:00:00Z'));
    });

    afterEach(() => {
      jest.useRealTimers();
    });

    it('should return "just now" for times less than a minute ago', () => {
      const date = new Date('2024-01-01T11:59:30Z');
      expect(getRelativeTimeString(date)).toBe('just now');
    });

    it('should return minutes for times less than an hour ago', () => {
      const date = new Date('2024-01-01T11:30:00Z');
      expect(getRelativeTimeString(date)).toBe('30 minutes ago');
    });

    it('should return hours for times less than a day ago', () => {
      const date = new Date('2024-01-01T10:00:00Z');
      expect(getRelativeTimeString(date)).toBe('2 hours ago');
    });

    it('should return days for times less than a month ago', () => {
      const date = new Date('2023-12-30T12:00:00Z');
      expect(getRelativeTimeString(date)).toBe('2 days ago');
    });
  });
});

describe('Resource Name Validation', () => {
  describe('isResourceNameValid', () => {
    it('should accept valid RFC 1123 subdomain names', () => {
      expect(isResourceNameValid('valid-name')).toBe(true);
      expect(isResourceNameValid('valid-name-123')).toBe(true);
      expect(isResourceNameValid('sub.domain.name')).toBe(true);
    });

    it('should reject invalid names', () => {
      expect(isResourceNameValid('Invalid-Name')).toBe(false);
      expect(isResourceNameValid('-invalid-name')).toBe(false);
      expect(isResourceNameValid('invalid-name-')).toBe(false);
      expect(isResourceNameValid('invalid@name')).toBe(false);
    });
  });
});

describe('Message Utilities', () => {
  describe('messageUtils', () => {
    describe('isToolCallRequestEvent', () => {
      it('should identify valid tool call request events', () => {
        const validContent = { type: 'ToolCallRequestEvent' };
        expect(messageUtils.isToolCallRequestEvent(validContent)).toBe(true);
      });

      it('should reject invalid tool call request events', () => {
        expect(messageUtils.isToolCallRequestEvent({})).toBe(false);
        expect(messageUtils.isToolCallRequestEvent({ type: 'OtherEvent' })).toBe(false);
        expect(messageUtils.isToolCallRequestEvent('not an object')).toBe(false);
      });
    });

    describe('isToolCallExecutionEvent', () => {
      it('should identify valid tool call execution events', () => {
        const validContent = { type: 'ToolCallExecutionEvent' };
        expect(messageUtils.isToolCallExecutionEvent(validContent)).toBe(true);
      });

      it('should reject invalid tool call execution events', () => {
        expect(messageUtils.isToolCallExecutionEvent({})).toBe(false);
        expect(messageUtils.isToolCallExecutionEvent({ type: 'OtherEvent' })).toBe(false);
        expect(messageUtils.isToolCallExecutionEvent('not an object')).toBe(false);
      });
    });

    describe('isToolCallSummaryMessage', () => {
      it('should identify valid tool call summary messages', () => {
        const validContent = { type: 'ToolCallSummaryMessage' };
        expect(messageUtils.isToolCallSummaryMessage(validContent)).toBe(true);
      });

      it('should reject invalid tool call summary messages', () => {
        expect(messageUtils.isToolCallSummaryMessage({})).toBe(false);
        expect(messageUtils.isToolCallSummaryMessage({ type: 'OtherMessage' })).toBe(false);
        expect(messageUtils.isToolCallSummaryMessage('not an object')).toBe(false);
      });
    });

    describe('isTextMessageContent', () => {
      it('should identify valid text message content', () => {
        const validContent = {
          content: 'Hello',
          type: 'TextMessage'
        };
        expect(messageUtils.isTextMessageContent(validContent)).toBe(true);
      });

      it('should reject invalid text message content', () => {
        expect(messageUtils.isTextMessageContent({ content: 'Hello' })).toBe(false);
        expect(messageUtils.isTextMessageContent({ type: 'TextMessage' })).toBe(false);
        expect(messageUtils.isTextMessageContent('not an object')).toBe(false);
      });
    });

    describe('isUser', () => {
      it('should identify user source', () => {
        expect(messageUtils.isUser('user')).toBe(true);
        expect(messageUtils.isUser('assistant')).toBe(false);
        expect(messageUtils.isUser('system')).toBe(false);
      });
    });
  });
}); 