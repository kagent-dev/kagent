import test from "node:test";
import assert from "node:assert/strict";

import { ensureDependenciesLabel } from "./release-label-backfill.mjs";

test("ensureDependenciesLabel skips creation when the label already exists", async () => {
  const requests = [];
  const originalFetch = global.fetch;

  global.fetch = async (url, options = {}) => {
    requests.push({ url, options });
    return new Response(JSON.stringify({ name: "dependencies" }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  };

  try {
    await ensureDependenciesLabel({
      token: "test-token",
      repository: "kagent-dev/kagent",
    });
  } finally {
    global.fetch = originalFetch;
  }

  assert.equal(requests.length, 1);
  assert.match(requests[0].url, /\/repos\/kagent-dev\/kagent\/labels\/dependencies$/);
  assert.equal(requests[0].options.method ?? "GET", "GET");
});

test("ensureDependenciesLabel creates the label when it is missing", async () => {
  const requests = [];
  const originalFetch = global.fetch;

  global.fetch = async (url, options = {}) => {
    requests.push({ url, options });

    if (requests.length === 1) {
      return new Response(JSON.stringify({ message: "Not Found" }), {
        status: 404,
        headers: { "content-type": "application/json" },
      });
    }

    return new Response(JSON.stringify({ name: "dependencies" }), {
      status: 201,
      headers: { "content-type": "application/json" },
    });
  };

  try {
    await ensureDependenciesLabel({
      token: "test-token",
      repository: "kagent-dev/kagent",
    });
  } finally {
    global.fetch = originalFetch;
  }

  assert.equal(requests.length, 2);
  assert.match(requests[0].url, /\/repos\/kagent-dev\/kagent\/labels\/dependencies$/);
  assert.equal(requests[0].options.method ?? "GET", "GET");
  assert.match(requests[1].url, /\/repos\/kagent-dev\/kagent\/labels$/);
  assert.equal(requests[1].options.method, "POST");
  assert.deepEqual(JSON.parse(requests[1].options.body), {
    name: "dependencies",
    color: "0366d6",
    description: "Dependency updates and version bumps",
  });
});
