import { isExtensionPointId } from "./extensionPoints";
import { isExtensionFormId } from "./formFields";
import { isExtensionTableId } from "./tableColumns";
import { coreRouteKeys } from "@/router/router";
import type { AppExtensionConfig } from "./types";

/**
 * Thrown when an install is malformed. A broken extension config is a deployment
 * mistake, not a runtime condition to recover from, so this stops the app rather
 * than letting it boot with silently missing contributions.
 */
export class AppExtensionConfigError extends Error {
  readonly problems: readonly string[];

  constructor(subject: string, problems: readonly string[]) {
    super(
      `${subject} is misconfigured:\n` +
        problems.map((problem) => `  - ${problem}`).join("\n"),
    );
    this.name = "AppExtensionConfigError";
    this.problems = problems;
  }
}

/**
 * Checks one config against the registries, collecting every problem before
 * throwing so one boot reports the whole list.
 *
 * The slot-ID check is the load-bearing one: TypeScript already rejects an
 * unknown point in typed config, but a config deserialised from JSON has no
 * such protection, and a typo would otherwise mean a component that silently
 * never renders.
 */
export function validateExtensionConfig(
  config: AppExtensionConfig,
  reservedPaths: readonly string[] = [],
): void {
  const problems = problemsIn(config, reservedPaths);

  if (problems.length > 0) {
    throw new AppExtensionConfigError(`App extension "${config.id}"`, problems);
  }
}

/**
 * Checks a whole install: each config on its own, then the collisions only the
 * install can see.
 *
 * Two extensions can each be perfectly valid and still be impossible to install
 * together — both claiming `/insights`, both contributing a nav entry keyed
 * `insights`, or the same extension listed twice. Each of those resolves silently
 * and arbitrarily at runtime (the router takes the first match, React warns about
 * the duplicate key and moves on), which is exactly the class of failure this
 * function exists to turn into a boot error.
 *
 * Everything is collected before throwing, across every extension, so one boot
 * reports the whole list rather than one problem per restart.
 */
export function validateAppExtensions(
  extensions: readonly AppExtensionConfig[],
  reservedPaths: readonly string[] = [],
): void {
  const problems: string[] = [];

  for (const config of extensions) {
    problems.push(
      ...problemsIn(config, reservedPaths).map(
        (problem) => `${config.id}: ${problem}`,
      ),
    );
  }

  problems.push(...collisionsAcross(extensions));

  if (problems.length > 0) {
    throw new AppExtensionConfigError("The installed app extensions", problems);
  }
}

/** Everything wrong with one config, in the order the reader would look for it. */
function problemsIn(
  config: AppExtensionConfig,
  reservedPaths: readonly string[],
): string[] {
  const problems: string[] = [];

  for (const id of Object.keys(config.slots ?? {})) {
    if (!isExtensionPointId(id)) {
      problems.push(`slot "${id}" is not a known extension point`);
    }
  }

  const seenFieldIds = new Set<string>();
  for (const field of config.formFields ?? []) {
    if (!isExtensionFormId(field.formId)) {
      problems.push(
        `form field "${field.id}" targets unknown form "${field.formId}"`,
      );
    }
    const scopedId = `${field.formId}/${field.id}`;
    if (seenFieldIds.has(scopedId)) {
      problems.push(
        `form field "${field.id}" is declared twice on "${field.formId}"`,
      );
    }
    seenFieldIds.add(scopedId);
  }

  const seenNavKeys = new Set<string>();
  const seenColumnIds = new Set<string>();
  for (const column of config.tableColumns ?? []) {
    if (!isExtensionTableId(column.tableId)) {
      problems.push(
        `table column "${column.id}" targets unknown table "${column.tableId}"`,
      );
    }
    const scoped = `${column.tableId}:${column.id}`;
    if (seenColumnIds.has(scoped)) {
      problems.push(`table column "${column.id}" is declared twice`);
    }
    seenColumnIds.add(scoped);
  }

  for (const item of config.navItems ?? []) {
    if (seenNavKeys.has(item.key)) {
      problems.push(`nav item key "${item.key}" is declared twice`);
    }
    seenNavKeys.add(item.key);
  }

  const reserved = new Set(reservedPaths);
  const seenPaths = new Set<string>();
  for (const route of config.routes ?? []) {
    if (route.replaces !== undefined && !coreRouteKeys.includes(route.replaces)) {
      problems.push(
        `route "${route.path}" declares it replaces "${route.replaces}", which is not a route this application has`,
      );
    }
    // A collision is an error unless the contribution says what it is replacing.
    if (reserved.has(route.path) && route.replaces === undefined) {
      problems.push(
        `route "${route.path}" collides with a core route. Set \`replaces\` to the key of the route it takes the place of if that is deliberate.`,
      );
    }
    if (seenPaths.has(route.path)) {
      problems.push(`route "${route.path}" is declared twice`);
    }
    seenPaths.add(route.path);
  }

  return problems;
}

/** What only the whole install can see: two extensions claiming the same thing. */
function collisionsAcross(extensions: readonly AppExtensionConfig[]): string[] {
  const problems: string[] = [];

  problems.push(
    ...duplicates(
      extensions.map((extension) => ({ owner: extension.id, value: extension.id })),
    ).map(([id]) => `extension "${id}" is installed more than once`),
  );

  problems.push(
    ...duplicates(
      extensions.flatMap((extension) =>
        (extension.navItems ?? []).map((item) => ({
          owner: extension.id,
          value: item.key,
        })),
      ),
    ).map(
      ([key, owners]) =>
        `nav item key "${key}" is contributed by more than one extension (${owners.join(", ")})`,
    ),
  );

  problems.push(
    ...duplicates(
      extensions.flatMap((extension) =>
        (extension.routes ?? []).map((route) => ({
          owner: extension.id,
          value: route.path,
        })),
      ),
    ).map(
      ([path, owners]) =>
        `route "${path}" is contributed by more than one extension (${owners.join(", ")})`,
    ),
  );

  return problems;
}

/** Values claimed more than once, each with the extensions that claimed it. */
function duplicates(
  claims: readonly { owner: string; value: string }[],
): [string, string[]][] {
  const owners = new Map<string, string[]>();
  for (const { owner, value } of claims) {
    owners.set(value, [...(owners.get(value) ?? []), owner]);
  }

  return [...owners].filter(([, claimed]) => claimed.length > 1);
}
