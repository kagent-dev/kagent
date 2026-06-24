import { getAuthResult } from "@/app/actions/auth";
import { headers } from "next/headers";
import { decodeJWT, isTokenExpired } from "@/lib/jwt";

jest.mock("next/headers", () => ({
  headers: jest.fn(),
}));

jest.mock("@/lib/jwt", () => ({
  decodeJWT: jest.fn(),
  isTokenExpired: jest.fn(),
}));

const mockedHeaders = headers as jest.Mock;
const mockedDecodeJWT = decodeJWT as jest.Mock;
const mockedIsTokenExpired = isTokenExpired as jest.Mock;

function withAuthorizationHeader(value: string | null) {
  mockedHeaders.mockResolvedValue({
    get: (name: string) => (name === "Authorization" ? value : null),
  });
}

describe("getAuthResult", () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it("returns unsecured when there is no Authorization header", async () => {
    withAuthorizationHeader(null);

    const result = await getAuthResult();

    expect(result).toEqual({ status: "unsecured", user: null });
    expect(mockedDecodeJWT).not.toHaveBeenCalled();
  });

  it("returns unsecured when the header is not a Bearer token", async () => {
    withAuthorizationHeader("Basic abc123");

    const result = await getAuthResult();

    expect(result).toEqual({ status: "unsecured", user: null });
  });

  it("returns expired when the token cannot be decoded", async () => {
    withAuthorizationHeader("Bearer not-a-jwt");
    mockedDecodeJWT.mockReturnValue(null);

    const result = await getAuthResult();

    expect(mockedDecodeJWT).toHaveBeenCalledWith("not-a-jwt");
    expect(result).toEqual({ status: "expired", user: null });
  });

  it("returns expired when the token is decoded but expired", async () => {
    withAuthorizationHeader("Bearer expired.jwt.token");
    mockedDecodeJWT.mockReturnValue({ sub: "user-1", exp: 1 });
    mockedIsTokenExpired.mockReturnValue(true);

    const result = await getAuthResult();

    expect(result).toEqual({ status: "expired", user: null });
  });

  it("returns authenticated with the decoded claims for a valid token", async () => {
    const claims = { sub: "user-1", email: "user@example.com", groups: ["admins"] };
    withAuthorizationHeader("Bearer valid.jwt.token");
    mockedDecodeJWT.mockReturnValue(claims);
    mockedIsTokenExpired.mockReturnValue(false);

    const result = await getAuthResult();

    expect(result).toEqual({ status: "authenticated", user: claims });
  });
});
