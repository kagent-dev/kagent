import { pathToFileURL } from "node:url";

const DEPENDENCIES_LABEL = {
  name: "dependencies",
  color: "0366d6",
  description: "Dependency updates and version bumps",
};

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

export async function ensureDependenciesLabel({
  token = process.env.GITHUB_TOKEN,
  repository = process.env.GITHUB_REPOSITORY,
} = {}) {
  if (!token) {
    throw new Error("GITHUB_TOKEN is required");
  }
  if (!repository) {
    throw new Error("GITHUB_REPOSITORY is required");
  }

  try {
    await githubRequest(
      token,
      `/repos/${repository}/labels/${encodeURIComponent(DEPENDENCIES_LABEL.name)}`,
    );
    console.log(`label exists: ${DEPENDENCIES_LABEL.name}`);
    return;
  } catch (error) {
    if (error.status !== 404) {
      throw error;
    }
  }

  await githubRequest(token, `/repos/${repository}/labels`, {
    method: "POST",
    body: DEPENDENCIES_LABEL,
  });
  console.log(`created label: ${DEPENDENCIES_LABEL.name}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await ensureDependenciesLabel();
}
