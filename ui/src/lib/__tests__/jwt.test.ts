// `jose` is ESM-only and next/jest doesn't transform it, so mock decodeJwt with
// a base64url payload parser that mirrors jose's behavior (decode-only, throws
// on malformed input). isTokenExpired is pure and exercises the real code.
jest.mock("jose", () => ({
  decodeJwt: (token: string) => {
    const parts = token.split(".");
    if (parts.length < 2 || !parts[1]) {
      throw new Error("Invalid JWT");
    }
    return JSON.parse(Buffer.from(parts[1], "base64url").toString("utf8"));
  },
}));

import { decodeJWT, isTokenExpired } from "@/lib/jwt";

function makeToken(payload: Record<string, unknown>): string {
  const b64 = (obj: unknown) =>
    Buffer.from(JSON.stringify(obj)).toString("base64url");
  return `${b64({ alg: "none", typ: "JWT" })}.${b64(payload)}.`;
}

describe("decodeJWT", () => {
  it("decodes a well-formed token's claims", () => {
    const claims = decodeJWT(makeToken({ sub: "user-1", exp: 9999999999 }));
    expect(claims).toMatchObject({ sub: "user-1", exp: 9999999999 });
  });

  it("returns null for a malformed token", () => {
    expect(decodeJWT("not-a-jwt")).toBeNull();
    expect(decodeJWT("")).toBeNull();
  });
});

describe("isTokenExpired", () => {
  it("is false for a token expiring in the future", () => {
    const future = Math.floor(Date.now() / 1000) + 3600;
    expect(isTokenExpired({ exp: future })).toBe(false);
  });

  it("is true for a token whose exp is in the past", () => {
    const past = Math.floor(Date.now() / 1000) - 3600;
    expect(isTokenExpired({ exp: past })).toBe(true);
  });

  it("is true when exp is missing (treated as expired, not valid-forever)", () => {
    expect(isTokenExpired({})).toBe(true);
  });
});
