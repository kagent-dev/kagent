/**
 * Authentication for the SPA.
 *
 * The Next server action this replaces read the `Authorization` header
 * oauth2-proxy injected into the server-side request and decoded the JWT. A
 * browser cannot read its own request headers, so the session is resolved by
 * asking the proxy instead — see `AuthSource` for the seam that keeps that
 * choice swappable.
 */
export { AuthProvider } from "./AuthProvider";
export { AuthContext, useAuth, useAuthStatus, useAuthUser } from "./authContext";
export type { AuthContextValue } from "./authContext";
export {
  classifyUserInfoResponse,
  createOAuth2ProxyAuthSource,
  toAuthUser,
} from "./oauth2ProxyAuthSource";
export type { OAuth2ProxyAuthSourceOptions } from "./oauth2ProxyAuthSource";
export { UNSECURED } from "./types";
export type { AuthResult, AuthSource, AuthStatus, AuthUser } from "./types";
