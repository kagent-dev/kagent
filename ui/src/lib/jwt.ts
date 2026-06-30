import { decodeJwt, JWTPayload } from "jose";

export function decodeJWT(token: string): JWTPayload | null {
  try {
    return decodeJwt(token);
  } catch {
    return null;
  }
}

export function isTokenExpired(claims: JWTPayload): boolean {
  // OIDC id_tokens are required to carry `exp`. Treat a missing `exp` as expired
  // rather than valid-forever, so a non-compliant/garbled token triggers re-auth
  // instead of being trusted indefinitely.
  if (!claims.exp) return true;
  return Date.now() >= claims.exp * 1000;
}
