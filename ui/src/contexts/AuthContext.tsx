"use client";

import React, { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { getCurrentUser, type CurrentUser, type AuthStatus } from "@/app/actions/auth";

// oauth2-proxy endpoint that (re)starts the OIDC flow. Client components can only
// read NEXT_PUBLIC_* env vars at runtime, so this mirrors the server-side
// SSO_REDIRECT_PATH (used by the login page) via NEXT_PUBLIC_SSO_REDIRECT_PATH,
// which the Helm chart injects from ui.auth.ssoRedirectPath.
const SSO_REDIRECT_PATH = process.env.NEXT_PUBLIC_SSO_REDIRECT_PATH || "/oauth2/start";
// Guards against redirect loops if re-auth keeps returning a stale token.
const REAUTH_GUARD_KEY = "kagent_reauth_attempt";
// Wide enough to cover a slow IdP round-trip so a genuinely in-flight re-auth
// isn't misread as a failed loop, while still catching a fast redirect loop.
const REAUTH_GUARD_WINDOW_MS = 60_000;

interface AuthContextValue {
  user: CurrentUser | null;
  status: AuthStatus;
  isLoading: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<CurrentUser | null>(null);
  const [status, setStatus] = useState<AuthStatus>("unsecured");
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  const fetchUser = async () => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await getCurrentUser();
      setStatus(result.status);
      setUser(result.user);
    } catch (e) {
      setError(e instanceof Error ? e : new Error("Failed to fetch user"));
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchUser();
  }, []);

  // When oauth2-proxy's session cookie is still valid but the forwarded
  // id_token has expired, re-run the OIDC flow to mint a fresh token instead
  // of silently rendering a logged-out UI. Only triggers in secured ("expired")
  // mode — never in "unsecured" mode where there is no /oauth2 endpoint.
  useEffect(() => {
    if (isLoading || status !== "expired" || typeof window === "undefined") return;

    const lastAttempt = Number(sessionStorage.getItem(REAUTH_GUARD_KEY) || "0");
    if (Date.now() - lastAttempt < REAUTH_GUARD_WINDOW_MS) {
      setError(
        new Error("Authentication expired and re-authentication did not refresh the session.")
      );
      return;
    }
    sessionStorage.setItem(REAUTH_GUARD_KEY, String(Date.now()));
    const rd = encodeURIComponent(window.location.pathname + window.location.search);
    window.location.assign(`${SSO_REDIRECT_PATH}?rd=${rd}`);
  }, [isLoading, status]);

  useEffect(() => {
    if (status === "authenticated" && typeof window !== "undefined") {
      sessionStorage.removeItem(REAUTH_GUARD_KEY);
    }
  }, [status]);

  return (
    <AuthContext.Provider value={{ user, status, isLoading, error, refetch: fetchUser }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}
