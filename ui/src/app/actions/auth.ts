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

export async function getCurrentUser(): Promise<CurrentUser | null> {
  const headersList = await headers();
  const authHeader = headersList.get("Authorization");

  if (!authHeader?.startsWith("Bearer ")) {
    return null;
  }

  const token = authHeader.slice(7);
  const claims = decodeJWT(token);

  if (!claims || isTokenExpired(claims)) {
    return null;
  }

  return claims as CurrentUser;
}

export async function getUserIdClaim(): Promise<string> {
  return process.env.KAGENT_USER_ID_CLAIM || "sub";
}
