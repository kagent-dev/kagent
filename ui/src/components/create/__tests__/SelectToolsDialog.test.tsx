/**
 * Test: Tool Limit Warning (Issue #694)
 *
 * Verifies that the hard 20-tool limit has been replaced with a soft warning.
 * - Tools can be added beyond 20 (no hard block)
 * - A warning is shown when 20+ tools are selected
 * - The warning informs about potential token usage impact
 */

import { describe, it, expect, jest, beforeEach } from "@jest/globals";
import { render, screen, act } from "@testing-library/react";
import { SelectToolsDialog } from "../SelectToolsDialog";
import type { Tool, ToolsResponse, AgentResponse } from "@/types";

// Mock next/link
jest.mock("next/link", () => {
  return {
    __esModule: true,
    default: ({ children, ...props }: { children: React.ReactNode; href: string }) => <a {...props}>{children}</a>,
  };
});

// Mock sonner toast
jest.mock("sonner", () => ({
  toast: {
    warning: jest.fn(),
    error: jest.fn(),
    success: jest.fn(),
  },
}));

// Mock ScrollArea to render children directly
jest.mock("@/components/ui/scroll-area", () => ({
  ScrollArea: ({ children }: { children: React.ReactNode }) => <div data-testid="scroll-area">{children}</div>,
}));

// Helper to create mock tools
function createMockTools(count: number): ToolsResponse[] {
  return Array.from({ length: count }, (_, i) => ({
    id: `tool-${i}`,
    name: `Tool ${i}`,
    description: `Description for tool ${i}`,
    server_name: `server-${i}`,
    server_description: `Server ${i}`,
  })) as unknown as ToolsResponse[];
}

// Helper to create selected tools
function createSelectedTools(count: number): Tool[] {
  return Array.from({ length: count }, (_, i) => ({
    type: "McpServer" as const,
    mcpServer: {
      name: `server-${i}`,
      kind: "ToolServer",
      apiGroup: "kagent.dev",
      toolNames: [`tool-${i}`],
    },
  }));
}

const baseProps = {
  onOpenChange: jest.fn(),
  availableTools: createMockTools(30),
  onToolsSelected: jest.fn(),
  availableAgents: [] as AgentResponse[],
  loadingAgents: false,
  currentAgentNamespace: "default",
};

/**
 * Helper: renders the dialog first closed, then opens it to trigger
 * the useLayoutEffect that copies selectedTools → localSelectedTools.
 */
function renderDialogWithTools(selectedTools: Tool[]) {
  const props = { ...baseProps, selectedTools, open: false };
  const { rerender } = render(<SelectToolsDialog {...props} />);
  // Transition from closed to open to trigger useLayoutEffect
  act(() => {
    rerender(<SelectToolsDialog {...props} open={true} />);
  });
  return { rerender };
}

describe("SelectToolsDialog - Tool Limit Warning (Issue #694)", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it("should NOT show warning when fewer than 20 tools are selected", () => {
    renderDialogWithTools(createSelectedTools(5));

    expect(screen.queryByText(/may increase token usage/i)).not.toBeInTheDocument();
  });

  it("should show warning when 20 or more tools are selected", () => {
    renderDialogWithTools(createSelectedTools(20));

    expect(screen.getByText(/may increase token usage/i)).toBeInTheDocument();
  });

  it("should show warning with correct count when more than 20 tools are selected", () => {
    renderDialogWithTools(createSelectedTools(25));

    expect(screen.getByText(/may increase token usage/i)).toBeInTheDocument();
    expect(screen.getByText(/You have selected 25 tools/)).toBeInTheDocument();
  });

  it("should display the selected count without a maximum limit", () => {
    renderDialogWithTools(createSelectedTools(25));

    // Should show "Selected (25)" not "Selected (25/20)"
    expect(screen.getByText("Selected (25)")).toBeInTheDocument();
    expect(screen.queryByText(/\/20/)).not.toBeInTheDocument();
  });

  it("should not display 'Tool limit reached' blocking message", () => {
    renderDialogWithTools(createSelectedTools(20));

    expect(screen.queryByText(/Tool limit reached/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Deselect a tool to add another/i)).not.toBeInTheDocument();
  });

  it("should show footer text without mentioning a maximum", () => {
    renderDialogWithTools([]);

    expect(screen.getByText(/Select tools for your agent/i)).toBeInTheDocument();
    expect(screen.queryByText(/Select up to/i)).not.toBeInTheDocument();
  });

  it("should not disable tool items when at or above the threshold", () => {
    renderDialogWithTools(createSelectedTools(20));

    // No elements should have the opacity-50 cursor-not-allowed class
    const disabledElements = document.querySelectorAll(".opacity-50.cursor-not-allowed");
    expect(disabledElements.length).toBe(0);
  });
});
