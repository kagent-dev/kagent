/**
 * Reading the controller's proto messages as this app's domain types.
 *
 * Two conventions do most of the work here, and both are worth stating because
 * neither is obvious from the generated code.

 * (A `google.protobuf.Struct` field that is *not* wrapped in a `StructuredObject` —
 * `GetCurrentUserResponse.claims`, for instance — is generated as a bare
 * `JsonObject` and needs no unwrapping at all. It is the envelope that needs a
 * helper, not the Struct.)
 *
 * **`StructuredObject` is the Kubernetes object, as JSON.** `common.proto`
 * defines it as `{api_version, kind, value}` where `value` is a
 * `google.protobuf.Struct` — and the Go side fills it by `json.Marshal`ing the
 * custom resource (`go/api/structuredobject`). So the payload inside it is the
 * same shape the REST API used to return, which is why the domain types in
 * `../domain` survived the move: the work is unwrapping the envelope, not
 * redescribing what is in it. protoc-gen-es represents a `Struct` field directly
 * as `JsonObject`, so there is nothing to decode.
 *
 * **Proto3 has no absent repeated field.** A collection the controller has
 * nothing for arrives as an empty array rather than as `null` — the opposite of
 * the REST API, where Go marshalled a nil slice as JSON `null`. That is a
 * genuine improvement, but only for the live path: a hand-written fake or an
 * override can still hand over `undefined`, so collections are normalised here
 * once rather than defended against at each use.
 */

import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import type { JsonObject } from "@bufbuild/protobuf";
import { ApiError } from "../ApiError";
import type { ResourceReference, StructuredObject } from "@/generated/kagent/api/v1alpha1/common_pb";

/**
 * The API group and version every kagent custom resource is written under.
 *
 * Note that this is *not* the version in the proto package. The gRPC surface is
 * `kagent.api.v1alpha1` and the custom resources it carries are
 * `kagent.dev/v1alpha3`, so the two version numbers in play are deliberately
 * different: one versions the transport API, the other versions the CRDs travelling
 * inside it. Reading a `v1alpha1` import beside a `v1alpha3` payload looks like a
 * mistake and is not one.
 */
export const KAGENT_API_VERSION = "kagent.dev/v1alpha3";

/** A missing collection is an empty one — an absent list is not an error. */
export function list<T>(items: readonly T[] | undefined): T[] {
  return items ? [...items] : [];
}

/**
 * The Kubernetes object inside a `StructuredObject`.
 *
 * Throws rather than returning a partial object, because every caller goes on to
 * read a field of it: an envelope with no value produces an object whose every
 * field is `undefined`, which renders as a page full of blanks and no error —
 * the failure this codebase spends most of its effort not having.
 */
export function unwrap<T>(
  object: StructuredObject | undefined,
  rpc: string,
  what: string,
): T {
  const value = object?.value;
  if (!value) {
    throw new ApiError(`The API returned no ${what}.`, { kind: "parse", url: rpc });
  }
  return value as T;
}

/** Wraps a Kubernetes object for a write, in the envelope the controller decodes. */
export function wrap(
  kind: string,
  value: object,
  apiVersion = KAGENT_API_VERSION,
): { apiVersion: string; kind: string; value: JsonObject } {
  // `kind` must match what the handler expects exactly — `structuredobject.ToGo`
  // rejects a mismatch with `ErrKindMismatch` rather than ignoring it.
  return { apiVersion, kind, value: value as JsonObject };
}

/**
 * `namespace/name`, the ref format the domain types and the URLs both use.
 *
 * A ref with no namespace is a bare name rather than `/name`: the second form
 * sorts and reads as a resource in a namespace called the empty string, and it
 * has appeared on screen that way before.
 */
export function refToString(ref: ResourceReference | undefined): string {
  if (!ref?.name) return "";
  return ref.namespace ? `${ref.namespace}/${ref.name}` : ref.name;
}

/** RFC3339, the form every date on screen and in the domain types is parsed from. */
export function isoFrom(timestamp: Timestamp | undefined): string {
  return timestamp ? timestampDate(timestamp).toISOString() : "";
}

/**
 * A 64-bit proto field narrowed to a JavaScript number.
 *
 * `int64` and `uint64` become `bigint` in the generated code, and a `bigint` is
 * the worst kind of wrong value: it does not throw where it is produced. It
 * compares unequal to the `number` beside it, sorts and formats differently, and
 * `JSON.stringify` throws on it outright — so the failure surfaces somewhere far
 * from the field, in a table cell or a persisted draft. Narrowing at this boundary
 * keeps `bigint` out of the app entirely.
 *
 * Safe for every one of these, because all of them are ids, versions or counts
 * comfortably inside `Number.MAX_SAFE_INTEGER`. The complete inventory, so the next
 * reader can check the list against the schemas rather than trust it — regenerate it
 * with, from the repository root:
 *
 *   awk '/^message /{m=$2} /int64|uint64/{print FILENAME":"NR" "m}' \
 *     proto/kagent/api/v1alpha1/*.proto
 *
 * - `system.proto:85`   — `SubstrateActor.version`     (reached by `substrate.status`)
 * - `system.proto:96`   — `SubstrateWorker.version`    (reached by `substrate.status`)
 * - `memory.proto:38`   — `MemorySummary.access_count` (no operation id yet)
 * - `checkpoints.proto:32` — `Checkpoint.history_sequence` (no operation id yet)
 *
 * The last five have no operation behind them today. They are listed anyway: the
 * moment one gets an id, this is the helper its conversion needs, and a list that
 * only covered what happens to be wired is a list that goes stale silently.
 */
export function toNumber(value: bigint | number | undefined): number {
  return value === undefined ? 0 : Number(value);
}

/**
 * An empty proto string read as "not set".
 *
 * Proto3 cannot tell an unset string from an empty one, so a field the controller
 * left alone arrives as `""`. Several domain types make that distinction — an
 * absent `ateApiError` means "nothing went wrong", an empty one would render as a
 * warning with no text in it.
 */
export function orUndefined(value: string | undefined): string | undefined {
  return value ? value : undefined;
}
