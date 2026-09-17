import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { RootErrorBoundary } from "./RootErrorBoundary";

function Boom(): never {
  throw new Error("kaboom");
}

describe("RootErrorBoundary", () => {
  it("renders a fallback instead of letting a child crash propagate", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});

    render(
      <RootErrorBoundary>
        <Boom />
      </RootErrorBoundary>,
    );

    expect(screen.getByText("Something went wrong")).toBeVisible();
  });
});
