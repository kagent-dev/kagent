import { decodeJwt, JWTPayload } from "jose";

export function decodeJWT(token: string): JWTPayload | null {
  try {
    return decodeJwt(token);
  } catch {
    return null;
  }
}

export function isTokenExpired(claims: JWTPayload): boolean {
  if (!claims.exp) return false;
  return Date.now() >= claims.exp * 1000;
}
