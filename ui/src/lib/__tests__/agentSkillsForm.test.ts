import { describe, expect, it } from "@jest/globals";
import type { GitRepo } from "@/types";
import {
  MAX_SKILLS_PER_SOURCE,
  applyGitSkillUrlPathChange,
  defaultGitSkillFolderName,
  formRowToGitRepo,
  formRowsToGitRepos,
  gitRepoToFormRow,
  gitSkillDedupeKeyFromFormRow,
  gitSkillDedupeKeyFromRepo,
  gitSkillSourceDedupeKey,
  gitSkillRowUrlIssues,
  isDuplicateGitSkillFormRow,
  isDuplicateOciSkillRef,
  isPlausibleGitRemoteUrl,
  isValidSkillContainerImage,
  newEmptyGitSkillRow,
  validateDeclarativeAgentSkills,
  type GitSkillFormRow,
} from "../agentSkillsForm";

describe("agentSkillsForm", () => {
  describe("newEmptyGitSkillRow", () => {
    it("returns an empty row", () => {
      expect(newEmptyGitSkillRow()).toEqual({
        url: "",
        ref: "",
        path: "",
        name: "",
      });
    });
  });

  describe("gitRepoToFormRow", () => {
    it("maps API fields to form strings", () => {
      expect(
        gitRepoToFormRow({
          url: "https://github.com/a/b.git",
          ref: "v1",
          path: "pkg/skill",
          name: "myskill",
        }),
      ).toEqual({
        url: "https://github.com/a/b.git",
        ref: "v1",
        path: "pkg/skill",
        name: "myskill",
      });
    });

    it("defaults name from URL when name omitted in API", () => {
      expect(gitRepoToFormRow({ url: "https://x/y" })).toEqual({
        url: "https://x/y",
        ref: "",
        path: "",
        name: "y",
      });
    });

    it("defaults name from path when name omitted in API", () => {
      expect(
        gitRepoToFormRow({
          url: "https://github.com/peterj/myskills",
          path: "someskills/skill1",
        }),
      ).toEqual({
        url: "https://github.com/peterj/myskills",
        ref: "",
        path: "someskills/skill1",
        name: "skill1",
      });
    });
  });

  describe("defaultGitSkillFolderName", () => {
    it("uses last path segment from in-repo path", () => {
      expect(defaultGitSkillFolderName("https://github.com/peterj/myskills", "/someskills/skill1/")).toBe("skill1");
    });

    it("uses repo name when path is empty", () => {
      expect(defaultGitSkillFolderName("https://github.com/peterj/myskills", "")).toBe("myskills");
    });

    it("handles scp-style GitHub URL", () => {
      expect(defaultGitSkillFolderName("git@github.com:peterj/myskills.git", "")).toBe("myskills");
    });
  });

  describe("applyGitSkillUrlPathChange", () => {
    it("autofills name from URL when name was empty", () => {
      const row: GitSkillFormRow = { url: "", ref: "", path: "", name: "" };
      expect(applyGitSkillUrlPathChange(row, { url: "https://github.com/peterj/myskills" })).toMatchObject({
        name: "myskills",
      });
    });

    it("replaces name when it matched previous default and path is added", () => {
      const row: GitSkillFormRow = {
        url: "https://github.com/peterj/myskills",
        ref: "main",
        path: "",
        name: "myskills",
      };
      const next = applyGitSkillUrlPathChange(row, { path: "a/skill1" });
      expect(next.name).toBe("skill1");
    });

    it("keeps a custom name when it does not match previous default", () => {
      const row: GitSkillFormRow = {
        url: "https://github.com/peterj/myskills",
        ref: "main",
        path: "a/b",
        name: "custom",
      };
      const next = applyGitSkillUrlPathChange(row, { path: "x/y" });
      expect(next.name).toBe("custom");
    });
  });

  describe("formRowToGitRepos", () => {
    it("matches one row of formRowsToGitRepos for non-empty URL", () => {
      const row: GitSkillFormRow = {
        url: "https://a/b",
        ref: "r1",
        path: "p/q",
        name: "",
      };
      const one = formRowToGitRepo(row);
      const batch = formRowsToGitRepos([row]);
      expect(one).toEqual(batch[0]);
    });

    it("returns null for blank URL", () => {
      expect(formRowToGitRepo({ url: "  ", ref: "x", path: "", name: "" })).toBeNull();
    });
  });

  describe("formRowsToGitRepos", () => {
    it("drops rows with blank URL", () => {
      const rows: GitSkillFormRow[] = [
        { url: "", ref: "main", path: "", name: "" },
        { url: "  https://github.com/o/r.git  ", ref: "", path: "", name: "" },
      ];
      expect(formRowsToGitRepos(rows)).toEqual([{ url: "https://github.com/o/r.git", name: "r" }]);
    });

    it("includes optional ref, path, name when non-empty", () => {
      const rows: GitSkillFormRow[] = [
        {
          url: "git@github.com:o/r.git",
          ref: " develop ",
          path: " skills/x ",
          name: " myname ",
        },
      ];
      expect(formRowsToGitRepos(rows)).toEqual([
        {
          url: "git@github.com:o/r.git",
          ref: "develop",
          path: "skills/x",
          name: "myname",
        },
      ]);
    });
  });

  describe("isPlausibleGitRemoteUrl", () => {
    it.each([
      ["https://github.com/a/b.git", true],
      ["http://internal/git", true],
      ["git@github.com:a/b.git", true],
      ["ssh://git@host/repo", true],
      ["git://host/repo", true],
      ["", false],
      ["github.com/a/b", false],
      ["ftp://host/r.git", false],
    ])("%s → %s", (url, expected) => {
      expect(isPlausibleGitRemoteUrl(url)).toBe(expected);
    });
  });

  describe("isValidSkillContainerImage", () => {
    it("rejects empty or whitespace", () => {
      expect(isValidSkillContainerImage("")).toBe(false);
      expect(isValidSkillContainerImage("   ")).toBe(false);
    });

    it("accepts typical registry/repo:tag refs", () => {
      expect(isValidSkillContainerImage("ghcr.io/org/skill:v1")).toBe(true);
      expect(isValidSkillContainerImage("docker.io/library/alpine:latest")).toBe(true);
    });
  });

  describe("gitSkillDedupeKeyFromRepo / gitSkillDedupeKeyFromFormRow", () => {
    it("normalizes case and trims", () => {
      const g: GitRepo = { url: " HTTPS://X/Y ", ref: " Main ", path: " P " };
      expect(gitSkillDedupeKeyFromRepo(g)).toBe("https://x/y|main|p");
    });

    it("matches between repo and form row for same logical repo", () => {
      const row: GitSkillFormRow = {
        url: "https://a/b",
        ref: "r",
        path: "p",
        name: "n",
      };
      const repo = formRowsToGitRepos([row])[0];
      expect(gitSkillDedupeKeyFromFormRow(row)).toBe(gitSkillDedupeKeyFromRepo(repo));
    });
  });

  describe("gitSkillRowUrlIssues", () => {
    it("flags ref/path/name without URL", () => {
      expect(gitSkillRowUrlIssues({ url: "", ref: "main", path: "", name: "" })).toEqual({
        hasExtraWithoutUrl: true,
        urlInvalid: false,
      });
    });

    it("flags invalid URL scheme", () => {
      expect(gitSkillRowUrlIssues({ url: "noscheme/repo", ref: "", path: "", name: "" })).toEqual({
        hasExtraWithoutUrl: false,
        urlInvalid: true,
      });
    });

    it("clean empty row has no issues", () => {
      expect(gitSkillRowUrlIssues(newEmptyGitSkillRow())).toEqual({
        hasExtraWithoutUrl: false,
        urlInvalid: false,
      });
    });
  });

  describe("isDuplicateGitSkillFormRow", () => {
    const resolved: GitRepo[] = [
      { url: "https://a/b", ref: "main", path: "p1" },
      { url: "https://a/b", ref: "main", path: "p1" },
    ];

    it("returns false when URL is empty", () => {
      expect(
        isDuplicateGitSkillFormRow({ url: "", ref: "", path: "", name: "" }, resolved),
      ).toBe(false);
    });

    it("returns true when row matches duplicate in resolved list", () => {
      const row: GitSkillFormRow = {
        url: "https://a/b",
        ref: "main",
        path: "p1",
        name: "",
      };
      expect(isDuplicateGitSkillFormRow(row, resolved)).toBe(true);
    });
  });

  describe("isDuplicateOciSkillRef", () => {
    it("returns false for blank ref", () => {
      expect(isDuplicateOciSkillRef("", ["a", "b"])).toBe(false);
    });

    it("is case-insensitive", () => {
      const all = ["ghcr.io/x:v1", "GHCR.IO/x:v1"];
      expect(isDuplicateOciSkillRef("ghcr.io/x:v1", all)).toBe(true);
    });
  });

  describe("validateDeclarativeAgentSkills", () => {
    it("returns undefined when skills are empty", () => {
      expect(
        validateDeclarativeAgentSkills({
          skillRefs: ["", ""],
          skillGitRepos: [newEmptyGitSkillRow()],
          skillsGitAuthSecretName: "",
        }),
      ).toBeUndefined();
    });

    it("errors on invalid OCI ref", () => {
      const msg = validateDeclarativeAgentSkills({
        skillRefs: ["not-a-valid-image!!!"],
        skillGitRepos: [newEmptyGitSkillRow()],
        skillsGitAuthSecretName: "",
      });
      expect(msg).toMatch(/Invalid container image format/);
    });

    it("errors on duplicate OCI refs", () => {
      const msg = validateDeclarativeAgentSkills({
        skillRefs: ["ghcr.io/o/s:v1", "ghcr.io/o/s:v1"],
        skillGitRepos: [newEmptyGitSkillRow()],
        skillsGitAuthSecretName: "",
      });
      expect(msg).toMatch(/Duplicate skill image/);
    });

    it("errors when OCI count exceeds max", () => {
      const refs = Array.from({ length: MAX_SKILLS_PER_SOURCE + 1 }, (_, i) => `ghcr.io/o/s${i}:v1`);
      const msg = validateDeclarativeAgentSkills({
        skillRefs: refs,
        skillGitRepos: [newEmptyGitSkillRow()],
        skillsGitAuthSecretName: "",
      });
      expect(msg).toMatch(/At most/);
    });

    it("errors on partial Git row (ref without URL)", () => {
      const msg = validateDeclarativeAgentSkills({
        skillRefs: [],
        skillGitRepos: [{ url: "", ref: "main", path: "", name: "" }],
        skillsGitAuthSecretName: "",
      });
      expect(msg).toMatch(/need a repository URL/);
    });

    it("errors on invalid Git URL", () => {
      const msg = validateDeclarativeAgentSkills({
        skillRefs: [],
        skillGitRepos: [{ url: "bad-scheme/x", ref: "", path: "", name: "" }],
        skillsGitAuthSecretName: "",
      });
      expect(msg).toMatch(/Invalid Git URL/);
    });

    it("errors on duplicate Git repos", () => {
      const row: GitSkillFormRow = {
        url: "https://github.com/a/b.git",
        ref: "main",
        path: "",
        name: "",
      };
      const msg = validateDeclarativeAgentSkills({
        skillRefs: [],
        skillGitRepos: [row, { ...row }],
        skillsGitAuthSecretName: "",
      });
      expect(msg).toMatch(/Duplicate Git skill/);
    });

    it("errors when secret set but no Git repos", () => {
      const msg = validateDeclarativeAgentSkills({
        skillRefs: ["ghcr.io/o/s:v1"],
        skillGitRepos: [newEmptyGitSkillRow()],
        skillsGitAuthSecretName: "my-secret",
      });
      expect(msg).toMatch(/Add at least one Git repository/);
    });

    it("errors on invalid secret name", () => {
      const msg = validateDeclarativeAgentSkills({
        skillRefs: [],
        skillGitRepos: [
          { url: "https://github.com/a/b.git", ref: "", path: "", name: "" },
        ],
        skillsGitAuthSecretName: "Bad_Name",
      });
      expect(msg).toMatch(/valid Kubernetes resource name/);
    });

    it("allows valid secret with Git repo", () => {
      expect(
        validateDeclarativeAgentSkills({
          skillRefs: [],
          skillGitRepos: [
            { url: "https://github.com/a/b.git", ref: "", path: "", name: "" },
          ],
          skillsGitAuthSecretName: "git-auth",
        }),
      ).toBeUndefined();
    });

    it("allows OCI and Git together when both valid", () => {
      expect(
        validateDeclarativeAgentSkills({
          skillRefs: ["ghcr.io/org/skill:v1"],
          skillGitRepos: [
            { url: "https://github.com/o/r.git", ref: "main", path: "skills/x", name: "" },
          ],
          skillsGitAuthSecretName: "",
        }),
      ).toBeUndefined();
    });
  });
});
