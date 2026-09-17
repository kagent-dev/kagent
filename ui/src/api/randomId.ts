/**
 * A random UUID for a client-generated request, message or fixture id.
 *
 * Not `crypto.randomUUID`: that exists only in a secure context, so it is
 * undefined whenever the app is served over plain http from anything but
 * localhost, and the call throws. These ids identify a request, never
 * authenticate one, so `Math.random` is enough — a share token or anything
 * else a caller must not be able to guess does not belong here.
 */
export function randomId(): string {
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === "x" ? r : (r & 0x3) | 0x8).toString(16);
  });
}
