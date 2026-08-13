import React from "react";
import { render, screen } from "@testing-library/react";
import { TruncatableText } from "@/components/chat/TruncatableText";

jest.mock("next/dynamic", () => () => () => null);

describe("TruncatableText", () => {
  it("shows a pulse indicator while rendering streaming text", () => {
    render(<TruncatableText content="Streaming response" isStreaming />);

    expect(screen.getByText("Streaming response")).toHaveClass("whitespace-pre-wrap");
    expect(screen.getByText("...")).toHaveClass("animate-pulse");
  });

  it("does not show the pulse indicator for finalized text", () => {
    render(<TruncatableText content="Final response" />);

    expect(screen.queryByText("...")).not.toBeInTheDocument();
  });
});
