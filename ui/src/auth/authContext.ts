import { createContext, useContext } from "react";
import { UNSECURED } from "./types";
import type { AuthResult, AuthStatus, AuthUser } from "./types";

export interface AuthContextValue extends AuthResult {
  /**
   * True until the first resolution completes.
   *
   * Deliberately not a fourth `AuthStatus`: the three states are the app's
   * model and callers switch on them exhaustively. Resolution is a property of
   * this particular async source, not a state the user is in — and while it is
   * true, `status` already reads `unsecured`, which is the safe answer.
   */
  isResolving: boolean;
  /** Re-reads the session, e.g. after returning from the OIDC flow. */
  refresh: () => void;
}

export const AuthContext = createContext<AuthContextValue>({
  ...UNSECURED,
  isResolving: false,
  refresh: () => {},
});

export function useAuth(): AuthContextValue {
  return useContext(AuthContext);
}

/** Convenience for the common check. */
export function useAuthStatus(): AuthStatus {
  return useContext(AuthContext).status;
}

/** The signed-in user, or null in every other state. */
export function useAuthUser(): AuthUser | null {
  return useContext(AuthContext).user;
}
