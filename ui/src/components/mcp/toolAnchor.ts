/** The anchor id of one tool on a server's details page. */
export function toolAnchor(name: string): string {
  return `tool-${name}`;
}

/** Decodes a URL hash, returning it as-is when it is not valid percent-encoding. */
export function safeDecode(hash: string): string {
  try {
    return decodeURIComponent(hash);
  } catch {
    return hash;
  }
}
