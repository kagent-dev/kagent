import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { issueFor, useSubmitValidation, type FieldIssue } from "./useSubmitValidation";

type Field = "name" | "namespace";
const nameMissing: FieldIssue<Field>[] = [{ field: "name", message: "A name is required." }];

describe("useSubmitValidation", () => {
  it("shows nothing before the first submit", () => {
    const { result } = renderHook(() => useSubmitValidation(nameMissing));
    expect(result.current.submitted).toBe(false);
    expect(result.current.issues).toEqual([]);
    expect(result.current.errorFor("name")).toBeUndefined();
  });

  it("blocks a submit with issues and then shows them per field", () => {
    const { result } = renderHook(() => useSubmitValidation(nameMissing));
    let allowed = true;
    act(() => {
      allowed = result.current.attempt();
    });
    expect(allowed).toBe(false);
    expect(result.current.errorFor("name")).toBe("A name is required.");
    expect(result.current.errorFor("namespace")).toBeUndefined();
  });

  it("clears a field's message as soon as the issue is fixed", () => {
    const { result, rerender } = renderHook(({ issues }) => useSubmitValidation(issues), {
      initialProps: { issues: nameMissing },
    });
    act(() => void result.current.attempt());
    rerender({ issues: [] });
    expect(result.current.errorFor("name")).toBeUndefined();
    expect(result.current.attempt()).toBe(true);
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
