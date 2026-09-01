import { describe, expect, it } from "vitest";
import {
  parseResourceRef,
  sortedByFields,
  sortedByNamespaceThenName,
  sortedByRef,
} from "./order";

describe("parseResourceRef", () => {
  it("splits a namespace/name ref", () => {
    expect(parseResourceRef("kagent/default-model-config")).toEqual({
      namespace: "kagent",
      name: "default-model-config",
    });
  });

  it("treats a ref with no slash as a bare name", () => {
    // Rather than dropping it or reading the whole thing as a namespace: a row that
    // sorted by an empty name would land somewhere unrelated to its label.
    expect(parseResourceRef("lonely")).toEqual({ namespace: "", name: "lonely" });
  });

  it("keeps a slash that belongs to the name", () => {
    expect(parseResourceRef("ns/a/b")).toEqual({ namespace: "ns", name: "a/b" });
  });
});

describe("sortedByNamespaceThenName", () => {
  const identify = (value: string) => parseResourceRef(value);

  it("orders namespace descending, then name descending", () => {
    const sorted = sortedByNamespaceThenName(
      ["alpha/b", "beta/a", "alpha/a", "beta/b"],
      identify,
    );

    expect(sorted).toEqual(["beta/b", "beta/a", "alpha/b", "alpha/a"]);
  });

  it("orders embedded numbers by value, not by codepoint", () => {
    // Descending, so 10 comes before 9. Codepoint order would put "agent-9" first
    // and read as a bug to anyone scanning the column.
    expect(sortedByNamespaceThenName(["ns/agent-9", "ns/agent-10"], identify)).toEqual([
      "ns/agent-10",
      "ns/agent-9",
    ]);
  });

  it("leaves the caller's array alone", () => {
    // The argument is usually a cached array other readers share, and `sort`
    // mutates in place.
    const original = ["alpha/a", "beta/b"];
    sortedByNamespaceThenName(original, identify);
    expect(original).toEqual(["alpha/a", "beta/b"]);
  });
});

describe("the two response shapes", () => {
  it("sorts rows carrying a ref", () => {
    expect(sortedByRef([{ ref: "a/one" }, { ref: "b/two" }])).toEqual([
      { ref: "b/two" },
      { ref: "a/one" },
    ]);
  });

  it("sorts rows carrying namespace and name as fields", () => {
    expect(
      sortedByFields([
        { namespace: "a", name: "one" },
        { namespace: "b", name: "two" },
      ]),
    ).toEqual([
      { namespace: "b", name: "two" },
      { namespace: "a", name: "one" },
    ]);
  });

  it("sorts rows whose namespace is absent", () => {
    // An undefined namespace is a real case — the agents list carries one — and it
    // has to sort somewhere predictable rather than throwing.
    expect(sortedByFields([{ name: "b" }, { name: "a" }])).toEqual([
      { name: "b" },
      { name: "a" },
    ]);
  });
});
