import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * The exports of `helpers/app` that are seeded mock rows rather than helpers.
 *
 * A list because the distinction is not visible in the name: `routes` and `loadApp`
 * hold on either backend, `instances` and `agents` are ids only the fixture backend
 * has.
 */
const FIXTURE_EXPORTS = ["instances", "agents"];

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
const SHARED = join(__dirname, "shared");

/**
 * Folders whose subject is the application rather than a resource it manages.
 *
 * Named so that `RESOURCES` can be everything else. The list used to run the other way
 * and a new resource folder then had to be remembered into it — which is the drift this
 * file exists to catch, so it should not need remembering here either.
 */
const NOT_RESOURCES = ["agents", "auth", "chat", "extension-points", "substrate"];

/** Folders whose subject is one resource, and which therefore hold one spec. */
function resources(): string[] {
  return folders().filter((name) => !NOT_RESOURCES.includes(name));
}

function specsIn(dir: string): string[] {
  return readdirSync(dir).filter((name) => name.endsWith(".spec.ts"));
}

/** Folders under `shared/`, which are resources for the same reason `tests/` ones are. */
function sharedFolders(): string[] {
  return readdirSync(SHARED).filter(
    (name) => !name.startsWith(".") && statSync(join(SHARED, name)).isDirectory(),
  );
}

/**
 * Every spec under `shared/`, nested ones included.
 *
 * Recursive deliberately: the resource journeys moved a directory down into
 * `shared/<resource>/`, and a top-level-only read would have quietly stopped applying
 * the rule below to exactly the specs it matters most for.
 */
function sharedSpecs(): string[] {
  return [
    ...specsIn(SHARED).map((name) => join(SHARED, name)),
    ...sharedFolders().flatMap((folder) =>
      specsIn(join(SHARED, folder)).map((name) => join(SHARED, folder, name)),
    ),
  ];
}

function folders(): string[] {
  return readdirSync(TESTS).filter(
    // Dot-directories are whatever a contributor's local tooling dropped here; they are
    // gitignored and are not part of the suite's layout.
    (name) => !name.startsWith(".") && statSync(join(TESTS, name)).isDirectory(),
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

  it.each(resources())("%s holds exactly one spec, holding one test", (resource) => {
    const specs = specsIn(join(TESTS, resource));
    expect(specs, `${resource}/ should hold one spec`).toHaveLength(1);

    const source = readFileSync(join(TESTS, resource, specs[0]), "utf8");
    // `test.skip(` counts: a skipped test is still a test, and not counting it let one
    // sit in a resource folder while the file still claimed to hold a single journey.
    const tests = [...source.matchAll(/^test(\.skip)?\(/gm)];
    expect(
      tests,
      `${resource}/${specs[0]} should hold one test: the resource's whole life`,
    ).toHaveLength(1);
  });

  it.each(resources())("%s covers its empty and failure states", (resource) => {
    /*
     * The states a list can be in that are not "here are the rows", and the pair a
     * reader must never see confused: "there are none" and "we could not find out" lead
     * to opposite conclusions.
     *
     * Checked here because it is the kind of coverage that goes missing quietly —
     * `agent-templates` and `harnesses` had neither, on this branch and on main, and
     * adding the failure step to `harnesses` immediately found a page that rendered a
     * failed read as an empty table.
     */
    const [spec] = specsIn(join(TESTS, resource));
    const source = readFileSync(join(TESTS, resource, spec), "utf8");
    for (const scenario of ["empty", "error"]) {
      expect(
        new RegExp(`scenario: "${scenario}"|mock=${scenario}`).test(source),
        `${resource}/${spec} should drive the "${scenario}" scenario`,
      ).toBe(true);
    }
  });

  it("a journey in one test carries the lifecycle budget", () => {
    /*
     * Applied by shape rather than by folder, which is how `chat/questions.spec.ts`
     * came to hold twelve steps on the thirty-second default and time out in CI at
     * step eight. A resource folder is not what makes a test long; the number of
     * steps sharing one budget is.
     */
    const offenders: string[] = [];
    for (const dir of [TESTS, ...folders().map((name) => join(TESTS, name))]) {
      for (const spec of specsIn(dir)) {
        const source = readFileSync(join(dir, spec), "utf8");
        const tests = [...source.matchAll(/^test(\.skip)?\(/gm)].length;
        const steps = [...source.matchAll(/test\.step\(/g)].length;
        if (tests === 1 && steps >= 10 && !source.includes("LIFECYCLE_TIMEOUT")) {
          offenders.push(`${spec} (${steps} steps)`);
        }
      }
    }
    expect(
      offenders,
      "a single test of ten or more steps needs `test.describe.configure({ timeout: LIFECYCLE_TIMEOUT })`",
    ).toEqual([]);
  });

  it("a shared spec asserts nothing only one backend can answer", () => {
    /*
     * `shared/` runs in both suites, so a scenario query there is either ignored by a
     * cluster or — worse — read as a claim the run cannot make. The narrowness is the
     * folder's whole value: assertions true on both backends are the weakest ones, and
     * a lifecycle drifting in here would quietly cost the mock suite its precision.
     */
    for (const spec of sharedSpecs()) {
      const source = readFileSync(spec, "utf8");
      expect(
        /mock=|withScenario|scenario:|helpers\/mockCalls/.test(source),
        `${spec} drives the mock backend, so it cannot run live`,
      ).toBe(false);

      /*
       * And the other half, which a scenario check misses entirely: a seeded row named
       * by id. `instances.ready` is a UUID out of `src/mocks/fixtures.ts` — legal
       * TypeScript, green in the mock lane, and a 404 against any cluster. The README
       * has always said this folder may not name a fixture; until this ran, nothing
       * checked the half that is not a query parameter.
       */
      const imported = [...source.matchAll(/import\s*\{([^}]*)\}\s*from\s*"[^"]*helpers\/app"/g)]
        .flatMap((match) => match[1].split(","))
        .map((binding) => binding.trim().split(/\s+as\s+/)[0].trim());
      const fixtures = imported.filter((binding) => FIXTURE_EXPORTS.includes(binding));
      expect(
        fixtures,
        `${spec} names seeded fixtures (${fixtures.join(", ")}), which no cluster has`,
      ).toEqual([]);
    }
  });

  it.each(sharedFolders())(
    "shared/%s holds one spec, holding one test, titled for its folder",
    (folder) => {
      /*
       * The same shape as a resource folder in `tests/`, and for the same reasons — one
       * journey per resource, and a title that says where it lives so `--grep` can
       * select an area. Not the empty-and-error rule, though: those need `?mock=`, which
       * is the one thing a spec here may not touch.
       */
      const specs = specsIn(join(SHARED, folder));
      expect(specs, `shared/${folder}/ should hold one spec`).toHaveLength(1);

      const source = readFileSync(join(SHARED, folder, specs[0]), "utf8");
      expect(
        [...source.matchAll(/^test(\.skip)?\(/gm)],
        `shared/${folder}/${specs[0]} should hold one test`,
      ).toHaveLength(1);

      const expected = folder.replace(/-/g, " ");
      for (const title of titles(join(SHARED, folder, specs[0]))) {
        const prefix = title.split(":")[0];
        expect(
          prefix === expected || prefix.startsWith(`${expected} `),
          `shared/${folder}/${specs[0]}: "${title}" should begin with "${expected}"`,
        ).toBe(true);
      }
    },
  );

  it("every spec outside a folder is about the application, not a resource", () => {
    // Top level means the shell, routing, the dashboard, theme contrast — things that
    // are about the app rather than about something it manages. A resource folder
    // appearing here would mean the rule above was never applied to it.
    const loose = specsIn(TESTS).map((name) => name.replace(".spec.ts", ""));
    expect(loose.filter((name) => resources().includes(name))).toEqual([]);
  });
});
