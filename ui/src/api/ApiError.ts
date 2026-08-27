/**
 * The single error type every API call rejects with.
 *
 * Callers branch on `status` (a 404 is often "does not exist" rather than a
 * failure) and on `kind` for the cases that never reach the server at all.
 *
 * ## Why an HTTP status survived the move to gRPC
 *
 * The application API is gRPC-Web now, and gRPC has no statuses — it has codes.
 * Every screen that reads this type, though, was written against statuses, and
 * "not found" is the distinction those screens actually make. So a `ConnectError`
 * is mapped onto the status its code stands for (`fromConnectError` below), the
 * code is kept alongside it under `code` for anything that wants the real thing,
 * and no page had to learn a second vocabulary for the same idea.
 *
 * The mapping is the one the Connect protocol itself specifies, not one invented
 * here, so a code and the status it arrives as cannot disagree about what
 * happened.
 */

import { Code, ConnectError } from "@connectrpc/connect";

export type ApiErrorKind =
  /** The server answered with a failure — a non-2xx status, or a gRPC error code. */
  | "http"
  /** The request never reached a server — DNS, connection refused, CORS. */
  | "network"
  /** The client gave up waiting. */
  | "timeout"
  /** A successful response whose payload was not the shape we expected. */
  | "parse";

export class ApiError extends Error {
  readonly kind: ApiErrorKind;
  /** HTTP status, or the status the gRPC code stands for. */
  readonly status?: number;
  /** The gRPC code's name (`NotFound`, `PermissionDenied`, …), when there was one. */
  readonly code?: string;
  /**
   * What was called, to make a failure traceable.
   *
   * A URL for the handful of endpoints still served over HTTP, and the RPC's
   * fully-qualified name — `kagent.api.v1alpha1.AgentService/ListAgents` — for
   * everything else. The field keeps its name because every caller that logs it
   * only wants "which call was this".
   */
  readonly url: string;

  constructor(
    message: string,
    options: {
      kind: ApiErrorKind;
      url: string;
      status?: number;
      code?: string;
      cause?: unknown;
    },
  ) {
    super(message, { cause: options.cause });
    this.name = "ApiError";
    this.kind = options.kind;
    this.status = options.status;
    this.code = options.code;
    this.url = options.url;
  }

  /** True when the backend could not be reached at all. */
  get isUnreachable(): boolean {
    return this.kind === "network" || this.kind === "timeout";
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

/** True when `error` is a 404 — worth distinguishing from a real failure. */
export function isNotFound(error: unknown): boolean {
  return isApiError(error) && error.status === 404;
}

/**
 * The status each gRPC code stands for, from the Connect protocol's own table.
 *
 * Kept explicit rather than computed: the two that matter most to this app —
 * `NotFound` as a 404 and `Unauthenticated` as a 401 — are the ones a reader
 * will come here to check.
 */
const STATUS_BY_CODE: Record<Code, number> = {
  [Code.Canceled]: 499,
  [Code.Unknown]: 500,
  [Code.InvalidArgument]: 400,
  [Code.DeadlineExceeded]: 504,
  [Code.NotFound]: 404,
  [Code.AlreadyExists]: 409,
  [Code.PermissionDenied]: 403,
  [Code.ResourceExhausted]: 429,
  [Code.FailedPrecondition]: 400,
  [Code.Aborted]: 409,
  [Code.OutOfRange]: 400,
  [Code.Unimplemented]: 501,
  [Code.Internal]: 500,
  [Code.Unavailable]: 503,
  [Code.DataLoss]: 500,
  [Code.Unauthenticated]: 401,
};

/**
 * Turns anything a gRPC-Web call can reject with into an `ApiError`.
 *
 * Three of the codes are not really "the server said no" and are reported as the
 * thing they are, because `isUnreachable` is what the shell uses to decide
 * between "this failed" and "the backend is down":
 *
 * - `Unavailable` is what the transport reports when it could not connect at all,
 *   so it becomes `network`.
 * - `DeadlineExceeded` is a timeout.
 * - `Canceled` is almost always the caller's own abort. It is *not* converted —
 *   see `rethrowIfAborted`, which the operations call first so SWR can recognise
 *   an abort and stay quiet.
 *
 * A non-`ConnectError` reaching here is a bug in the client rather than a server
 * failure, and is reported as `parse` so it does not masquerade as one.
 */
export function fromConnectError(error: unknown, rpc: string): ApiError {
  if (error instanceof ApiError) return error;

  if (error instanceof ConnectError) {
    return new ApiError(error.rawMessage || error.message, {
      kind: kindOf(error.code),
      status: STATUS_BY_CODE[error.code] ?? 500,
      code: Code[error.code],
      url: rpc,
      cause: error,
    });
  }

  return new ApiError(
    error instanceof Error ? error.message : `The call to ${rpc} failed.`,
    { kind: "parse", url: rpc, cause: error },
  );
}

function kindOf(code: Code): ApiErrorKind {
  if (code === Code.Unavailable) return "network";
  if (code === Code.DeadlineExceeded) return "timeout";
  return "http";
}

/**
 * Passes a caller-driven abort straight through, undressed.
 *
 * SWR recognises an `AbortError` and keeps quiet about it; wrapped in an
 * `ApiError` it becomes an error state on screen for a request the app itself
 * chose to drop — a page that navigated away rendering "the request failed".
 */
export function rethrowIfAborted(error: unknown, signal: AbortSignal | undefined): void {
  if (signal?.aborted) throw error;
}
