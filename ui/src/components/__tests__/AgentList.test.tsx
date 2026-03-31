import { afterAll, beforeEach, describe, expect, it, jest } from "@jest/globals";
import { render, screen } from "@testing-library/react";
import React from "react";

const mockUseAgents = jest.fn();

jest.mock("@/components/AgentsProvider", () => ({
  useAgents: () => mockUseAgents(),
}));

jest.mock("next/link", () => ({
  __esModule: true,
  default: ({ children, href }: { children: React.ReactNode; href: string }) => <a href={href}>{children}</a>,
}));

describe("AgentList", () => {
  const originalEnv = process.env;

  beforeEach(() => {
    jest.resetModules();
    process.env = { ...originalEnv };
    mockUseAgents.mockReturnValue({
      agents: [],
      loading: false,
      error: null,
    });
  });

  afterAll(() => {
    process.env = originalEnv;
  });

  it("shows the create action when read-only mode is disabled", async () => {
    delete process.env.NEXT_PUBLIC_READONLY_MODE;
    const { default: AgentList } = await import("../AgentList");

    render(<AgentList />);

    expect(screen.getByRole("link", { name: /create new agent/i })).toBeInTheDocument();
  });

  it("hides the create action when read-only mode is enabled", async () => {
    process.env.NEXT_PUBLIC_READONLY_MODE = "true";
    const { default: AgentList } = await import("../AgentList");

    render(<AgentList />);

    expect(screen.queryByRole("link", { name: /create new agent/i })).not.toBeInTheDocument();
    expect(screen.getByText(/this ui is running in read-only mode/i)).toBeInTheDocument();
  });
});
