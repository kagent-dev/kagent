import '@testing-library/jest-dom';
import { TextEncoder, TextDecoder } from 'util';

// Mock uuid module (ESM-only, needs mocking for Jest)
jest.mock('uuid', () => ({
  v4: jest.fn(() => 'test-uuid-v4'),
}));

// @a2a-js/sdk's CJS bundle requires `jose` at module-load time.
// In Jest (CJS) this can trip on jose's ESM-only entrypoint, so we provide
// a minimal mock since UI tests do not exercise agent-card signature codepaths.
jest.mock('jose', () => ({}));

// Polyfill TextEncoder/TextDecoder for Node.js test environment
global.TextEncoder = TextEncoder;
global.TextDecoder = TextDecoder as typeof global.TextDecoder;

// Polyfill Request/Response for Next.js server actions
if (typeof Request === 'undefined') {
  global.Request = class Request {} as unknown as typeof Request;
}
if (typeof Response === 'undefined') {
  global.Response = class Response {} as unknown as typeof Response;
}
if (typeof Headers === 'undefined') {
  global.Headers = class Headers {} as unknown as typeof Headers;
}

// cmdk / Radix use ResizeObserver
global.ResizeObserver = class ResizeObserver {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
} as unknown as typeof ResizeObserver;

// jsdom: cmdk scrolls selected items into view
Element.prototype.scrollIntoView = function scrollIntoView() {};

// jsdom: SidebarProvider / useIsMobile
Object.defineProperty(window, "matchMedia", {
  writable: true,
  configurable: true,
  value: jest.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: jest.fn(),
    removeListener: jest.fn(),
    addEventListener: jest.fn(),
    removeEventListener: jest.fn(),
    dispatchEvent: jest.fn(),
  })),
});

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