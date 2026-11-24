import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TYPING_PAUSE_MS, issueFor, useSubmitValidation, type FieldIssue } from "./useSubmitValidation";

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

describe("useSubmitValidation while typing", () => {
  afterEach(() => vi.useRealTimers());

  const badName: FieldIssue<Field>[] = [{ field: "name", message: "Use lowercase letters." }];

  it("holds a new message until typing pauses", () => {
    vi.useFakeTimers();
    const { result, rerender } = renderHook(({ issues }) => useSubmitValidation(issues), {
      initialProps: { issues: [] as FieldIssue<Field>[] },
    });
    act(() => result.current.field("name").onType?.("A"));
    rerender({ issues: badName });
    expect(result.current.errorFor("name")).toBeUndefined();

    act(() => vi.advanceTimersByTime(TYPING_PAUSE_MS - 1));
    act(() => result.current.field("name").onType?.("AB"));
    act(() => vi.advanceTimersByTime(TYPING_PAUSE_MS - 1));
    expect(result.current.errorFor("name")).toBeUndefined();

    act(() => vi.advanceTimersByTime(1));
    expect(result.current.errorFor("name")).toBe("Use lowercase letters.");
  });

  it("keeps a message already on screen, and drops one the typing fixed", () => {
    vi.useFakeTimers();
    const issues: FieldIssue<Field>[] = [...badName, { field: "namespace", message: "Pick a namespace." }];
    const { result, rerender } = renderHook(({ issues }) => useSubmitValidation(issues), { initialProps: { issues } });
    act(() => {
      result.current.touch("name");
      result.current.touch("namespace");
    });
    act(() => result.current.field("name").onType?.("abc"));
    rerender({ issues: issues.slice(1) });
    expect(result.current.errorFor("name")).toBeUndefined();
    expect(result.current.errorFor("namespace")).toBe("Pick a namespace.");
  });

  it("shows a held message at once when the field is left or the form submitted", () => {
    vi.useFakeTimers();
    const { result, rerender } = renderHook(({ issues }) => useSubmitValidation(issues), {
      initialProps: { issues: [] as FieldIssue<Field>[] },
    });
    act(() => result.current.field("name").onType?.("A"));
    rerender({ issues: badName });
    act(() => result.current.field("name").onTouch());
    expect(result.current.errorFor("name")).toBe("Use lowercase letters.");

    act(() => result.current.reset());
    act(() => result.current.field("name").onType?.("A"));
    expect(result.current.errorFor("name")).toBeUndefined();
    act(() => {
      result.current.attempt();
    });
    expect(result.current.errorFor("name")).toBe("Use lowercase letters.");
  });

  it("does not touch a field when what was typed is blank", () => {
    vi.useFakeTimers();
    const { result } = renderHook(() => useSubmitValidation(nameMissing));
    act(() => result.current.field("name").onType?.("  "));
    act(() => vi.advanceTimersByTime(TYPING_PAUSE_MS));
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
