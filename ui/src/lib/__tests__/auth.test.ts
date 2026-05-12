import { describe, expect, it, jest, beforeEach, afterAll } from '@jest/globals';
import {
  CORS_ALLOW_HEADERS,
  extractAllowedHeaders,
  getAuthHeadersFromRequest,
  parseAllowedForwardHeaders,
} from '../auth';
import type { NextRequest } from 'next/server';

function fakeRequest(headers: Record<string, string>): NextRequest {
  const lower: Record<string, string> = {};
  for (const [k, v] of Object.entries(headers)) {
    lower[k.toLowerCase()] = v;
  }
  return {
    headers: {
      get: (name: string) => lower[name.toLowerCase()] ?? null,
    },
  } as unknown as NextRequest;
}

describe('parseAllowedForwardHeaders', () => {
  it('returns just the default forward set when env is undefined', () => {
    const { allowed, blocked } = parseAllowedForwardHeaders(undefined);
    expect(Array.from(allowed)).toEqual(['authorization']);
    expect(blocked).toEqual([]);
  });

  it('returns just the default forward set when env is empty', () => {
    const { allowed, blocked } = parseAllowedForwardHeaders('');
    expect(Array.from(allowed)).toEqual(['authorization']);
    expect(blocked).toEqual([]);
  });

  it('adds extra headers from env, lowercased', () => {
    const { allowed } = parseAllowedForwardHeaders('X-Slack-User, X-Slack-Team');
    expect(allowed.has('authorization')).toBe(true);
    expect(allowed.has('x-slack-user')).toBe(true);
    expect(allowed.has('x-slack-team')).toBe(true);
  });

  it('trims whitespace and ignores empty entries', () => {
    const { allowed } = parseAllowedForwardHeaders('  X-A  ,, ,X-B,');
    expect(allowed.has('x-a')).toBe(true);
    expect(allowed.has('x-b')).toBe(true);
    expect(allowed.size).toBe(3); // authorization + x-a + x-b
  });

  it('drops hop-by-hop / routing headers and reports them as blocked', () => {
    const { allowed, blocked } = parseAllowedForwardHeaders(
      'Host, Connection, Transfer-Encoding, Content-Length, X-Slack-User'
    );
    expect(allowed.has('host')).toBe(false);
    expect(allowed.has('connection')).toBe(false);
    expect(allowed.has('transfer-encoding')).toBe(false);
    expect(allowed.has('content-length')).toBe(false);
    expect(allowed.has('x-slack-user')).toBe(true);
    expect(blocked.sort()).toEqual(
      ['connection', 'content-length', 'host', 'transfer-encoding'].sort()
    );
  });

  it('treats Authorization listed in env as a no-op (already in default set)', () => {
    const { allowed, blocked } = parseAllowedForwardHeaders('Authorization');
    expect(Array.from(allowed)).toEqual(['authorization']);
    expect(blocked).toEqual([]);
  });
});

describe('extractAllowedHeaders', () => {
  it('returns only headers present in the allowlist', () => {
    const got = extractAllowedHeaders(
      new Set(['authorization', 'x-slack-user']),
      (name) =>
        ({
          authorization: 'Bearer abc',
          'x-slack-user': 'U123',
          'x-other': 'should-not-appear',
        } as Record<string, string>)[name] ?? null
    );
    expect(got).toEqual({
      authorization: 'Bearer abc',
      'x-slack-user': 'U123',
    });
  });

  it('skips headers that are absent from the request', () => {
    const got = extractAllowedHeaders(
      new Set(['authorization', 'x-slack-user']),
      (name) => (name === 'authorization' ? 'Bearer abc' : null)
    );
    expect(got).toEqual({ authorization: 'Bearer abc' });
  });

  it('skips empty header values', () => {
    const got = extractAllowedHeaders(
      new Set(['authorization', 'x-slack-user']),
      (name) => (name === 'authorization' ? 'Bearer abc' : ''),
    );
    expect(got).toEqual({ authorization: 'Bearer abc' });
  });

  it('returns an empty record when the allowlist is empty', () => {
    const got = extractAllowedHeaders(new Set(), () => 'whatever');
    expect(got).toEqual({});
  });
});

describe('getAuthHeadersFromRequest (env-driven)', () => {
  const originalEnv = process.env;

  beforeEach(() => {
    process.env = { ...originalEnv };
    delete process.env.KAGENT_ADDITIONAL_FORWARDED_HEADERS;
  });

  afterAll(() => {
    process.env = originalEnv;
  });

  it('forwards only Authorization by default', () => {
    const got = getAuthHeadersFromRequest(
      fakeRequest({ Authorization: 'Bearer x', 'X-Slack-User': 'U123' })
    );
    expect(got).toEqual({ authorization: 'Bearer x' });
  });

  it('forwards extra headers when env lists them', () => {
    process.env.KAGENT_ADDITIONAL_FORWARDED_HEADERS = 'X-Slack-User, X-Slack-Team';
    const got = getAuthHeadersFromRequest(
      fakeRequest({
        Authorization: 'Bearer x',
        'X-Slack-User': 'U123',
        'X-Slack-Team': 'T456',
        'X-Not-Listed': 'nope',
      })
    );
    expect(got).toEqual({
      authorization: 'Bearer x',
      'x-slack-user': 'U123',
      'x-slack-team': 'T456',
    });
  });

  it('reads env on every call (not frozen at module load)', () => {
    const req = fakeRequest({ Authorization: 'Bearer x', 'X-Late': 'v' });

    expect(getAuthHeadersFromRequest(req)).toEqual({ authorization: 'Bearer x' });

    process.env.KAGENT_ADDITIONAL_FORWARDED_HEADERS = 'X-Late';
    expect(getAuthHeadersFromRequest(req)).toEqual({
      authorization: 'Bearer x',
      'x-late': 'v',
    });

    delete process.env.KAGENT_ADDITIONAL_FORWARDED_HEADERS;
    expect(getAuthHeadersFromRequest(req)).toEqual({ authorization: 'Bearer x' });
  });

  it('does not forward hop-by-hop headers even if listed', () => {
    const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    process.env.KAGENT_ADDITIONAL_FORWARDED_HEADERS = 'Host, Connection, X-Slack-User';
    const got = getAuthHeadersFromRequest(
      fakeRequest({
        Authorization: 'Bearer x',
        Host: 'evil.example.com',
        Connection: 'close',
        'X-Slack-User': 'U123',
      })
    );
    expect(got).toEqual({
      authorization: 'Bearer x',
      'x-slack-user': 'U123',
    });
    warnSpy.mockRestore();
  });
});

describe('CORS_ALLOW_HEADERS', () => {
  it('does not advertise additional forwarded headers cross-origin', () => {
    expect(CORS_ALLOW_HEADERS).toBe('Content-Type, Authorization, Accept');
  });
});
