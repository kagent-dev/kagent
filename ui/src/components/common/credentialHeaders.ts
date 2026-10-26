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
    const source = header.valueFrom;
    if ([source.secretKeyRef, source.tokenPassthrough, source.actorToken].filter(Boolean).length !== 1) issues.push("Each credential header needs exactly one credential source.");
    if (header.valueFrom.secretKeyRef && (!header.valueFrom.secretKeyRef.name.trim() || !header.valueFrom.secretKeyRef.key.trim())) issues.push("Each credential Secret needs a name and key.");
    if (source.actorToken) {
      const { audiences, expirationSeconds } = source.actorToken;
      if (audiences.length < 1 || audiences.length > 16) issues.push("Actor tokens need between 1 and 16 audiences.");
      if (audiences.some((audience) => audience.length === 0 || Array.from(audience).length > 512)) issues.push("Each actor token audience must contain between 1 and 512 characters.");
      if (new Set(audiences).size !== audiences.length) issues.push("Actor token audiences must be unique.");
      if (audiences.includes("kagent-runtime")) issues.push("Actor token audiences cannot include kagent-runtime.");
      if (expirationSeconds !== undefined && (!Number.isInteger(expirationSeconds) || expirationSeconds < 300 || expirationSeconds > 3600)) issues.push("Actor token lifetime must be a whole number between 300 and 3600 seconds.");
    }
  }
  return issues;
}
