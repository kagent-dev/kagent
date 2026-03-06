import { render, screen, fireEvent, act } from "@testing-library/react";

// Mock next/navigation
const mockName = "kanban";
const mockPath: string[] | undefined = undefined;
jest.mock("next/navigation", () => ({
  useParams: () => ({ name: mockName, path: mockPath }),
}));

// Mock next-themes
jest.mock("next-themes", () => ({
  useTheme: () => ({ resolvedTheme: "dark" }),
}));

// Mock namespace context
jest.mock("@/lib/namespace-context", () => ({
  useNamespace: () => ({ namespace: "default" }),
}));

// Mock lucide-react icons
jest.mock("lucide-react", () => ({
  AlertCircle: ({ className }: { className?: string }) => (
    <span data-testid="icon-alert-circle" className={className} />
  ),
  Loader2: ({ className }: { className?: string }) => (
    <span data-testid="icon-loader" className={className} />
  ),
  RefreshCw: ({ className }: { className?: string }) => (
    <span data-testid="icon-refresh" className={className} />
  ),
}));

// Mock Button component
jest.mock("@/components/ui/button", () => ({
  Button: ({
    children,
    onClick,
    ...props
  }: React.PropsWithChildren<{ onClick?: () => void }>) => (
    <button onClick={onClick} {...props}>
      {children}
    </button>
  ),
}));

import PluginPage from "../[name]/[[...path]]/page";

describe("PluginPage", () => {
  it("shows loading skeleton initially", () => {
    render(<PluginPage />);
    expect(screen.getByTestId("plugin-loading")).toBeInTheDocument();
    expect(screen.getByText("Loading plugin…")).toBeInTheDocument();
  });

  it("hides loading and shows iframe after load", () => {
    render(<PluginPage />);
    const iframe = screen.getByTitle("Plugin: kanban");

    act(() => {
      fireEvent.load(iframe);
    });

    expect(screen.queryByTestId("plugin-loading")).not.toBeInTheDocument();
    expect(iframe).not.toHaveClass("hidden");
  });

  it("shows error fallback on iframe error", () => {
    render(<PluginPage />);
    const iframe = screen.getByTitle("Plugin: kanban");

    // Event listeners are attached directly on the iframe element via useEffect
    act(() => {
      iframe.dispatchEvent(new Event("error"));
    });

    expect(screen.queryByTestId("plugin-loading")).not.toBeInTheDocument();
    expect(screen.getByTestId("plugin-error")).toBeInTheDocument();
    expect(screen.getByText("Plugin unavailable")).toBeInTheDocument();
    expect(screen.getByText(/kanban/)).toBeInTheDocument();
  });

  it("retries loading on retry button click", () => {
    render(<PluginPage />);
    const iframe = screen.getByTitle("Plugin: kanban");

    // Trigger error
    act(() => {
      iframe.dispatchEvent(new Event("error"));
    });
    expect(screen.getByTestId("plugin-error")).toBeInTheDocument();

    // Click retry
    act(() => {
      fireEvent.click(screen.getByText("Retry"));
    });

    // Should show loading again (iframe re-created via key change)
    expect(screen.getByTestId("plugin-loading")).toBeInTheDocument();
    expect(screen.queryByTestId("plugin-error")).not.toBeInTheDocument();
  });

  it("renders iframe with correct src using /_p/ prefix", () => {
    render(<PluginPage />);
    const iframe = screen.getByTitle("Plugin: kanban");
    expect(iframe).toHaveAttribute("src", "/_p/kanban/");
  });

  it("renders iframe with sandbox attribute", () => {
    render(<PluginPage />);
    const iframe = screen.getByTitle("Plugin: kanban");
    expect(iframe).toHaveAttribute(
      "sandbox",
      "allow-scripts allow-same-origin allow-forms allow-popups"
    );
  });
});
