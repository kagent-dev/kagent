import { render, screen, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AgentDetailsSidebar } from "../AgentDetailsSidebar";

// Mock next/link
jest.mock("next/link", () => ({
  __esModule: true,
  default: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));

// Mock next/navigation
jest.mock("next/navigation", () => ({
  usePathname: () => "/agents/default/test-agent/chat",
  useRouter: () => ({ push: jest.fn() }),
}));

// Mock getAgents action
jest.mock("@/app/actions/agents", () => ({
  getAgents: () => Promise.resolve({ data: [] }),
}));

// Mock Sheet components
const mockSheetProps = jest.fn();
jest.mock("@/components/ui/sheet", () => ({
  Sheet: ({ open, onOpenChange, children }: { open: boolean; onOpenChange: (v: boolean) => void; children: React.ReactNode }) => {
    mockSheetProps({ open, onOpenChange });
    return open ? <div data-testid="sheet">{children}</div> : null;
  },
  SheetContent: ({ children, side, className }: { children: React.ReactNode; side?: string; className?: string }) => (
    <div data-testid="sheet-content" data-side={side} className={className}>{children}</div>
  ),
  SheetHeader: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <div data-testid="sheet-header" className={className}>{children}</div>
  ),
  SheetTitle: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <h2 data-testid="sheet-title" className={className}>{children}</h2>
  ),
  SheetDescription: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <p data-testid="sheet-description" className={className}>{children}</p>
  ),
}));

// Mock sidebar sub-components (used inside Sheet content)
jest.mock("@/components/ui/sidebar", () => ({
  useSidebar: () => ({ state: "expanded", isMobile: false }),
  SidebarGroup: ({ children, className }: React.PropsWithChildren<{ className?: string }>) => (
    <div data-testid="sidebar-group" className={className}>{children}</div>
  ),
  SidebarGroupLabel: ({ children, className }: React.PropsWithChildren<{ className?: string }>) => (
    <div data-testid="sidebar-group-label" className={className}>{children}</div>
  ),
  SidebarMenu: ({ children }: React.PropsWithChildren) => <ul data-testid="sidebar-menu">{children}</ul>,
  SidebarMenuItem: ({ children }: React.PropsWithChildren) => <li data-testid="sidebar-menu-item">{children}</li>,
  SidebarMenuButton: ({ children, tooltip, className }: React.PropsWithChildren<{ tooltip?: string; className?: string }>) => (
    <button data-testid="sidebar-menu-button" className={className}>{children}</button>
  ),
}));

// Mock other UI components
jest.mock("@/components/ui/scroll-area", () => ({
  ScrollArea: ({ children, className }: React.PropsWithChildren<{ className?: string }>) => (
    <div data-testid="scroll-area" className={className}>{children}</div>
  ),
}));

jest.mock("@/components/LoadingState", () => ({
  LoadingState: () => <div data-testid="loading-state" />,
}));

jest.mock("@/components/ui/collapsible", () => ({
  Collapsible: ({ children, open }: React.PropsWithChildren<{ open?: boolean }>) => (
    <div data-testid="collapsible" data-open={open}>{children}</div>
  ),
  CollapsibleContent: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
  CollapsibleTrigger: ({ children }: React.PropsWithChildren<{ asChild?: boolean }>) => <div>{children}</div>,
}));

jest.mock("@/components/ui/button", () => ({
  Button: ({ children, ...props }: React.PropsWithChildren<Record<string, unknown>>) => (
    <button {...props}>{children}</button>
  ),
}));

jest.mock("@/components/ui/tooltip", () => ({
  Tooltip: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
  TooltipContent: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
  TooltipProvider: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
  TooltipTrigger: ({ children }: React.PropsWithChildren<{ asChild?: boolean }>) => <div>{children}</div>,
}));

jest.mock("@/components/ui/badge", () => ({
  Badge: ({ children }: React.PropsWithChildren) => <span>{children}</span>,
}));

jest.mock("@/lib/toolUtils", () => ({
  isAgentTool: () => false,
  isMcpTool: () => false,
  getToolDescription: () => "",
  getToolIdentifier: () => "tool-id",
  getToolDisplayName: () => "Tool Name",
}));

jest.mock("@/lib/k8sUtils", () => ({
  k8sRefUtils: { fromRef: (ref: string) => ({ name: ref, namespace: "default" }) },
}));

jest.mock("@/lib/utils", () => ({
  cn: (...args: unknown[]) => args.filter(Boolean).join(" "),
}));

const mockAgent = {
  agent: {
    metadata: { name: "test-agent", namespace: "default" },
    spec: { description: "A test agent", type: "Declarative", skills: { refs: [] } },
  },
  tools: [],
  model: "gpt-4",
};

describe("AgentDetailsSidebar (Step 5)", () => {
  const mockOnClose = jest.fn();

  beforeEach(() => {
    jest.clearAllMocks();
  });

  const renderSidebar = async (open: boolean) => {
    await act(async () => {
      render(
        <AgentDetailsSidebar
          selectedAgentName="test-agent"
          currentAgent={mockAgent as any}
          allTools={[]}
          open={open}
          onClose={mockOnClose}
        />
      );
    });
  };

  it("renders as a Sheet when open=true", async () => {
    await renderSidebar(true);

    expect(screen.getByTestId("sheet")).toBeInTheDocument();
    expect(screen.getByTestId("sheet-content")).toBeInTheDocument();
    expect(screen.getByTestId("sheet-content")).toHaveAttribute("data-side", "right");
  });

  it("does not render content when open=false", async () => {
    await renderSidebar(false);

    expect(screen.queryByTestId("sheet")).not.toBeInTheDocument();
    expect(screen.queryByTestId("sheet-content")).not.toBeInTheDocument();
  });

  it("displays 'Agent Details' title in Sheet header", async () => {
    await renderSidebar(true);

    expect(screen.getByTestId("sheet-title")).toHaveTextContent("Agent Details");
  });

  it("displays agent name and namespace", async () => {
    await renderSidebar(true);

    expect(screen.getByText(/default\/test-agent/)).toBeInTheDocument();
  });

  it("displays agent description", async () => {
    await renderSidebar(true);

    expect(screen.getByText("A test agent")).toBeInTheDocument();
  });

  it("passes onClose to Sheet onOpenChange", async () => {
    await renderSidebar(true);

    // The Sheet should receive onOpenChange prop (our onClose callback)
    expect(mockSheetProps).toHaveBeenCalledWith(
      expect.objectContaining({ open: true, onOpenChange: expect.any(Function) })
    );
  });
});
