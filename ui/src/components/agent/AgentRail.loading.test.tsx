import { render, screen } from "@testing-library/react";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { AgentInstance, ApiResource } from "@/api";
import { themeFor } from "@/theme/theme";
import { AgentRail } from "./AgentRail";

/**
 * The rail holds its shape while the conversation is being read.
 *
 * All three assertions below were the same defect: something in the rail was derived
 * from the instance, so it arrived a beat late and moved everything under it. A reader
 * opening a conversation saw the nav gain a row, the identity card grow by a line, and
 * the bulk bar appear — three jumps at the moment they were aiming at something.
 */

vi.mock("@/api/hooks/useConversationTitles", () => ({
  useConversationTitles: () => ({}),
}));

const loadingList: ApiResource<AgentInstance[]> = {
  data: undefined,
  isLoading: true,
  isValidating: true,
  error: undefined,
  isEmpty: false,
  refresh: async () => {},
};

/** The rail as a chat page mounts it before the record lands: an id, and nothing else. */
function renderLoadingRail() {
  return render(
    <ThemeProvider theme={themeFor("dark")}>
      <MemoryRouter initialEntries={["/agents/abc123/chat"]}>
        <AgentRail agentRef={{ id: "abc123" }} instances={loadingList} />
      </MemoryRouter>
    </ThemeProvider>,
  );
}

describe("the agent rail while the conversation is being read", () => {
  it("draws the select-all box, and does not let it be used yet", () => {
    renderLoadingRail();

    const selectAll = screen.getByTestId("chat-select-all");
    expect(selectAll).toBeInTheDocument();
    expect(
      selectAll instanceof HTMLInputElement
        ? selectAll
        : selectAll.querySelector("input"),
    ).toBeDisabled();
  });

  it("draws Agent Details in its final place, inert until it has an address", () => {
    renderLoadingRail();

    const details = screen.getByTestId("agent-nav-agent-conversations-pending");
    expect(details).toBeInTheDocument();
    expect(details).toHaveAttribute("aria-disabled", "true");
    // Not a link: there is nowhere to go until the record names the agent.
    expect(details.tagName).not.toBe("A");
    // And not standing in for the real entry, which a click-based suite looks for.
    expect(screen.queryByTestId("agent-nav-agent-conversations")).toBeNull();
  });

  it("reserves the identity card's second line", () => {
    renderLoadingRail();

    // The harness fills this line and only arrives with the record. Empty, it still
    // occupies one — which is what stops the card growing when the read returns.
    // Asserted as the reserved height, not as "the element exists": an empty element
    // exists either way, so counting them passes whether or not the line is held.
    const card = screen.getByTestId("agent-rail-identity");
    const lines = card.querySelectorAll(".ant-typography");
    const secondary = lines[lines.length - 1];
    expect(secondary.textContent).toBe("");
    expect(getComputedStyle(secondary).minHeight).toBe("16px");
  });
});
