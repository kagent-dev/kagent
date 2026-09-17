import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { AuthContext } from "./authContext";
import { createOAuth2ProxyAuthSource } from "./oauth2ProxyAuthSource";
import {
  clearReauthenticationAttempt,
  startReauthentication,
} from "./reauthenticate";
import { UNSECURED } from "./types";
import type { AuthResult, AuthSource } from "./types";

interface AuthProviderProps {
  /** Defaults to oauth2-proxy. Swap here to move to a backend `/api/me`. */
  source?: AuthSource;
  children: ReactNode;
}

export function AuthProvider({ source, children }: AuthProviderProps) {
  const authSource = useMemo(
    () => source ?? createOAuth2ProxyAuthSource(),
    [source],
  );

  // Starts `unsecured`, which is both the safe default and the correct answer
  // for the overwhelmingly common local case, so nothing has to special-case
  // the window before the first resolution lands.
  const [result, setResult] = useState<AuthResult>(UNSECURED);
  const [isResolving, setResolving] = useState(true);
  const [reloadToken, setReloadToken] = useState(0);

  // Flipped here rather than inside the effect: the effect's job is to start
  // the request, and re-entering the resolving state is something the caller
  // does, not something the render does.
  const refresh = useCallback(() => {
    setResolving(true);
    setReloadToken((n) => n + 1);
  }, []);

  useEffect(() => {
    // Stale results are discarded with a flag rather than by aborting the
    // request. StrictMode mounts, unmounts and remounts in development, so an
    // abort on cleanup cancels the first probe every time — and the browser
    // logs each cancelled request as `net::ERR_FAILED`, a console error for
    // something that is not a failure. One small GET is cheaper than that
    // noise, and cheaper than the flaky test it caused.
    let active = true;

    void authSource.resolve().then((next) => {
      // `resolve` never rejects, so there is no error branch to handle — an
      // unreachable source has already been reported as `unsecured`.
      if (!active) return;
      setResult(next);
      setResolving(false);
    });

    return () => {
      active = false;
    };
  }, [authSource, reloadToken]);

  // Re-run the sign-in when the proxy says the session has lapsed.
  //
  // `AuthStatus` has said the UI should do this since these types were written, and
  // nothing did: the header offered "Session expired — sign in", which navigated to a
  // page offering "Sign in with SSO". Two clicks to recover from something the reader did
  // not do, where the UI this replaced recovered on its own.
  //
  // Only for `expired`, never for `unsecured` — there is no `/oauth2` endpoint in that
  // state and redirecting would bounce the browser between the app and a 404. And only
  // once: `startReauthentication` declines a second attempt inside its window, which
  // leaves the manual buttons as the way out of a proxy that keeps handing back a token
  // it will not refresh.
  useEffect(() => {
    if (isResolving) return;

    if (result.status === "authenticated") {
      clearReauthenticationAttempt();
      return;
    }

    if (result.status === "expired") startReauthentication();
  }, [isResolving, result.status]);

  const value = useMemo(
    () => ({ ...result, isResolving, refresh }),
    [result, isResolving, refresh],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
