import { Component, type ErrorInfo, type ReactNode } from "react";

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
}

/** Degrades one extension contribution to nothing instead of taking the page down. */
export class SlotErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };

  static getDerivedStateFromError(): State {
    return { hasError: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Extension contribution crashed", error, info);
  }

  render() {
    return this.state.hasError ? null : this.props.children;
  }
}
