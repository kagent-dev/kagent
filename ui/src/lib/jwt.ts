import { decodeJwt, JWTPayload } from "jose";

// Configurable claim names (from env vars, with defaults)
const CLAIM_USER_ID = process.env.JWT_CLAIM_USER_ID || "sub";
const CLAIM_EMAIL = process.env.JWT_CLAIM_EMAIL || "email";
const CLAIM_NAME = process.env.JWT_CLAIM_NAME || ""; // Empty = try defaults
const CLAIM_GROUPS = process.env.JWT_CLAIM_GROUPS || ""; // Empty = try defaults

export function decodeJWT(token: string): JWTPayload | null {
  try {
    return decodeJwt(token);
  } catch {
    return null;
  }
}

export interface UserClaims {
  user: string;
  email: string;
  name: string;
  groups: string[];
}

export function extractUserFromClaims(claims: JWTPayload): UserClaims {
  return {
    user: getClaimValue(claims, CLAIM_USER_ID, ["sub"]) || "",
    email: getClaimValue(claims, CLAIM_EMAIL, ["email"]) || "",
    name: getClaimValue(claims, CLAIM_NAME, ["name", "preferred_username"]) || "",
    groups: getGroupsClaim(claims),
  };
}

// Get a claim value, trying configured name first, then fallbacks
function getClaimValue(claims: JWTPayload, configured: string, fallbacks: string[]): string | undefined {
  if (configured) {
    const value = claims[configured];
    if (typeof value === "string") return value;
  }
  for (const key of fallbacks) {
    const value = claims[key];
    if (typeof value === "string") return value;
  }
  return undefined;
}

// Get groups claim, trying configured name first, then common provider names
function getGroupsClaim(claims: JWTPayload): string[] {
  const fallbacks = ["groups", "cognito:groups", "roles"];
  const keysToTry = CLAIM_GROUPS ? [CLAIM_GROUPS, ...fallbacks] : fallbacks;

  for (const key of keysToTry) {
    const value = claims[key];
    if (Array.isArray(value)) {
      return value.filter((g): g is string => typeof g === "string");
    }
    if (typeof value === "string") {
      return value
        .split(",")
        .map((g) => g.trim())
        .filter(Boolean);
    }
  }
  return [];
}

export function isTokenExpired(claims: JWTPayload): boolean {
  if (!claims.exp) return false;
  return Date.now() >= claims.exp * 1000;
}
