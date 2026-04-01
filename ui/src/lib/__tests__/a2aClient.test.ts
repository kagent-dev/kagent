import { describe, expect, it, jest, beforeEach } from '@jest/globals';
import { KagentA2AClient } from '../a2aClient';

// Mock fetch globally
const mockFetch = jest.fn() as jest.MockedFunction<typeof fetch>;
global.fetch = mockFetch;

// Mock utils
jest.mock('../utils', () => ({
  getBackendUrl: () => 'http://localhost:8083/api',
}));

describe('KagentA2AClient', () => {
  let client: KagentA2AClient;

  beforeEach(() => {
    client = new KagentA2AClient();
    mockFetch.mockClear();
  });

  describe('getAgentUrl', () => {
    it('should construct correct agent URL', () => {
      const url = client.getAgentUrl('test-namespace', 'test-agent');
      expect(url).toBe('http://localhost:8083/api/a2a/test-namespace/test-agent');
    });
  });

  describe('createStreamingRequest', () => {
    it('should create valid JSON-RPC request structure', () => {
      const params = {
        message: {
          kind: 'message' as const,
          messageId: 'msg-1',
          role: 'user' as const,
          parts: [{ kind: 'text' as const, text: 'Hello' }],
        },
      };

      const request = client.createStreamingRequest(params);

      expect(request.jsonrpc).toBe('2.0');
      expect(request.method).toBe('message/stream');
      expect(request.params).toEqual(params);
      expect(typeof request.id).toBe('string');
      expect(request.id).toBeTruthy();
    });
  });

  describe('resubscribeTask', () => {
    it('should throw error on non-ok response', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 404,
        statusText: 'Not Found',
        text: () => Promise.resolve('Task not found'),
      } as Response);

      await expect(
        client.resubscribeTask('test-ns', 'test-agent', 'task-123')
      ).rejects.toThrow('A2A resubscribe failed: 404 Not Found - Task not found');

      expect(mockFetch).toHaveBeenCalledWith(
        '/a2a/test-ns/test-agent',
        expect.objectContaining({
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Accept': 'text/event-stream',
          },
        })
      );

      // Verify the request body contains resubscribe params
      const callArgs = mockFetch.mock.calls[0];
      const requestBody = JSON.parse(callArgs[1]?.body as string);
      expect(requestBody.jsonrpc).toBe('2.0');
      expect(requestBody.method).toBe('tasks/resubscribe');
      expect(requestBody.params).toEqual({ id: 'task-123' });
    });

    it('should throw error when response body is null', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        body: null,
      } as Response);

      await expect(
        client.resubscribeTask('test-ns', 'test-agent', 'task-123')
      ).rejects.toThrow('Response body is null');
    });

    it('should pass abort signal to fetch', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        body: null,
      } as Response);

      const abortController = new AbortController();

      await expect(
        client.resubscribeTask('test-ns', 'test-agent', 'task-123', abortController.signal)
      ).rejects.toThrow('Response body is null');

      expect(mockFetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          signal: abortController.signal,
        })
      );
    });
  });

  describe('sendMessageStream', () => {
    it('should throw error on non-ok response', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        text: () => Promise.resolve('Server error'),
      } as Response);

      const params = {
        message: {
          kind: 'message' as const,
          messageId: 'msg-1',
          role: 'user' as const,
          parts: [{ kind: 'text' as const, text: 'Hello' }],
        },
      };

      await expect(
        client.sendMessageStream('test-ns', 'test-agent', params)
      ).rejects.toThrow('A2A proxy request failed: 500 Internal Server Error - Server error');

      expect(mockFetch).toHaveBeenCalledWith(
        '/a2a/test-ns/test-agent',
        expect.objectContaining({
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Accept': 'text/event-stream',
          },
        })
      );

      // Verify the request body contains message/stream method
      const callArgs = mockFetch.mock.calls[0];
      const requestBody = JSON.parse(callArgs[1]?.body as string);
      expect(requestBody.jsonrpc).toBe('2.0');
      expect(requestBody.method).toBe('message/stream');
      expect(requestBody.params).toEqual(params);
    });

    it('should throw error when response body is null', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        body: null,
      } as Response);

      const params = {
        message: {
          kind: 'message' as const,
          messageId: 'msg-1',
          role: 'user' as const,
          parts: [{ kind: 'text' as const, text: 'Hello' }],
        },
      };

      await expect(
        client.sendMessageStream('test-ns', 'test-agent', params)
      ).rejects.toThrow('Response body is null');
    });

    it('should pass abort signal to fetch', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        body: null,
      } as Response);

      const params = {
        message: {
          kind: 'message' as const,
          messageId: 'msg-1',
          role: 'user' as const,
          parts: [{ kind: 'text' as const, text: 'Hello' }],
        },
      };

      const abortController = new AbortController();

      await expect(
        client.sendMessageStream('test-ns', 'test-agent', params, abortController.signal)
      ).rejects.toThrow('Response body is null');

      expect(mockFetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          signal: abortController.signal,
        })
      );
    });
  });
});
