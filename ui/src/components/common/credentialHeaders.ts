import type { Header } from "@/api";

export function credentialHeaderIssues(headers: Header[], ordinaryNames: string[] = []): string[] {
  const issues: string[] = [];
  if (headers.length > 16) issues.push("At most 16 credential headers are allowed.");
  const names = new Set(ordinaryNames.map((name) => name.toLowerCase()));
  const protectedNames = new Set(["host", "connection", "content-length", "transfer-encoding", "te", "trailer", "upgrade", "keep-alive", "proxy-authorization", "proxy-authenticate", "cookie"]);
  for (const header of headers) {
    const name = header.name.toLowerCase();
    if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(header.name) || header.name.length > 256) issues.push("Credential headers need a valid HTTP header name.");
    if (protectedNames.has(name)) issues.push(`Header ${header.name} cannot carry a credential.`);
    if (names.has(name)) issues.push(`Header ${header.name} is configured more than once.`);
    names.add(name);
    if (header.valueFrom.secretKeyRef && (!header.valueFrom.secretKeyRef.name.trim() || !header.valueFrom.secretKeyRef.key.trim())) issues.push("Each credential Secret needs a name and key.");
  }
  return issues;
}
