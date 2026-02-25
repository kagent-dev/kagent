import { render, screen, act, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import ChatLayoutUI from "../ChatLayoutUI";

// Mock next/navigation
jest.mock("next/navigation", () => ({
  usePathname: () => "/agents/default/test-agent/chat",
  useRouter: () => ({ push: jest.fn() }),
}));

// Mock session fetching
jest.mock("@/app/actions/sessions", () => ({
  getSessionsForAgent: () => Promise.resolve({ data: [], error: null }),
}));

// Mock SessionsSidebar
jest.mock("@/components/sidebars/SessionsSidebar", () => ({
  __esModule: true,
  default: () => <div data-testid="sessions-sidebar" />,
}));

// Mock AgentDetailsSidebar
const mockAgentDetailsSidebar = jest.fn();
jest.mock("@/components/sidebars/AgentDetailsSidebar", () => ({
  AgentDetailsSidebar: (props: Record<string, unknown>) => {
    mockAgentDetailsSidebar(props);
    return props.open ? <div data-testid="agent-details-sidebar" /> : null;
  },
}));

// Mock sonner
jest.mock("sonner", () => ({
  toast: { error: jest.fn() },
}));

// Mock UI components
jest.mock("@/components/ui/button", () => ({
  Button: ({ children, onClick, ...props }: React.PropsWithChildren<{ onClick?: () => void }>) => (
    <button onClick={onClick} {...props}>{children}</button>
  ),
}));

const mockAgent = {
  agent: {
    metadata: { name: "test-agent", namespace: "default" },
    spec: { description: "A test agent", type: "Declarative" },
  },
  tools: [],
  model: "gpt-4",
};

describe("ChatLayoutUI (Step 5)", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  const renderComponent = async (children: React.ReactNode = <div>content</div>) => {
    let result: ReturnType<typeof render>;
    await act(async () => {
      result = render(
        <ChatLayoutUI
          agentName="test-agent"
          namespace="default"
          currentAgent={mockAgent as any}
          allAgents={[]}
          allTools={[]}
        >
          {children}
        </ChatLayoutUI>
      );
    });
    return result!;
  };

  it("renders children in the content area", async () => {
    await renderComponent(<div data-testid="chat-content">Chat here</div>);
    expect(screen.getByTestId("chat-content")).toBeInTheDocument();
  });

  it("renders sessions sidebar", async () => {
    await renderComponent();
    expect(screen.getByTestId("sessions-sidebar")).toBeInTheDocument();
  });

  it("renders info trigger button for agent details", async () => {
    await renderComponent();
    expect(screen.getByRole("button", { name: "Show agent details" })).toBeInTheDocument();
  });

  it("opens AgentDetailsSidebar when info button is clicked", async () => {
    const user = userEvent.setup();
    await renderComponent();

    // Initially closed
    expect(screen.queryByTestId("agent-details-sidebar")).not.toBeInTheDocument();
    expect(mockAgentDetailsSidebar).toHaveBeenCalledWith(
      expect.objectContaining({ open: false })
    );

    // Click the info button
    await user.click(screen.getByRole("button", { name: "Show agent details" }));

    // Now open
    await waitFor(() => {
      expect(screen.getByTestId("agent-details-sidebar")).toBeInTheDocument();
    });
    expect(mockAgentDetailsSidebar).toHaveBeenLastCalledWith(
      expect.objectContaining({ open: true })
    );
  });

  it("passes onClose callback to AgentDetailsSidebar", async () => {
    await renderComponent();
    expect(mockAgentDetailsSidebar).toHaveBeenCalledWith(
      expect.objectContaining({ onClose: expect.any(Function) })
    );
  });

  it("wraps content in a flex container for horizontal layout", async () => {
    const { container } = await renderComponent();

    // Outermost div should have flex layout
    const wrapper = container.firstElementChild;
    expect(wrapper?.className).toContain("flex");
    expect(wrapper?.className).toContain("h-full");
    expect(wrapper?.className).toContain("w-full");
  });
});
