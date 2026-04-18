/**
 * @jest-environment jsdom
 */
import { describe, it, expect, jest, beforeEach } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PromptInstructionsTextarea } from "@/components/create/PromptInstructionsTextarea";
import type { PromptTemplateSummary } from "@/types";

jest.mock("@/lib/textareaCaret", () => ({
  getCaretViewportCoords: () => ({ top: 200, left: 200 }),
}));

const mockCatalog: PromptTemplateSummary[] = [
  {
    namespace: "kagent",
    name: "team-prompts",
    keyCount: 2,
    keys: ["system", "tools"],
  },
];

describe("PromptInstructionsTextarea", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it("renders instructions label and textarea", () => {
    const onChange = jest.fn();
    const onPick = jest.fn();
    render(
      <PromptInstructionsTextarea
        value="Hello"
        onChange={onChange}
        disabled={false}
        namespace="kagent"
        onPickInclude={onPick}
        catalogOverride={mockCatalog}
      />,
    );
    expect(document.querySelector('textarea[name="systemMessage"]')).toBeInTheDocument();
  });

  it("opens mention popover when typing @ after whitespace", async () => {
    const user = userEvent.setup();
    const onChange = jest.fn();
    const onPick = jest.fn();

    render(
      <PromptInstructionsTextarea
        value=""
        onChange={onChange}
        disabled={false}
        namespace="kagent"
        onPickInclude={onPick}
        catalogOverride={mockCatalog}
      />,
    );

    const ta = document.querySelector('textarea[name="systemMessage"]') as HTMLTextAreaElement;
    await user.click(ta);
    await user.keyboard("hello @");

    expect(await screen.findByRole("dialog", { name: /Insert prompt fragment/i })).toBeInTheDocument();
    expect(screen.getByText("system")).toBeInTheDocument();
  });
});
