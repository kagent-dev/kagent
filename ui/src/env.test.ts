import { afterEach, describe, expect, it } from "vitest";
import { ENV_DEFAULTS, env, envFlag, envIsSet, readEnv } from "./env";

afterEach(() => {
  delete window.environmentVariables;
});

describe("env", () => {
  it("falls back to the default when nothing was injected", () => {
    expect(env("API_BASE_URL")).toBe(ENV_DEFAULTS.API_BASE_URL);
  });

  it("returns what was injected", () => {
    window.environmentVariables = { API_BASE_URL: "https://api.example.test" };
    expect(env("API_BASE_URL")).toBe("https://api.example.test");
  });

  // An unset chart value renders as an empty string rather than an absent key.
  it("treats an empty value as absent", () => {
    window.environmentVariables = { API_BASE_URL: "" };
    expect(env("API_BASE_URL")).toBe(ENV_DEFAULTS.API_BASE_URL);
  });

  // A document written by an older image will not have every key the current
  // bundle reads, and the keys it does have still have to work.
  it("defaults the keys a partial document omits", () => {
    window.environmentVariables = { API_BASE_URL: "/other" };
    expect(env("API_BASE_URL")).toBe("/other");
    expect(env("SSO_REDIRECT_PATH")).toBe(ENV_DEFAULTS.SSO_REDIRECT_PATH);
  });
});

describe("envFlag", () => {
  it("reads a value passed through a shell and a chart as on", () => {
    window.environmentVariables = { ENABLE_MOCK_UI: "TRUE\n" };
    expect(envFlag("ENABLE_MOCK_UI")).toBe(true);
  });

  it("reads anything else as off", () => {
    window.environmentVariables = { ENABLE_MOCK_UI: "false" };
    expect(envFlag("ENABLE_MOCK_UI")).toBe(false);
  });
});

describe("envIsSet", () => {
  // The distinction the mock/live switch turns on: explicitly off is an
  // instruction, unset means "decide for me".
  it("separates explicitly off from unset", () => {
    window.environmentVariables = { ENABLE_MOCK_UI: "false" };
    expect(envIsSet("ENABLE_MOCK_UI")).toBe(true);

    window.environmentVariables = {};
    expect(envIsSet("ENABLE_MOCK_UI")).toBe(false);
  });
});

describe("readEnv", () => {
  it("reads a key the application knows nothing about", () => {
    window.environmentVariables = { LOCAL_CLUSTER_NAME: "prod-us-east" };
    expect(readEnv("LOCAL_CLUSTER_NAME", "mgmt-cluster")).toBe("prod-us-east");
  });

  it("uses the caller's fallback when it is absent", () => {
    expect(readEnv("LOCAL_CLUSTER_NAME", "mgmt-cluster")).toBe("mgmt-cluster");
  });
});
