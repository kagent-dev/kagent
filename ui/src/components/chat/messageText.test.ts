import { describe, expect, it } from "vitest";
import type { ChatMessage, ChatPart } from "@/api";
import { userTurnCount } from "./messageText";

const message = (role: ChatMessage["role"], parts: ChatPart[]): ChatMessage => ({
  id: `${role}-${parts.length}`,
  role,
  parts,
  createdAt: "2025-01-01T00:00:00Z",
});

const typed = (text: string) => message("user", [{ kind: "text", text }]);

describe("userTurnCount", () => {
  it("counts user messages that carry typed text", () => {
    expect(
      userTurnCount([typed("one"), message("agent", [{ kind: "text", text: "reply" }]), typed("two")]),
    ).toBe(2);
  });

  it("skips a tool-approval answer", () => {
    const approval = message("user", [
      {
        kind: "tool_approval",
        approval: { tools: [], decisions: [], askedBy: "agent" },
      } as ChatPart,
    ]);
    expect(userTurnCount([typed("one"), approval])).toBe(1);
  });

  it("skips an ask_user answer", () => {
    const answer = message("user", [
      {
        kind: "ask_user",
        interaction: { questions: [], answers: [], askedBy: "agent" },
      } as ChatPart,
    ]);
    expect(userTurnCount([typed("one"), answer])).toBe(1);
  });

  it("skips a message that only sends files", () => {
    const file = message("user", [{ kind: "file", name: "a.png", mediaType: "image/png" }]);
    expect(userTurnCount([typed("one"), file])).toBe(1);
  });
});
