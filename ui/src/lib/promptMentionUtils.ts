/**
 * Pure helpers for the @-mention picker in agent instructions (prompt libraries + template variables).
 */

export type MentionItem =
  | {
      kind: "include";
      /** Kubernetes ConfigMap name (for grouping and registration). */
      configMapName: string;
      /** First segment in `{{include "…/key"}}` — alias when set on the agent, else same as configMapName. */
      includeSourceId: string;
      key: string;
      label: string;
    }
  | { kind: "variable"; insert: string; label: string; hint: string };

/** Available in systemMessage when prompt sources / template processing applies (see docs/architecture/prompt-templates.md). */
export const TEMPLATE_VARIABLES: ReadonlyArray<{ example: string; hint: string }> = [
  { example: "{{.AgentName}}", hint: "metadata.name" },
  { example: "{{.AgentNamespace}}", hint: "metadata.namespace" },
  { example: "{{.Description}}", hint: "spec.description" },
  { example: "{{.ToolNames}}", hint: "MCP tools (range in templates)" },
  { example: "{{.SkillNames}}", hint: "skills.refs / gitRefs" },
];

/** @ at line/word boundary: insert include and register a prompt library source. */
export function getActiveMention(text: string, cursor: number): { start: number; query: string } | null {
  if (cursor < 0) {
    return null;
  }
  const before = text.slice(0, cursor);
  const at = before.lastIndexOf("@");
  if (at < 0) {
    return null;
  }
  const prev = at > 0 ? before[at - 1] : "\n";
  if (!/[\s\n]/.test(prev)) {
    return null;
  }
  const after = before.slice(at + 1);
  if (/[\s\n]/.test(after)) {
    return null;
  }
  return { start: at, query: after };
}

export function variableFieldName(insert: string): string {
  const m = /^\{\{\.([^}]+)\}\}$/.exec(insert.trim());
  return (m?.[1] ?? insert).toLowerCase();
}

/** Match library fragment keys and template variables against the typed @query (supports `name/key`, tokens, substring). */
export function matchesMentionQuery(it: MentionItem, rawQuery: string): boolean {
  const q = rawQuery.trim().toLowerCase();
  if (!q) {
    return true;
  }

  if (it.kind === "variable") {
    const ex = it.insert.toLowerCase();
    const hint = it.hint.toLowerCase();
    const field = variableFieldName(it.insert);
    const blob = `${ex} ${hint} ${field}`;

    const slash = q.indexOf("/");
    if (slash >= 0) {
      const a = q.slice(0, slash).trim();
      const b = q.slice(slash + 1).trim();
      return (!a || blob.includes(a)) && (!b || blob.includes(b));
    }

    const tokens = q.split(/\s+/).filter(Boolean);
    if (tokens.length > 1) {
      return tokens.every((t) => blob.includes(t));
    }

    return blob.includes(q);
  }

  const cmLower = it.configMapName.toLowerCase();
  const idLower = it.includeSourceId.toLowerCase();
  const keyLower = it.key.toLowerCase();
  const labelLower = it.label.toLowerCase();
  const combined = `${cmLower}/${keyLower}`;
  const idCombined = `${idLower}/${keyLower}`;

  const slash = q.indexOf("/");
  if (slash >= 0) {
    const cmPart = q.slice(0, slash).trim();
    const keyPart = q.slice(slash + 1).trim();
    const cmOk = !cmPart || cmLower.includes(cmPart) || idLower.includes(cmPart);
    const keyOk = !keyPart || keyLower.includes(keyPart);
    return cmOk && keyOk;
  }

  const tokens = q.split(/\s+/).filter(Boolean);
  if (tokens.length > 1) {
    return tokens.every(
      (t) =>
        combined.includes(t) ||
        idCombined.includes(t) ||
        labelLower.includes(t) ||
        cmLower.includes(t) ||
        idLower.includes(t) ||
        keyLower.includes(t),
    );
  }

  return (
    combined.includes(q) ||
    idCombined.includes(q) ||
    labelLower.includes(q) ||
    cmLower.includes(q) ||
    idLower.includes(q) ||
    keyLower.includes(q)
  );
}

export function scoreMentionMatch(it: MentionItem, rawQuery: string): number {
  const q = rawQuery.trim().toLowerCase();
  if (!q) {
    return 0;
  }

  if (it.kind === "variable") {
    const ex = it.insert.toLowerCase();
    const hint = it.hint.toLowerCase();
    const field = variableFieldName(it.insert);
    let s = 0;
    if (ex.startsWith(q) || field.startsWith(q)) {
      s += 100;
    }
    if (hint.includes(q)) {
      s += 40;
    }
    if (ex.includes(q)) {
      s += 30;
    }
    if (field.includes(q)) {
      s += 25;
    }
    return s;
  }

  const cm = it.configMapName.toLowerCase();
  const id = it.includeSourceId.toLowerCase();
  const key = it.key.toLowerCase();
  let s = 0;
  const slash = q.indexOf("/");
  if (slash >= 0) {
    const cmPart = q.slice(0, slash).trim();
    const keyPart = q.slice(slash + 1).trim();
    if (cmPart && id.startsWith(cmPart)) {
      s += 90;
    } else if (cmPart && cm.startsWith(cmPart)) {
      s += 80;
    }
    if (keyPart && key.startsWith(keyPart)) {
      s += 120;
    }
  }
  if (key.startsWith(q)) {
    s += 100;
  }
  if (id.startsWith(q)) {
    s += 65;
  }
  if (cm.startsWith(q)) {
    s += 60;
  }
  if (key.includes(q)) {
    s += 25;
  }
  if (id.includes(q)) {
    s += 20;
  }
  if (cm.includes(q)) {
    s += 15;
  }
  return s;
}
