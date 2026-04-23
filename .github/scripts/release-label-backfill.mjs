import { pathToFileURL } from "node:url";

const CATEGORY_LABELS = new Set([
  "enhancement",
  "bug",
  "documentation",
  "testing",
  "dependencies",
]);

const ENSURED_LABELS = {
  dependencies: {
    color: "0366d6",
    description: "Dependency updates and version bumps",
  },
};

export function classifyTitle(title) {
  const rules = [
    [/^feat(?:\([^)]*\))?:/i, "enhancement"],
    [/^fix(?:\([^)]*\))?:/i, "bug"],
    [/^docs(?:\([^)]*\))?:/i, "documentation"],
    [/^test(?:\([^)]*\))?:/i, "testing"],
    [/^chore\(deps\):/i, "dependencies"],
    [/^\[feature\]/i, "enhancement"],
    [/^\[bug\]/i, "bug"],
    [/^\[docs\]/i, "documentation"],
  ];

  for (const [pattern, label] of rules) {
    if (pattern.test(title)) {
      return label;
    }
  }

  return null;
}

export function shouldBackfill(labels, targetLabel) {
  if (!targetLabel) {
    return false;
  }

  if (labels.includes("ignore-for-release")) {
    return false;
  }

  return !labels.some((label) => CATEGORY_LABELS.has(label));
}

async function githubRequest(token, path, { method = "GET", body } = {}) {
  const response = await fetch(`https://api.github.com${path}`, {
    method,
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${token}`,
      "User-Agent": "kagent-release-label-backfill",
      "X-GitHub-Api-Version": "2022-11-28",
    },
    body: body ? JSON.stringify(body) : undefined,
  });

  if (response.status === 204) {
    return null;
  }

  const text = await response.text();
  const payload = text ? JSON.parse(text) : null;

  if (!response.ok) {
    const message = payload?.message ?? response.statusText;
    const error = new Error(`${method} ${path} failed: ${response.status} ${message}`);
    error.status = response.status;
    throw error;
  }

  return payload;
}

async function ensureLabel(token, repository, name, definition) {
  try {
    await githubRequest(token, `/repos/${repository}/labels/${encodeURIComponent(name)}`);
    console.log(`label exists: ${name}`);
    return;
  } catch (error) {
    if (error.status !== 404) {
      throw error;
    }
  }

  await githubRequest(token, `/repos/${repository}/labels`, {
    method: "POST",
    body: {
      name,
      color: definition.color,
      description: definition.description,
    },
  });
  console.log(`created label: ${name}`);
}

async function listOpenPullRequests(token, repository) {
  const pullRequests = [];

  for (let page = 1; ; page += 1) {
    const issues = await githubRequest(
      token,
      `/repos/${repository}/issues?state=open&per_page=100&page=${page}`,
    );
    const openPullRequests = issues.filter((issue) => issue.pull_request);

    pullRequests.push(...openPullRequests);

    if (issues.length < 100) {
      break;
    }
  }

  return pullRequests;
}

async function addLabel(token, repository, issueNumber, label) {
  await githubRequest(token, `/repos/${repository}/issues/${issueNumber}/labels`, {
    method: "POST",
    body: { labels: [label] },
  });
}

export async function backfillReleaseLabels({
  token = process.env.GITHUB_TOKEN,
  repository = process.env.GITHUB_REPOSITORY,
} = {}) {
  if (!token) {
    throw new Error("GITHUB_TOKEN is required");
  }
  if (!repository) {
    throw new Error("GITHUB_REPOSITORY is required");
  }

  for (const [name, definition] of Object.entries(ENSURED_LABELS)) {
    await ensureLabel(token, repository, name, definition);
  }

  const pullRequests = await listOpenPullRequests(token, repository);
  let labeledCount = 0;

  for (const pullRequest of pullRequests) {
    const labels = pullRequest.labels.map((label) => label.name);
    const targetLabel = classifyTitle(pullRequest.title);

    if (!shouldBackfill(labels, targetLabel)) {
      continue;
    }

    await addLabel(token, repository, pullRequest.number, targetLabel);
    labeledCount += 1;
    console.log(`labeled #${pullRequest.number} with ${targetLabel}`);
  }

  console.log(
    `processed ${pullRequests.length} open pull requests, added ${labeledCount} release labels`,
  );
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await backfillReleaseLabels();
}
