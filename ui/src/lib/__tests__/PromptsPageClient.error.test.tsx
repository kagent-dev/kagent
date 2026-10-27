import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useRouter, useSearchParams } from "next/navigation";
import { PromptsPageClient } from "@/components/prompts/PromptsPageClient";

jest.mock("@/components/NamespaceCombobox", () => ({
  NamespaceCombobox: ({
    value,
    onValueChange,
  }: {
    value?: string;
    onValueChange: (value: string) => void;
  }) => (
    <select
      aria-label="Namespace"
      value={value || ""}
      onChange={(event) => onValueChange(event.target.value)}
    >
      <option value="dev-kagent">dev-kagent</option>
      <option value="dev-shared">dev-shared</option>
    </select>
  ),
}));

jest.mock("next/navigation", () => ({
  useRouter: jest.fn(),
  useSearchParams: jest.fn(),
}));

const mockUseRouter = useRouter as jest.Mock;
const mockUseSearchParams = useSearchParams as jest.Mock;

const LIST_ERROR = "Failed to list prompt template ConfigMaps";

function setup() {
  const replace = jest.fn();
  mockUseRouter.mockReturnValue({ replace });
  mockUseSearchParams.mockReturnValue(new URLSearchParams());
  return { replace };
}

describe("PromptsPageClient error state", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it("keeps the namespace selector visible when listing fails", () => {
    setup();

    render(<PromptsPageClient namespace="dev-kagent" items={[]} error={LIST_ERROR} />);

    expect(screen.getByRole("heading", { name: "Prompt Libraries" })).toBeInTheDocument();
    expect(screen.getByLabelText("Namespace")).toHaveValue("dev-kagent");
    expect(screen.getByRole("alert")).toHaveTextContent(LIST_ERROR);
    expect(screen.queryByText("Error Encountered")).not.toBeInTheDocument();
    expect(screen.queryByText("No prompt libraries in this namespace")).not.toBeInTheDocument();
  });

  it("navigates to the chosen namespace so the user can recover", async () => {
    const user = userEvent.setup();
    const { replace } = setup();

    render(<PromptsPageClient namespace="dev-kagent" items={[]} error={LIST_ERROR} />);

    await user.selectOptions(screen.getByLabelText("Namespace"), "dev-shared");

    expect(replace).toHaveBeenCalledWith("/prompts?namespace=dev-shared", { scroll: false });
  });
});
