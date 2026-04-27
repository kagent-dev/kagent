import type { GitRepo } from "@/types";
import { isResourceNameValid } from "@/lib/utils";

/** Matches CRD max items for `skills.refs` and `skills.gitRefs`. */
export const MAX_SKILLS_PER_SOURCE = 20;

/** Form row for `spec.skills.gitRefs` (GitRepo). */
export type GitSkillFormRow = {
  url: string;
  ref: string;
  path: string;
  name: string;
};

export function newEmptyGitSkillRow(): GitSkillFormRow {
  return { url: "", ref: "", path: "", name: "" };
}

/** Last non-empty segment of a slash-separated path (no leading/trailing slashes required). */
function lastPathSegment(path: string): string {
  const parts = path.split("/").filter(Boolean);
  return parts.length > 0 ? (parts[parts.length - 1] ?? "") : "";
}

/**
 * Default folder name under /skills: last path segment in `pathInRepo` if set, else
 * the repo name from the clone URL. Matches the controller’s gitSkillName when
 * `name` is omitted in the API.
 */
export function defaultGitSkillFolderName(url: string, pathInRepo: string): string {
  const p = pathInRepo.trim().replace(/^\/+/, "").replace(/\/+$/g, "");
  if (p) {
    return lastPathSegment(p);
  }
  const u = url.trim();
  if (!u) {
    return "";
  }
  if (/^(?:https?|git|git\+ssh|ssh):\/\//i.test(u)) {
    try {
      const parsed = new URL(u);
      const seg = parsed.pathname.replace(/\/+$/, "").replace(/\.git$/i, "");
      if (seg) {
        return lastPathSegment(seg);
      }
    } catch {
      /* fall through */
    }
  }
  const scp = /^git@[^:]+:(.+)$/.exec(u);
  if (scp) {
    const tail = scp[1].replace(/\.git$/i, "");
    if (tail) {
      return lastPathSegment(tail);
    }
  }
  const noGit = u.replace(/\.git$/i, "");
  const i = noGit.lastIndexOf("/");
  if (i >= 0) {
    return noGit.slice(i + 1);
  }
  return noGit;
}

/**
 * When URL or path in repo changes, update the suggested "name" only if the user
 * has not set a custom value (empty or still equal to the previous default).
 */
export function applyGitSkillUrlPathChange(
  row: GitSkillFormRow,
  change: { url?: string; path?: string },
): GitSkillFormRow {
  const nextUrl = change.url !== undefined ? change.url : row.url;
  const nextPath = change.path !== undefined ? change.path : row.path;
  const oldDerived = defaultGitSkillFolderName(row.url, row.path);
  const newDerived = defaultGitSkillFolderName(nextUrl, nextPath);
  const t = row.name.trim();
  const name = t === "" || t === oldDerived ? newDerived : row.name;
  return { ...row, url: nextUrl, path: nextPath, name };
}

export function gitRepoToFormRow(g: GitRepo): GitSkillFormRow {
  const url = g.url || "";
  const path = g.path || "";
  const d = defaultGitSkillFolderName(url, path);
  return {
    url,
    ref: g.ref ?? "",
    path,
    name: (g.name && g.name.trim()) || d,
  };
}

/**
 * One form row → API `GitRepo` (or `null` if URL is blank — empty rows are dropped).
 * Applies the same `name` defaulting as the server (via `defaultGitSkillFolderName`).
 */
export function formRowToGitRepo(row: GitSkillFormRow): GitRepo | null {
  const url = row.url.trim();
  if (!url) {
    return null;
  }
  const o: GitRepo = { url };
  const r = row.ref.trim();
  if (r) o.ref = r;
  const p = row.path.trim();
  if (p) o.path = p;
  const n = row.name.trim() || defaultGitSkillFolderName(url, p);
  if (n) o.name = n;
  return o;
}

/** Non-empty GitRepo entries from form rows (empty URL rows are dropped). */
export function formRowsToGitRepos(rows: GitSkillFormRow[]): GitRepo[] {
  return rows
    .map((row) => formRowToGitRepo(row))
    .filter((g): g is GitRepo => g !== null);
}

const GIT_REMOTE_RE = /^(https?:\/\/|git@|git:\/\/|ssh:\/\/)/i;

export function isPlausibleGitRemoteUrl(url: string): boolean {
  return GIT_REMOTE_RE.test(url.trim());
}

/** Basic check for OCI skill image reference format. */
export function isValidSkillContainerImage(image: string): boolean {
  if (!image.trim()) return false;
  const imageRegex =
    /^(?:(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}(?::\d+)?\/)?[A-Za-z0-9][A-Za-z0-9._-]*(?:\/[A-Za-z0-9][A-Za-z0-9._-]*)*(?::[A-Za-z0-9][A-Za-z0-9._-]*)?(?:@sha256:[a-f0-9]{64})?$/i;
  return imageRegex.test(image.trim());
}

/**
 * Stable key for “same Git skill source” (URL + ref + in-repo path). Use for
 * de-duplication and for comparing a form row to a resolved `GitRepo`.
 */
export function gitSkillSourceDedupeKey(url: string, ref: string, pathInRepo: string): string {
  return `${url.trim().toLowerCase()}|${ref.trim().toLowerCase()}|${pathInRepo.trim().toLowerCase()}`;
}

export function gitSkillDedupeKeyFromRepo(g: GitRepo): string {
  return gitSkillSourceDedupeKey(g.url, g.ref || "", g.path || "");
}

export function gitSkillDedupeKeyFromFormRow(row: GitSkillFormRow): string {
  return gitSkillSourceDedupeKey(row.url, row.ref, row.path);
}

export function gitSkillRowUrlIssues(row: GitSkillFormRow): {
  hasExtraWithoutUrl: boolean;
  urlInvalid: boolean;
} {
  const urlTrim = row.url.trim();
  const hasExtraWithoutUrl =
    !urlTrim && !!(row.ref.trim() || row.path.trim() || row.name.trim());
  const urlInvalid = urlTrim.length > 0 && !isPlausibleGitRemoteUrl(urlTrim);
  return { hasExtraWithoutUrl, urlInvalid };
}

function hasDuplicateStrings(keys: string[]): boolean {
  return keys.some((k, i) => keys.indexOf(k) !== i);
}

/** True when this row’s URL+ref+path appears more than once among resolved Git repos. */
export function isDuplicateGitSkillFormRow(
  row: GitSkillFormRow,
  resolvedGitRepos: GitRepo[],
): boolean {
  if (!row.url.trim()) {
    return false;
  }
  const rowKey = gitSkillDedupeKeyFromFormRow(row);
  const count = resolvedGitRepos.filter(
    (g) => gitSkillDedupeKeyFromRepo(g) === rowKey,
  ).length;
  return count > 1;
}

export function isDuplicateOciSkillRef(ref: string, allRefs: string[]): boolean {
  const t = ref.trim();
  if (!t) {
    return false;
  }
  return allRefs.filter((r) => r.trim().toLowerCase() === t.toLowerCase()).length > 1;
}

export type DeclarativeAgentSkillsFormInput = {
  skillRefs: string[];
  skillGitRepos: GitSkillFormRow[];
  skillsGitAuthSecretName: string;
};

/**
 * Validates OCI refs, Git repos, and optional git auth secret for the declarative agent form.
 * Returns the first error message, or `undefined` if valid.
 */
export function validateDeclarativeAgentSkills(
  input: DeclarativeAgentSkillsFormInput,
): string | undefined {
  const nonEmptyRefs = (input.skillRefs || []).filter((ref) => ref.trim());
  const gitRepos = formRowsToGitRepos(input.skillGitRepos || []);

  if (nonEmptyRefs.length > 0) {
    if (nonEmptyRefs.length > MAX_SKILLS_PER_SOURCE) {
      return `At most ${MAX_SKILLS_PER_SOURCE} container image skills are allowed`;
    }
    const invalidRefs = nonEmptyRefs.filter((ref) => !isValidSkillContainerImage(ref));
    if (invalidRefs.length > 0) {
      return `Invalid container image format: ${invalidRefs[0]}`;
    }
    const trimmedLower = nonEmptyRefs.map((ref) => ref.trim().toLowerCase());
    if (hasDuplicateStrings(trimmedLower)) {
      const dupIdx = trimmedLower.findIndex(
        (ref, idx) => trimmedLower.indexOf(ref) !== idx,
      );
      return `Duplicate skill image: ${nonEmptyRefs[dupIdx]}`;
    }
  }

  const partialGit = (input.skillGitRepos || []).some(
    (row) =>
      !row.url.trim() && !!(row.ref.trim() || row.path.trim() || row.name.trim()),
  );
  if (partialGit) {
    return "Git skill rows that set ref, path, or name need a repository URL";
  }
  if (gitRepos.length > MAX_SKILLS_PER_SOURCE) {
    return `At most ${MAX_SKILLS_PER_SOURCE} Git skill sources are allowed`;
  }
  const badUrl = gitRepos.find((g) => !isPlausibleGitRemoteUrl(g.url));
  if (badUrl) {
    return `Invalid Git URL (use https://, http://, git@, or ssh://): ${badUrl.url}`;
  }
  if (hasDuplicateStrings(gitRepos.map(gitSkillDedupeKeyFromRepo))) {
    return "Duplicate Git skill (same URL, ref, and path)";
  }

  const sec = input.skillsGitAuthSecretName?.trim();
  if (sec && gitRepos.length === 0) {
    return "Add at least one Git repository to use a credentials secret, or clear the secret name";
  }
  if (sec && !isResourceNameValid(sec)) {
    return "Git auth secret name must be a valid Kubernetes resource name";
  }

  return undefined;
}
