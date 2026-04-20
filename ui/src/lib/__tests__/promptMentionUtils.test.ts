import { describe, it, expect } from "@jest/globals";
import {
  getActiveMention,
  matchesMentionQuery,
  scoreMentionMatch,
  TEMPLATE_VARIABLES,
  type MentionItem,
} from "@/lib/promptMentionUtils";

describe("getActiveMention", () => {
  it("returns null when cursor is negative", () => {
    expect(getActiveMention("hello", -1)).toBeNull();
  });

  it("returns null when @ is not at word/line boundary", () => {
    expect(getActiveMention("foo@bar", 7)).toBeNull();
  });

  it("returns active mention after space", () => {
    expect(getActiveMention("hello @foo", 10)).toEqual({ start: 6, query: "foo" });
  });

  it("returns active mention at line start", () => {
    expect(getActiveMention("@abc", 4)).toEqual({ start: 0, query: "abc" });
  });

  it("returns null when query contains space", () => {
    expect(getActiveMention("hello @a b", 10)).toBeNull();
  });
});

describe("matchesMentionQuery", () => {
  const include: MentionItem = {
    kind: "include",
    configMapName: "my-lib",
    includeSourceId: "my-lib",
    key: "system",
    label: "my-lib / system",
  };

  it("matches all when query empty", () => {
    expect(matchesMentionQuery(include, "")).toBe(true);
    const variable: MentionItem = {
      kind: "variable",
      insert: "{{.AgentName}}",
      label: "{{.AgentName}}",
      hint: "metadata.name",
    };
    expect(matchesMentionQuery(variable, "   ")).toBe(true);
  });

  it("matches include by name/key path", () => {
    expect(matchesMentionQuery(include, "my-lib/system")).toBe(true);
    expect(matchesMentionQuery(include, "system")).toBe(true);
  });

  it("matches include by alias path when includeSourceId differs from ConfigMap name", () => {
    const aliased: MentionItem = {
      kind: "include",
      configMapName: "kagent-builtin-prompts",
      includeSourceId: "builtin",
      key: "safety",
      label: "builtin / safety (kagent-builtin-prompts)",
    };
    expect(matchesMentionQuery(aliased, "builtin/safety")).toBe(true);
    expect(matchesMentionQuery(aliased, "builtin")).toBe(true);
  });

  it("matches variable by field name", () => {
    const variable: MentionItem = {
      kind: "variable",
      insert: "{{.AgentName}}",
      label: "{{.AgentName}}",
      hint: "metadata.name",
    };
    expect(matchesMentionQuery(variable, "AgentName")).toBe(true);
    expect(matchesMentionQuery(variable, "agent")).toBe(true);
  });

  it("TEMPLATE_VARIABLES entries match AgentName-style query", () => {
    const item: MentionItem = {
      kind: "variable",
      insert: "{{.AgentName}}",
      label: "{{.AgentName}}",
      hint: "metadata.name",
    };
    expect(matchesMentionQuery(item, "AgentName")).toBe(true);
    expect(TEMPLATE_VARIABLES.length).toBeGreaterThanOrEqual(5);
  });
});

describe("scoreMentionMatch", () => {
  it("returns 0 for empty query", () => {
    const include: MentionItem = {
      kind: "include",
      configMapName: "lib",
      includeSourceId: "lib",
      key: "k",
      label: "lib / k",
    };
    expect(scoreMentionMatch(include, "")).toBe(0);
  });

  it("ranks prefix matches higher for includes", () => {
    const a: MentionItem = {
      kind: "include",
      configMapName: "team",
      includeSourceId: "team",
      key: "prompts",
      label: "team / prompts",
    };
    const b: MentionItem = {
      kind: "include",
      configMapName: "other",
      includeSourceId: "other",
      key: "x",
      label: "other / x",
    };
    const q = "prompts";
    expect(scoreMentionMatch(a, q)).toBeGreaterThan(scoreMentionMatch(b, q));
  });
});
