import '@testing-library/jest-dom';
import { TextEncoder, TextDecoder } from 'util';

// Mock uuid module (ESM-only, needs mocking for Jest)
jest.mock('uuid', () => ({
  v4: jest.fn(() => 'test-uuid-v4'),
}));

// Polyfill TextEncoder/TextDecoder for Node.js test environment
global.TextEncoder = TextEncoder;
global.TextDecoder = TextDecoder as typeof global.TextDecoder;

// Polyfill Request/Response for Next.js server actions
if (typeof Request === 'undefined') {
  global.Request = class Request {} as any;
}
if (typeof Response === 'undefined') {
  global.Response = class Response {} as any;
}
if (typeof Headers === 'undefined') {
  global.Headers = class Headers {} as any;
}

// Mock next/router
jest.mock('next/router', () => ({
  useRouter() {
    return {
      route: '/',
      pathname: '',
      query: {},
      asPath: '',
      push: jest.fn(),
      replace: jest.fn(),
    };
  },
}));

// Mock next/navigation
jest.mock('next/navigation', () => ({
  useRouter() {
    return {
      push: jest.fn(),
      replace: jest.fn(),
      refresh: jest.fn(),
      back: jest.fn(),
    };
  },
  usePathname() {
    return '';
  },
  useSearchParams() {
    return new URLSearchParams();
  },
})); 