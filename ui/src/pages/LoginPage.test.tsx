import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ThemeProvider } from "@emotion/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { LoginPage } from "./LoginPage";

const useAuth = vi.hoisted(() => vi.fn());

vi.mock("@/auth", () => ({ useAuth }));

const assign = vi.fn();

beforeEach(() => {
  assign.mockClear();
  useAuth.mockReturnValue({ status: "expired", user: undefined });
  window.environmentVariables = { SSO_REDIRECT_PATH: "/oauth2/start" };
  Object.defineProperty(window, "location", {
    configurable: true,
    value: { pathname: "/login", search: "", hash: "", assign },
  });
});

function renderAt(entry: string) {
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <MemoryRouter initialEntries={[entry]}>
        <LoginPage />
      </MemoryRouter>
    </ThemeProvider>,
  );
}

/**
 * The deep link a signed-out reader followed, which oauth2-proxy's `sign_in.html`
 * forwards here as `rd`. It is the whole point of the page carrying a query string.
 */
describe("LoginPage sign-in destination", () => {
  it("returns the reader to the page the proxy intercepted", async () => {
    renderAt("/login?rd=%2Fagents%2Fkagent%2Fk8s-agent%2Fchat");

    await userEvent.click(screen.getByTestId("login-submit"));

    expect(assign).toHaveBeenCalledWith(
      "/oauth2/start?rd=%2Fagents%2Fkagent%2Fk8s-agent%2Fchat",
    );
  });

  it("refuses a destination that leaves the origin", async () => {
    // `/login?rd=...` is a link anybody can send, so an off-site `rd` must not
    // become where an authenticated session lands.
    renderAt("/login?rd=https%3A%2F%2Fevil.example.com%2Fphish");

    await userEvent.click(screen.getByTestId("login-submit"));

    expect(assign).toHaveBeenCalledWith("/oauth2/start?rd=%2F");
  });

  it("falls back to the page being read when there is no rd", async () => {
    // Arriving from the header's "Session expired" button: nothing was intercepted,
    // and the reader is still where they were.
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { pathname: "/agents/foo", search: "?tab=logs", hash: "", assign },
    });

    renderAt("/login");

    await userEvent.click(screen.getByTestId("login-submit"));

    expect(assign).toHaveBeenCalledWith("/oauth2/start?rd=%2Fagents%2Ffoo%3Ftab%3Dlogs");
  });
});
