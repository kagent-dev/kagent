import { afterEach, describe, expect, it, vi } from "vitest";
import { runtimeConfig } from "./runtimeConfig";

const IMAGE = "ghcr.io/kagent-dev/kagent/tools@sha256:" + "a".repeat(64);

afterEach(() => {
  delete window.environmentVariables;
  vi.restoreAllMocks();
});

describe("runtimeConfig builtinSkills", () => {
  it("reads the catalog the chart passes", () => {
    window.environmentVariables = {
      BUILTIN_SKILLS: JSON.stringify({
        image: IMAGE,
        skills: [{ name: "k8s-troubleshooting", description: "Diagnose unhealthy workloads.", path: "opt/kagent-tools-skills/skills/k8s-troubleshooting" }],
      }),
    };
    expect(runtimeConfig().builtinSkills).toEqual({
      image: IMAGE,
      skills: [{ name: "k8s-troubleshooting", description: "Diagnose unhealthy workloads.", path: "opt/kagent-tools-skills/skills/k8s-troubleshooting" }],
    });
  });

  it("offers nothing when the chart passes no catalog", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    expect(runtimeConfig().builtinSkills).toBeUndefined();
    window.environmentVariables = { BUILTIN_SKILLS: "" };
    expect(runtimeConfig().builtinSkills).toBeUndefined();
    expect(warn).not.toHaveBeenCalled();
  });

  it.each([
    ["not JSON", "{"],
    ["not an object", "[]"],
    ["no image", JSON.stringify({ skills: [] })],
    ["no skills list", JSON.stringify({ image: IMAGE })],
    ["a skill without a name", JSON.stringify({ image: IMAGE, skills: [{ description: "x", path: "opt/kagent-tools-skills/skills/x" }] })],
    ["a skill without a path", JSON.stringify({ image: IMAGE, skills: [{ name: "x", description: "x" }] })],
  ])("warns and offers nothing for %s", (_, value) => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    window.environmentVariables = { BUILTIN_SKILLS: value };
    expect(runtimeConfig().builtinSkills).toBeUndefined();
    expect(warn).toHaveBeenCalledOnce();
  });
});
