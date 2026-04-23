import test from "node:test";
import assert from "node:assert/strict";

import { classifyTitle, shouldBackfill } from "./release-label-backfill.mjs";

test("classifyTitle maps supported conventional prefixes", () => {
  assert.equal(classifyTitle("feat(ui): add a button"), "enhancement");
  assert.equal(classifyTitle("fix: handle empty input"), "bug");
  assert.equal(classifyTitle("docs(api): update README"), "documentation");
  assert.equal(classifyTitle("test: cover label sweeper"), "testing");
  assert.equal(classifyTitle("chore(deps): bump lucide-react"), "dependencies");
});

test("classifyTitle maps supported legacy bracket prefixes", () => {
  assert.equal(classifyTitle("[FEATURE] add support for x"), "enhancement");
  assert.equal(classifyTitle("[BUG] fix exporter config"), "bug");
  assert.equal(classifyTitle("[DOCS] correct the install guide"), "documentation");
});

test("classifyTitle ignores ambiguous titles", () => {
  assert.equal(classifyTitle("Add askUser config"), null);
  assert.equal(classifyTitle("Improve session resilience"), null);
  assert.equal(classifyTitle("cli: add --provider flag"), null);
});

test("shouldBackfill skips unlabeled categories only when safe", () => {
  assert.equal(shouldBackfill([], "enhancement"), true);
  assert.equal(shouldBackfill(["stale"], "bug"), true);
  assert.equal(shouldBackfill(["ignore-for-release"], "dependencies"), false);
  assert.equal(shouldBackfill(["bug"], "bug"), false);
  assert.equal(shouldBackfill([], null), false);
});
