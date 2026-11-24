import { describe, expect, it } from "vitest";
import { readToolSearch, toolMatches, writeToolSearch } from "./toolSearch";

const pods = { name: "k8s_get_pods", description: "Lists pods." };
const podLogs = { name: "k8s_get_pods_logs", description: "Reads a pod's logs." };

describe("toolMatches", () => {
  it("matches everything when there are no terms", () => {
    expect(toolMatches(pods, { terms: [], exact: false })).toBe(true);
    expect(toolMatches(pods, { terms: [" "], exact: true })).toBe(true);
  });

  it("matches part of a name or description, ignoring case", () => {
    expect(toolMatches(pods, { terms: ["GET_POD"], exact: false })).toBe(true);
    expect(toolMatches(pods, { terms: ["lists"], exact: false })).toBe(true);
    expect(toolMatches(pods, { terms: ["helm"], exact: false })).toBe(false);
  });

  it("matches whole names only when exact, so a longer name with the same prefix is left out", () => {
    const search = { terms: ["k8s_get_pods"], exact: true };
    expect(toolMatches(pods, search)).toBe(true);
    expect(toolMatches(podLogs, search)).toBe(false);
    expect(toolMatches(pods, { terms: ["Lists pods."], exact: true })).toBe(false);
  });

  it("matches any one of several terms", () => {
    const search = { terms: ["helm_list", "k8s_get_pods_logs"], exact: true };
    expect(toolMatches(pods, search)).toBe(false);
    expect(toolMatches(podLogs, search)).toBe(true);
  });
});

describe("readToolSearch and writeToolSearch", () => {
  it("round-trip the terms and the exact flag, keeping other parameters", () => {
    const written = writeToolSearch(new URLSearchParams("kind=RemoteMCPServer&tab=usage"), {
      terms: ["a", "b"],
      exact: true,
    });
    expect(written.toString()).toBe("kind=RemoteMCPServer&tab=usage&q=a&q=b&exact=1");
    expect(readToolSearch(written)).toEqual({ terms: ["a", "b"], exact: true });
  });

  it("drops blank and repeated terms", () => {
    expect(readToolSearch(new URLSearchParams("q=a&q=&q=a&q=%20b"))).toEqual({ terms: ["a", "b"], exact: false });
  });
});
