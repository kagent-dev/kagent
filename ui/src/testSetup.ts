import "@testing-library/jest-dom/vitest";

// antd reads matchMedia for responsive breakpoints; jsdom does not implement it.
if (!window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as typeof window.matchMedia;
}

// antd measures its overlays — dropdowns, tooltips, the select's own menu — with a
// ResizeObserver, which jsdom does not implement. Without this, opening any of them in
// a test throws `ResizeObserver is not defined`, which reads as a component fault and is
// not one. Observing nothing is the right stub: layout has no meaning in jsdom, and the
// only thing under test is what the component does when something is chosen.
if (!("ResizeObserver" in globalThis)) {
  class NoopResizeObserver implements ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  }
  globalThis.ResizeObserver = NoopResizeObserver;
}
