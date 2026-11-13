import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { expect, it, vi } from "vitest";
import { HarnessNewPage } from "./HarnessNewPage";

vi.mock("@/api", () => ({ useHarnesses: () => ({ data: [], error: undefined }) }));
vi.mock("@/components/harness-form/HarnessForm", () => ({ HarnessForm: () => <div data-testid="form" /> }));
vi.mock("@/components/Structure/PageFrame", () => ({ PageFrame: ({ children }: { children: React.ReactNode }) => <>{children}</> }));
vi.mock("@/components/harness-form/HarnessFields", () => ({ HarnessIntro: () => null }));

it("says so when the harness to copy is not in the list", () => {
  render(<MemoryRouter initialEntries={["/new?from=ns/missing"]}><HarnessNewPage /></MemoryRouter>);
  expect(screen.getByTestId("harness-copy-not-found")).toBeInTheDocument();
  expect(screen.getByTestId("form")).toBeInTheDocument();
});
