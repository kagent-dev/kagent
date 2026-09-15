import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * The layout rules from `README.md`, checked.
 *
 * Two of the four were already enforced by ESLint — the shared fixture import and
 * antd's class names — and the other two were not enforced by anything, which this
 * branch's own README calls the way a convention regrows as an exception. It was
 * right: `auth/` had drifted and nothing said so.
 *
 * Here rather than in ESLint because these are claims about the *tree*: which folder a
 * file sits in, and how many specs a folder holds. A lint rule sees one file at a time
 * and would need a config block per folder to say it.
 *
 * The parse is deliberately shallow — the naming token of each top-level `test.describe`
 * or `test` at column zero, by regex rather than by AST. A nested describe or an oddly
 * indented test is invisible to it. That is a real limit and an acceptable one: this
 * checks a naming convention, and a title it cannot see is a title nobody greps for
 * either.
 */

const TESTS = join(__dirname, "tests");

/** Folders whose subject is one resource, and which therefore hold one spec. */
const RESOURCES = [
  "models",
  "mcp-servers",
  "prompts",
  "agent-templates",
  "harnesses",
  "schedules",
];

function specsIn(dir: string): string[] {
  return readdirSync(dir).filter((name) => name.endsWith(".spec.ts"));
}

function folders(): string[] {
  return readdirSync(TESTS).filter((name) =>
    statSync(join(TESTS, name)).isDirectory(),
  );
}

/** The naming token of each top-level `test.describe` or `test` in a file. */
function titles(file: string): string[] {
  const source = readFileSync(file, "utf8");
  const described = [...source.matchAll(/^test\.describe\("([^"]+)"/gm)].map(
    (match) => match[1],
  );
  if (described.length > 0) return described;
  return [...source.matchAll(/^test\("([^"]+)"/gm)].map((match) => match[1]);
}

describe("playwright layout", () => {
  it.each(folders())("every title in %s/ begins with its folder", (folder) => {
    // The folder is the surface; a title beginning with it is what makes `--grep`
    // able to select an area and a title able to say where it lives.
    const expected = folder.replace(/-/g, " ");
    for (const spec of specsIn(join(TESTS, folder))) {
      for (const title of titles(join(TESTS, folder, spec))) {
        // The prefix is everything before the first colon: `chat agent rail: …` is the
        // folder plus a narrower subject, and both halves count as beginning with it.
        const prefix = title.split(":")[0];
        expect(
          prefix === expected || prefix.startsWith(`${expected} `),
          `${folder}/${spec}: "${title}" should begin with "${expected}"`,
        ).toBe(true);
      }
    }
  });

  it.each(RESOURCES)("%s holds exactly one spec, holding one test", (resource) => {
    const specs = specsIn(join(TESTS, resource));
    expect(specs, `${resource}/ should hold one spec`).toHaveLength(1);

    const source = readFileSync(join(TESTS, resource, specs[0]), "utf8");
    const tests = [...source.matchAll(/^test\(/gm)];
    expect(
      tests,
      `${resource}/${specs[0]} should hold one test: the resource's whole life`,
    ).toHaveLength(1);
  });

  it("every spec outside a folder is about the application, not a resource", () => {
    // Top level means the shell, routing, the dashboard, theme contrast — things that
    // are about the app rather than about something it manages. A resource folder
    // appearing here would mean the rule above was never applied to it.
    const loose = specsIn(TESTS).map((name) => name.replace(".spec.ts", ""));
    expect(loose.filter((name) => RESOURCES.includes(name))).toEqual([]);
  });
});
