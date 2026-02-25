import { render, screen, waitFor, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { NamespaceSelector } from "../NamespaceSelector";

// Mock listNamespaces
const mockListNamespaces = jest.fn();
jest.mock("@/app/actions/namespaces", () => ({
  listNamespaces: () => mockListNamespaces(),
}));

// Mock useSidebar
const mockSidebarState = jest.fn(() => "expanded");
jest.mock("@/components/ui/sidebar", () => ({
  useSidebar: () => ({ state: mockSidebarState() }),
}));

// Mock UI primitives used by NamespaceSelector
jest.mock("@/components/ui/button", () => ({
  Button: ({ children, disabled, ...props }: React.PropsWithChildren<{ disabled?: boolean }>) => (
    <button disabled={disabled} {...props}>
      {children}
    </button>
  ),
}));

jest.mock("@/components/ui/popover", () => {
  const React = require("react");
  const PopoverContext = React.createContext({ open: false, setOpen: (_: boolean) => {} });
  return {
    Popover: ({ open, onOpenChange, children }: { open: boolean; onOpenChange: (v: boolean) => void; children: React.ReactNode }) => {
      return (
        <PopoverContext.Provider value={{ open, setOpen: onOpenChange }}>
          <div data-testid="popover">{children}</div>
        </PopoverContext.Provider>
      );
    },
    PopoverTrigger: ({ children, asChild }: { children: React.ReactNode; asChild?: boolean }) => (
      <div data-testid="popover-trigger">{children}</div>
    ),
    PopoverContent: ({ children }: { children: React.ReactNode }) => (
      <div data-testid="popover-content">{children}</div>
    ),
  };
});

jest.mock("@/components/ui/command", () => ({
  Command: ({ children }: React.PropsWithChildren) => <div data-testid="command">{children}</div>,
  CommandInput: ({ placeholder }: { placeholder?: string }) => (
    <input data-testid="command-input" placeholder={placeholder} />
  ),
  CommandList: ({ children }: React.PropsWithChildren) => <div data-testid="command-list">{children}</div>,
  CommandEmpty: ({ children }: React.PropsWithChildren) => <div data-testid="command-empty">{children}</div>,
  CommandGroup: ({ children }: React.PropsWithChildren) => <div data-testid="command-group">{children}</div>,
  CommandItem: ({ children, onSelect, value }: React.PropsWithChildren<{ onSelect?: (v: string) => void; value?: string }>) => (
    <div data-testid="command-item" data-value={value} onClick={() => onSelect?.(value || "")}>
      {children}
    </div>
  ),
}));

jest.mock("@/components/ui/tooltip", () => ({
  Tooltip: ({ children }: React.PropsWithChildren) => <div data-testid="tooltip">{children}</div>,
  TooltipContent: ({ children }: React.PropsWithChildren) => (
    <div data-testid="tooltip-content">{children}</div>
  ),
  TooltipProvider: ({ children }: React.PropsWithChildren) => <div>{children}</div>,
  TooltipTrigger: ({ children, asChild }: React.PropsWithChildren<{ asChild?: boolean }>) => (
    <div data-testid="tooltip-trigger">{children}</div>
  ),
}));

describe("NamespaceSelector", () => {
  const mockOnValueChange = jest.fn();

  beforeEach(() => {
    jest.clearAllMocks();
    mockSidebarState.mockReturnValue("expanded");
    mockListNamespaces.mockResolvedValue({
      data: [
        { name: "default", status: "Active" },
        { name: "kagent", status: "Active" },
        { name: "production", status: "Active" },
      ],
    });
  });

  it("renders namespace name from value prop", async () => {
    render(<NamespaceSelector value="kagent" onValueChange={mockOnValueChange} />);

    await waitFor(() => {
      // The trigger button displays the selected namespace
      const trigger = screen.getByRole("combobox");
      expect(trigger).toHaveTextContent("kagent");
    });
  });

  it("shows loading spinner while namespaces are loading", () => {
    // Make the promise hang
    mockListNamespaces.mockReturnValue(new Promise(() => {}));

    render(<NamespaceSelector value="" onValueChange={mockOnValueChange} />);

    expect(screen.getByText("Loading...")).toBeInTheDocument();
  });

  it("calls onValueChange when a namespace item is clicked", async () => {
    render(<NamespaceSelector value="kagent" onValueChange={mockOnValueChange} />);

    await waitFor(() => {
      expect(screen.getAllByTestId("command-item")).toHaveLength(3);
    });

    const items = screen.getAllByTestId("command-item");
    const productionItem = items.find((el) => el.getAttribute("data-value") === "production");
    expect(productionItem).toBeDefined();

    await act(async () => {
      await userEvent.click(productionItem!);
    });

    expect(mockOnValueChange).toHaveBeenCalledWith("production");
  });

  it("selects default namespace when value is empty", async () => {
    render(<NamespaceSelector value="" onValueChange={mockOnValueChange} />);

    await waitFor(() => {
      // Should prefer "kagent" as default
      expect(mockOnValueChange).toHaveBeenCalledWith("kagent");
    });
  });

  it("renders icon-only with tooltip when sidebar is collapsed", async () => {
    mockSidebarState.mockReturnValue("collapsed");

    render(<NamespaceSelector value="kagent" onValueChange={mockOnValueChange} />);

    await waitFor(() => {
      expect(screen.getByTestId("tooltip")).toBeInTheDocument();
      expect(screen.getByTestId("tooltip-content")).toHaveTextContent("kagent");
    });

    // Should NOT render popover trigger (expanded-only UI)
    expect(screen.queryByTestId("popover")).not.toBeInTheDocument();
  });

  it("renders namespace list after loading", async () => {
    render(<NamespaceSelector value="kagent" onValueChange={mockOnValueChange} />);

    await waitFor(() => {
      const items = screen.getAllByTestId("command-item");
      expect(items).toHaveLength(3);
    });
  });
});
