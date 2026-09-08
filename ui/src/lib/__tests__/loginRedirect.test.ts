import { describe, expect, it } from '@jest/globals';
import { sanitizeRedirect } from '../loginRedirect';

describe('sanitizeRedirect', () => {
  it('passes through a same-origin path', () => {
    expect(sanitizeRedirect('/agents/foo')).toBe('/agents/foo');
  });

  it('passes through a same-origin path with a query string', () => {
    expect(sanitizeRedirect('/agents/foo?tab=history')).toBe('/agents/foo?tab=history');
  });

  it('defaults to "/" when undefined', () => {
    expect(sanitizeRedirect(undefined)).toBe('/');
  });

  it('defaults to "/" for an empty string', () => {
    expect(sanitizeRedirect('')).toBe('/');
  });

  it('rejects an absolute URL', () => {
    expect(sanitizeRedirect('https://evil.example.com/phish')).toBe('/');
  });

  it('treats a bare host+path with no leading slash as a relative path segment', () => {
    // Matches URL/browser semantics: with no scheme and no leading "/",
    // this resolves relative to the current path rather than a new host.
    expect(sanitizeRedirect('evil.example.com/phish')).toBe('/evil.example.com/phish');
  });

  it('rejects a protocol-relative URL', () => {
    expect(sanitizeRedirect('//evil.example.com/phish')).toBe('/');
  });

  it('rejects a backslash-prefixed path some browsers treat as protocol-relative', () => {
    expect(sanitizeRedirect('/\\evil.example.com/phish')).toBe('/');
  });

  it('rejects a tab-smuggled protocol-relative URL (stripped by the URL parser before host resolution)', () => {
    expect(sanitizeRedirect('/\t/evil.example.com/phish')).toBe('/');
  });

  it('rejects a different scheme entirely', () => {
    expect(sanitizeRedirect('javascript:alert(1)')).toBe('/');
  });
});
