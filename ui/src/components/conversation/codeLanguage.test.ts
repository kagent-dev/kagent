import { resolveCodeLanguage } from "./codeLanguage";

describe("resolveCodeLanguage", () => {
  it.each([
    ["yaml", "a: 1", "yaml", "yaml"],
    ["yml", "a: 1", "yaml", "yml"],
    ["bash", "ls", "shell", "bash"],
    ["json", "{}", "javascript", "json"],
    ["rust", "fn main() {}", "rust", "rust"],
  ])("uses the fence name %s", (fence, code, id, label) => {
    expect(resolveCodeLanguage(fence, code)).toEqual({ id, label });
  });

  it.each([
    ["apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x", "yaml"],
    ["kubectl rollout restart deployment/x -n prod", "shell"],
    ["$ helm upgrade kagent ./chart", "shell"],
    ['{ "pods": 3, "ready": true }', "json"],
    ["package main\n\nfunc main() {}", "go"],
    ["def handler(event):\n    return event", "python"],
    ["FROM node:22\nRUN yarn", "dockerfile"],
    ["SELECT id FROM sessions WHERE agent = $1", "sql"],
    ["const pods: number = 3;", "typescript"],
    ["just some words", "text"],
  ])("detects an unlabelled block: %s", (code, label) => {
    expect(resolveCodeLanguage(undefined, code).label).toBe(label);
  });
});
