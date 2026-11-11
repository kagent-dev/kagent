import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { backLinkState, useBackLink } from "./backLink";

const FALLBACK = { to: "/agents?tab=templates", label: "Back to templates" };

function Probe() {
  const back = useBackLink(FALLBACK);
  return <a href={back.to}>{back.label}</a>;
}

function renderAt(state: unknown) {
  render(
    <MemoryRouter initialEntries={[{ pathname: "/agent-templates/kagent/a", state }]}>
      <Probe />
    </MemoryRouter>,
  );
  return screen.getByRole("link");
}

describe("useBackLink", () => {
  it("leads back to the page that opened this one, search included", () => {
    const link = renderAt(
      backLinkState({ pathname: "/agents/abc/chat", search: "?mock=ok" }, "Back to conversation"),
    );
    expect(link.textContent).toBe("Back to conversation");
    expect(link.getAttribute("href")).toBe("/agents/abc/chat?mock=ok");
  });

  it("falls back to the list on a direct visit", () => {
    const link = renderAt(undefined);
    expect(link.textContent).toBe("Back to templates");
    expect(link.getAttribute("href")).toBe(FALLBACK.to);
  });

  it.each([
    { from: "https://example.com/", fromLabel: "Back" },
    { from: "//example.com/", fromLabel: "Back" },
    { from: "/agents", fromLabel: "" },
    { from: 42, fromLabel: "Back" },
    "not an object",
  ])("ignores state that is not an in-app origin: %j", (state) => {
    expect(renderAt(state).textContent).toBe("Back to templates");
  });
});
