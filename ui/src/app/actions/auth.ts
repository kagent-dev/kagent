"use server";

import { headers } from "next/headers";
import { decodeJWT, isTokenExpired } from "@/lib/jwt";

export interface CurrentUser extends Record<string, unknown> {
  sub?: string;
  name?: string;
  preferred_username?: string;
  email?: string;
  groups?: string[];
}

// authenticated → valid, non-expired token forwarded by oauth2-proxy
// expired       → oauth2-proxy session is still valid but the forwarded
//                 id_token is missing/expired → the UI should re-run OIDC
// unsecured     → no Authorization header at all (no oauth2-proxy in front);
//                 the UI must NOT redirect or it would loop with no /oauth2 endpoint
export type AuthStatus = "authenticated" | "expired" | "unsecured";

export interface AuthResult {
  status: AuthStatus;
  user: CurrentUser | null;
}

export async function getAuthResult(): Promise<AuthResult> {
  const headersList = await headers();
  const authHeader = headersList.get("Authorization");

  if (!authHeader?.startsWith("Bearer ")) {
    return { status: "unsecured", user: null };
  }

  const token = authHeader.slice(7);
  const claims = decodeJWT(token);

  if (!claims || isTokenExpired(claims)) {
    return { status: "expired", user: null };
  }

  return { status: "authenticated", user: claims as CurrentUser };
}
