import React from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { AuthProvider, useAuth } from "@/contexts/AuthContext";
import { getCurrentUser, type AuthResult } from "@/app/actions/auth";

jest.mock("@/app/actions/auth", () => ({
  getCurrentUser: jest.fn(),
}));

const mockedGetCurrentUser = getCurrentUser as jest.Mock;

const REAUTH_GUARD_KEY = "kagent_reauth_attempt";

function Consumer() {
  const { status, error, isLoading } = useAuth();
  return (
    <div>
      <span data-testid="status">{status}</span>
      <span data-testid="error">{error?.message ?? ""}</span>
      <span data-testid="loading">{String(isLoading)}</span>
    </div>
  );
}

function renderProvider() {
  return render(
    <AuthProvider>
      <Consumer />
    </AuthProvider>
  );
}

function setResult(result: AuthResult) {
  mockedGetCurrentUser.mockResolvedValue(result);
}

// jsdom fully hardens window.location (it is non-configurable and assign is
// read-only), so the redirect call (window.location.assign) cannot be spied on
// here. Instead we assert the observable contract that gates the redirect: the
// sessionStorage re-auth guard, written immediately before assign() is invoked
// ("guard written" == "redirect attempted"). console.error is silenced to drop
// jsdom's expected "Not implemented: navigation" noise from the assign() call.
describe("AuthProvider re-auth behavior", () => {
  let consoleErrorSpy: jest.SpyInstance;

  beforeEach(() => {
    jest.clearAllMocks();
    sessionStorage.clear();
    consoleErrorSpy = jest.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    consoleErrorSpy.mockRestore();
  });

  it("attempts an OIDC re-auth redirect when the session is expired", async () => {
    setResult({ status: "expired", user: null });

    renderProvider();

    await waitFor(() => {
      expect(sessionStorage.getItem(REAUTH_GUARD_KEY)).not.toBeNull();
    });
    expect(screen.getByTestId("error").textContent).toBe("");
  });

  it("does not redirect in unsecured mode (no oauth2-proxy in front)", async () => {
    setResult({ status: "unsecured", user: null });

    renderProvider();

    await waitFor(() => {
      expect(screen.getByTestId("loading").textContent).toBe("false");
    });
    expect(sessionStorage.getItem(REAUTH_GUARD_KEY)).toBeNull();
    expect(screen.getByTestId("error").textContent).toBe("");
  });

  it("does not redirect again within the guard window and surfaces an error", async () => {
    const previousAttempt = String(Date.now());
    sessionStorage.setItem(REAUTH_GUARD_KEY, previousAttempt);
    setResult({ status: "expired", user: null });

    renderProvider();

    await waitFor(() => {
      expect(screen.getByTestId("error").textContent).toMatch(
        /re-authentication did not refresh/i
      );
    });
    // The guard is not re-stamped when the loop is detected.
    expect(sessionStorage.getItem(REAUTH_GUARD_KEY)).toBe(previousAttempt);
  });

  it("clears the re-auth guard once authenticated", async () => {
    sessionStorage.setItem(REAUTH_GUARD_KEY, String(Date.now()));
    setResult({ status: "authenticated", user: { sub: "user-1" } });

    renderProvider();

    await waitFor(() => {
      expect(screen.getByTestId("status").textContent).toBe("authenticated");
    });
    expect(sessionStorage.getItem(REAUTH_GUARD_KEY)).toBeNull();
  });
});
