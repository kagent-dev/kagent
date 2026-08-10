import type { EnvVar, GitRepo, S3SkillRef } from "@/types";
import { isResourceNameValid } from "@/lib/utils";

/** Matches CRD max items for `skills.refs`, `skills.gitRefs`, and `skills.s3Refs`. */
export const MAX_SKILLS_PER_SOURCE = 20;

/** Secret keys expected for S3 static credentials → skills.initContainer.env. */
export const S3_SKILLS_AWS_ACCESS_KEY_ID = "AWS_ACCESS_KEY_ID";
export const S3_SKILLS_AWS_SECRET_ACCESS_KEY = "AWS_SECRET_ACCESS_KEY";
export const S3_SKILLS_AWS_SESSION_TOKEN = "AWS_SESSION_TOKEN";

const S3_SKILLS_AWS_ENV_NAMES = new Set([
  S3_SKILLS_AWS_ACCESS_KEY_ID,
  S3_SKILLS_AWS_SECRET_ACCESS_KEY,
  S3_SKILLS_AWS_SESSION_TOKEN,
]);

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

/** Form row for `spec.skills.s3Refs` (S3SkillRef). */
export type S3SkillFormRow = {
  uri: string;
  region: string;
  name: string;
};

export function newEmptyS3SkillRow(): S3SkillFormRow {
  return { uri: "", region: "", name: "" };
}

/** Default /skills folder name: last URI path segment, archive extension stripped. */
export function defaultS3SkillFolderName(uri: string): string {
  const u = uri.trim().replace(/\/+$/, "");
  if (!u) return "";
  const base = lastPathSegment(u.replace(/^s3:\/\//i, ""));
  const lower = base.toLowerCase();
  if (lower.endsWith(".tar.gz")) return base.slice(0, -".tar.gz".length);
  if (lower.endsWith(".tgz")) return base.slice(0, -".tgz".length);
  if (lower.endsWith(".zip")) return base.slice(0, -".zip".length);
  return base;
}

export function applyS3SkillUriChange(row: S3SkillFormRow, uri: string): S3SkillFormRow {
  const oldDerived = defaultS3SkillFolderName(row.uri);
  const newDerived = defaultS3SkillFolderName(uri);
  const t = row.name.trim();
  const name = t === "" || t === oldDerived ? newDerived : row.name;
  return { ...row, uri, name };
}

export function s3RefToFormRow(ref: S3SkillRef): S3SkillFormRow {
  const uri = ref.uri || "";
  const d = defaultS3SkillFolderName(uri);
  return {
    uri,
    region: ref.region ?? "",
    name: (ref.name && ref.name.trim()) || d,
  };
}

export function formRowToS3Ref(row: S3SkillFormRow): S3SkillRef | null {
  const uri = row.uri.trim();
  if (!uri) return null;
  const o: S3SkillRef = { uri };
  const region = row.region.trim();
  if (region) o.region = region;
  const n = row.name.trim() || defaultS3SkillFolderName(uri);
  if (n) o.name = n;
  return o;
}

export function formRowsToS3Refs(rows: S3SkillFormRow[]): S3SkillRef[] {
  return rows.map(formRowToS3Ref).filter((r): r is S3SkillRef => r !== null);
}

export function isPlausibleS3Uri(uri: string): boolean {
  return /^s3:\/\/[^/]+\/.+/i.test(uri.trim());
}

export function s3SkillDedupeKey(uri: string): string {
  return uri.trim().toLowerCase().replace(/\/+$/, "");
}

export function isDuplicateS3SkillFormRow(
  row: S3SkillFormRow,
  resolved: S3SkillRef[],
): boolean {
  if (!row.uri.trim()) return false;
  const key = s3SkillDedupeKey(row.uri);
  return resolved.filter((r) => s3SkillDedupeKey(r.uri) === key).length > 1;
}

export function s3SkillRowUriIssues(row: S3SkillFormRow): {
  hasExtraWithoutUri: boolean;
  uriInvalid: boolean;
} {
  const uriTrim = row.uri.trim();
  const hasExtraWithoutUri =
    !uriTrim && !!(row.region.trim() || row.name.trim());
  const uriInvalid = uriTrim.length > 0 && !isPlausibleS3Uri(uriTrim);
  return { hasExtraWithoutUri, uriInvalid };
}

/** Build initContainer.env entries for static AWS keys from a Secret. */
export function s3SkillsAuthEnvFromSecret(secretName: string): EnvVar[] {
  const name = secretName.trim();
  if (!name) return [];
  return [
    {
      name: S3_SKILLS_AWS_ACCESS_KEY_ID,
      valueFrom: { secretKeyRef: { name, key: S3_SKILLS_AWS_ACCESS_KEY_ID } },
    },
    {
      name: S3_SKILLS_AWS_SECRET_ACCESS_KEY,
      valueFrom: { secretKeyRef: { name, key: S3_SKILLS_AWS_SECRET_ACCESS_KEY } },
    },
    {
      name: S3_SKILLS_AWS_SESSION_TOKEN,
      valueFrom: {
        secretKeyRef: {
          name,
          key: S3_SKILLS_AWS_SESSION_TOKEN,
          optional: true,
        },
      },
    },
  ];
}

/** Read secret name from initContainer.env AWS_ACCESS_KEY_ID secretKeyRef, if present. */
export function s3SkillsAuthSecretNameFromEnv(env: EnvVar[] | undefined): string {
  const accessKey = (env || []).find((e) => e.name === S3_SKILLS_AWS_ACCESS_KEY_ID);
  return accessKey?.valueFrom?.secretKeyRef?.name?.trim() || "";
}

/** Drop AWS credential env vars we manage; keep any other initContainer env. */
export function filterNonS3SkillsAuthEnv(env: EnvVar[] | undefined): EnvVar[] {
  return (env || []).filter((e) => !S3_SKILLS_AWS_ENV_NAMES.has(e.name));
}

export type DeclarativeAgentSkillsFormInput = {
  skillRefs: string[];
  skillGitRepos: GitSkillFormRow[];
  skillsGitAuthSecretName: string;
  skillS3Repos?: S3SkillFormRow[];
  skillsS3AuthSecretName?: string;
};

/** True when the form has at least one OCI, Git, or S3 skill source configured. */
export function declarativeAgentSkillsConfigured(
  input: DeclarativeAgentSkillsFormInput,
): boolean {
  const nonEmptyRefs = (input.skillRefs || []).some((ref) => ref.trim());
  const gitRepos = formRowsToGitRepos(input.skillGitRepos || []);
  const s3Refs = formRowsToS3Refs(input.skillS3Repos || []);
  return nonEmptyRefs || gitRepos.length > 0 || s3Refs.length > 0;
}

export const SUBSTRATE_SANDBOX_SKILLS_UNSUPPORTED_MSG =
  "Skills are not supported for Agent Substrate sandbox agents yet";

/** Returns an error when skills are configured on a sandbox (Agent Substrate) agent. */
export function validateSubstrateSandboxSkillsConflict(
  input: DeclarativeAgentSkillsFormInput,
  runInSandbox: boolean,
): string | undefined {
  if (!runInSandbox) {
    return undefined;
  }
  if (declarativeAgentSkillsConfigured(input)) {
    return SUBSTRATE_SANDBOX_SKILLS_UNSUPPORTED_MSG;
  }
  return undefined;
}

/**
 * Validates OCI refs, Git/S3 sources, and optional auth secrets for the declarative agent form.
 * Returns the first error message, or `undefined` if valid.
 */
export function validateDeclarativeAgentSkills(
  input: DeclarativeAgentSkillsFormInput,
): string | undefined {
  const nonEmptyRefs = (input.skillRefs || []).filter((ref) => ref.trim());
  const gitRepos = formRowsToGitRepos(input.skillGitRepos || []);
  const s3Refs = formRowsToS3Refs(input.skillS3Repos || []);

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

  const partialS3 = (input.skillS3Repos || []).some(
    (row) => !row.uri.trim() && !!(row.region.trim() || row.name.trim()),
  );
  if (partialS3) {
    return "S3 skill rows that set region or name need a URI";
  }
  if (s3Refs.length > MAX_SKILLS_PER_SOURCE) {
    return `At most ${MAX_SKILLS_PER_SOURCE} S3 skill sources are allowed`;
  }
  const badS3 = s3Refs.find((r) => !isPlausibleS3Uri(r.uri));
  if (badS3) {
    return `Invalid S3 URI (use s3://bucket/key-or-prefix): ${badS3.uri}`;
  }
  if (hasDuplicateStrings(s3Refs.map((r) => s3SkillDedupeKey(r.uri)))) {
    return "Duplicate S3 skill URI";
  }

  const s3Sec = input.skillsS3AuthSecretName?.trim();
  if (s3Sec && s3Refs.length === 0) {
    return "Add at least one S3 skill to use AWS credentials, or clear the secret name";
  }
  if (s3Sec && !isResourceNameValid(s3Sec)) {
    return "S3 auth secret name must be a valid Kubernetes resource name";
  }

  return undefined;
}
