/**
 * A random UUID for a client-generated request, message or fixture id.
 *
 * Not `crypto.randomUUID`: that exists only in a secure context, so it is
 * undefined whenever the app is served over plain http from anything but
 * localhost, and the call throws. `getRandomValues` carries no such condition.
 */
export function randomId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
