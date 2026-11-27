import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { issueFor, useSubmitValidation, type FieldIssue } from "./useSubmitValidation";

type Field = "name" | "namespace";
const nameMissing: FieldIssue<Field>[] = [{ field: "name", message: "A name is required." }];

describe("useSubmitValidation", () => {
  it("shows nothing on untouched fields but reports the draft invalid", () => {
    const { result } = renderHook(() => useSubmitValidation(nameMissing));
    expect(result.current.valid).toBe(false);
    expect(result.current.issues).toEqual([]);
    expect(result.current.errorFor("name")).toBeUndefined();
    expect(result.current.field("name").error).toBeUndefined();
  });

  it("shows a touched field's issue and leaves the others hidden", () => {
    const issues: FieldIssue<Field>[] = [...nameMissing, { field: "namespace", message: "Bad namespace." }];
    const { result } = renderHook(() => useSubmitValidation(issues));
    act(() => result.current.field("name").onTouch());
    expect(result.current.field("name").error).toBe("A name is required.");
    expect(result.current.errorFor("namespace")).toBeUndefined();
  });

  it("blocks a submit with issues and then shows every issue", () => {
    const { result } = renderHook(() => useSubmitValidation(nameMissing));
    let allowed = true;
    act(() => {
      allowed = result.current.attempt();
    });
    expect(allowed).toBe(false);
    expect(result.current.errorFor("name")).toBe("A name is required.");
    expect(result.current.errorFor("namespace")).toBeUndefined();
  });

  it("clears a field's message as soon as the issue is fixed, and reset hides it again", () => {
    const { result, rerender } = renderHook(({ issues }) => useSubmitValidation(issues), {
      initialProps: { issues: nameMissing },
    });
    act(() => result.current.touch("name"));
    rerender({ issues: [] });
    expect(result.current.valid).toBe(true);
    expect(result.current.errorFor("name")).toBeUndefined();
    expect(result.current.attempt()).toBe(true);

    rerender({ issues: nameMissing });
    expect(result.current.errorFor("name")).toBe("A name is required.");
    act(() => result.current.reset());
    expect(result.current.errorFor("name")).toBeUndefined();
  });
});

describe("issueFor", () => {
  it("returns the first message for a field", () => {
    const issues: FieldIssue<Field>[] = [
      { field: "name", message: "first" },
      { field: "name", message: "second" },
    ];
    expect(issueFor(issues, "name")).toBe("first");
    expect(issueFor(issues, "namespace")).toBeUndefined();
  });
});
