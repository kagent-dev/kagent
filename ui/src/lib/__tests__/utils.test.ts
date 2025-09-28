import { describe, expect, it, jest, beforeEach, afterEach, afterAll } from '@jest/globals';
import { getBackendUrl, getRelativeTimeString, isResourceNameValid, messageUtils } from '../utils';

describe('URL Generation Utilities', () => {
  const originalEnv = process.env;

  beforeEach(() => {
    jest.resetModules();
    process.env = { ...originalEnv };
  });

  afterAll(() => {
    process.env = originalEnv;
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
