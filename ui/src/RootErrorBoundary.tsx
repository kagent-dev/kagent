import { Component, type ErrorInfo, type ReactNode } from "react";

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
}

/**
 * Catches a crash above the theme/auth/provider tree, where no antd or Emotion
 * context exists yet — so the fallback is plain inline styles only.
 */
export class RootErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };

  static getDerivedStateFromError(): State {
    return { hasError: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("App crashed before rendering", error, info);
  }

  render() {
    return this.state.hasError ? <RootErrorFallback /> : this.props.children;
  }
}

// Pinned to the colours index.html paints before the bundle loads: when this
// renders, no theme has mounted, so that is what the page behind it is.
export function RootErrorFallback() {
  return (
      <div
        style={{
          display: "grid",
          placeItems: "center",
          minHeight: "100vh",
          background: "#030712",
          color: "#e5e7eb",
          fontFamily: "system-ui, sans-serif",
          padding: 24,
        }}
      >
        <style>{`
          .root-error-reload { background: #6d28d9; }
          .root-error-reload:hover { background: #5b21b6; }
          .root-error-reload:active { background: #4c1d95; }
          .root-error-reload:focus-visible { outline: 2px solid #a78bfa; outline-offset: 2px; }
        `}</style>
        <div style={{ display: "grid", gap: 16, maxWidth: 420, textAlign: "center" }}>
          <h1 style={{ margin: 0, fontSize: 20 }}>Something went wrong</h1>
          <p style={{ margin: 0, color: "#9ca3af" }}>
            The app hit an error and could not load. Reloading usually fixes it.
          </p>
          <button
            type="button"
            className="root-error-reload"
            onClick={() => window.location.reload()}
            style={{
              justifySelf: "center",
              padding: "8px 20px",
              borderRadius: 6,
              border: "none",
              color: "#fff",
              cursor: "pointer",
            }}
          >
            Reload
          </button>
        </div>
      </div>
    );
}
