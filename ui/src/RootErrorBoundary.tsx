import { Component, type ErrorInfo, type ReactNode } from "react";
import { storedMode } from "./theme/themeMode";

interface Props {
  children: ReactNode;
}

interface State {
  error?: unknown;
}

/**
 * Catches a crash above the theme/auth/provider tree, where no antd or Emotion
 * context exists yet — so the fallback carries its own styles and depends on
 * nothing but the DOM.
 */
export class RootErrorBoundary extends Component<Props, State> {
  state: State = {};

  static getDerivedStateFromError(error: unknown): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("App crashed before rendering", error, info);
  }

  render() {
    return "error" in this.state ? (
      <RootErrorFallback error={this.state.error} />
    ) : (
      this.props.children
    );
  }
}

/** The message and stack, as raw text to read or copy. */
function formatError(error: unknown): string {
  if (error instanceof Error) {
    return error.stack?.includes(error.message)
      ? error.stack
      : `${error.name}: ${error.message}\n${error.stack ?? ""}`.trimEnd();
  }
  return String(error);
}

/*
 * Its own palette, as CSS variables: this renders above the theme provider, so
 * there is no token to read. The mode the user chose wins, and the system
 * preference decides when they have chosen nothing.
 */
const styles = `
.rootError {
  --page: #030712;
  --card: #0b101c;
  --edge: #1f2937;
  --ink: #e5e7eb;
  --muted: #9ca3af;
  --code: #060a14;
  --codeInk: #cbd5e1;
  --accent: #c4b5fd;
  --accentHover: #ddd6fe;
  --accentActive: #a78bfa;
  --badgeBg: #1e152e;
  --badgeEdge: #4c1d95;
  --shadow: 0 12px 40px rgb(0 0 0 / 45%);
}
@media (prefers-color-scheme: light) {
  .rootError:not([data-theme="dark"]) {
    --page: #f6f5f9;
    --card: #ffffff;
    --edge: #ddd7e7;
    --ink: #151927;
    --muted: #6b6577;
    --code: #faf9fd;
    --codeInk: #2c3040;
    --accent: #6d28d9;
    --accentHover: #5b21b6;
    --accentActive: #4c1d95;
    --badgeBg: #f5f3ff;
    --badgeEdge: #ddd6fe;
    --shadow: 0 12px 40px rgb(21 25 39 / 10%);
  }
}
.rootError[data-theme="light"] {
  --page: #f6f5f9;
  --card: #ffffff;
  --edge: #ddd7e7;
  --ink: #151927;
  --muted: #6b6577;
  --code: #faf9fd;
  --codeInk: #2c3040;
  --accent: #6d28d9;
  --accentHover: #5b21b6;
  --accentActive: #4c1d95;
  --badgeBg: #f5f3ff;
  --badgeEdge: #ddd6fe;
  --shadow: 0 12px 40px rgb(21 25 39 / 10%);
}
.rootError {
  display: grid;
  place-items: center;
  min-height: 100vh;
  padding: 24px;
  background: var(--page);
  color: var(--ink);
  font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
}
.rootError-card {
  width: 100%;
  max-width: 620px;
  padding: 28px;
  border: 1px solid var(--edge);
  border-radius: 12px;
  background: var(--card);
  box-shadow: var(--shadow);
}
.rootError-icon {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  margin: 0 auto 16px;
  border: 1px solid var(--badgeEdge);
  border-radius: 50%;
  background: var(--badgeBg);
  color: var(--accent);
}
.rootError-title {
  margin: 0;
  font-size: 19px;
  font-weight: 600;
  text-align: center;
  letter-spacing: -0.01em;
}
.rootError-lede {
  margin: 8px 0 0;
  color: var(--muted);
  font-size: 14px;
  line-height: 1.5;
  text-align: center;
}
.rootError-details {
  margin-top: 24px;
  border-top: 1px solid var(--edge);
}
.rootError-details > summary {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 4px 0;
  color: var(--accent);
  font-size: 13px;
  list-style: none;
  cursor: pointer;
  user-select: none;
}
.rootError-details > summary::-webkit-details-marker { display: none; }
.rootError-details > summary:hover { color: var(--accentHover); }
.rootError-details > summary:active { color: var(--accentActive); }
.rootError-details > summary:focus-visible {
  outline: 2px solid var(--accentActive);
  outline-offset: 3px;
  border-radius: 4px;
}
.rootError-chevron { transition: transform 150ms ease; }
.rootError-details[open] .rootError-chevron { transform: rotate(90deg); }
.rootError-stack {
  margin: 12px 0 0;
  padding: 14px;
  border: 1px solid var(--edge);
  border-radius: 8px;
  background: var(--code);
  overflow: auto;
  max-height: 46vh;
}
.rootError-stack code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
  color: var(--codeInk);
  white-space: pre-wrap;
  word-break: break-word;
}
`;

export function RootErrorFallback({ error }: { error?: unknown }) {
  return (
    <div className="rootError" data-theme={storedMode()}>
      <style>{styles}</style>
      <div className="rootError-card">
        <div className="rootError-icon">
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path
              d="M12 9v4m0 4h.01M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </div>
        <h1 className="rootError-title">Something went wrong</h1>
        <p className="rootError-lede">The app hit an error and could not load.</p>

        <details className="rootError-details">
          <summary>
            <svg
              className="rootError-chevron"
              width="12"
              height="12"
              viewBox="0 0 24 24"
              fill="none"
              aria-hidden="true"
            >
              <path
                d="m9 18 6-6-6-6"
                stroke="currentColor"
                strokeWidth="2.5"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
            Error details
          </summary>
          {/* Raw text, kept as written: a stack is only readable with its own
              line breaks and indentation. */}
          <pre className="rootError-stack">
            <code data-testid="root-error-details">{formatError(error)}</code>
          </pre>
        </details>
      </div>
    </div>
  );
}
