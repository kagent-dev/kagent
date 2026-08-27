import { isExtensionPointId } from "./extensionPoints";
import { isVendorFormId } from "./formFields";
import { isVendorTableId } from "./tableColumns";
import { coreRouteKeys } from "@/router/router";
import type { VendorExtensionConfig } from "./types";

/**
 * Thrown when the global config is malformed. A broken extension config is a
 * deployment mistake, not a runtime condition to recover from, so this stops
 * the app rather than letting it boot with silently missing contributions.
 */
export class VendorExtensionConfigError extends Error {
  readonly problems: readonly string[];

  constructor(configId: string, problems: readonly string[]) {
    super(
      `Vendor extension "${configId}" is misconfigured:\n` +
        problems.map((problem) => `  - ${problem}`).join("\n"),
    );
    this.name = "VendorExtensionConfigError";
    this.problems = problems;
  }
}

/**
 * Checks the config against the registries, collecting every problem before
 * throwing so one boot reports the whole list.
 *
 * The slot-ID check is the load-bearing one: TypeScript already rejects an
 * unknown point in typed config, but a config deserialised from JSON has no
 * such protection, and a typo would otherwise mean a component that silently
 * never renders.
 */
export function validateVendorExtensionConfig(
  config: VendorExtensionConfig,
  reservedPaths: readonly string[] = [],
): void {
  const problems: string[] = [];

  for (const id of Object.keys(config.slots ?? {})) {
    if (!isExtensionPointId(id)) {
      problems.push(`slot "${id}" is not a known extension point`);
    }
  }

  const seenFieldIds = new Set<string>();
  for (const field of config.formFields ?? []) {
    if (!isVendorFormId(field.formId)) {
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
    if (!isVendorTableId(column.tableId)) {
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

  if (problems.length > 0) {
    throw new VendorExtensionConfigError(config.id, problems);
  }
}
