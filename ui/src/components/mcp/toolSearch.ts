/** A search over one MCP server's tools: any of `terms` matches (OR). */
export interface ToolSearch {
  terms: readonly string[];
  /** Whole tool names only, so `get_pods` does not also find `get_pods_logs`. */
  exact: boolean;
}

/** The search terms in a details page address: `?q=a&q=b`. */
export const TOOL_SEARCH_PARAM = "q";
/** Present as `exact=1` when the terms are whole tool names. */
export const TOOL_EXACT_PARAM = "exact";

/** The search an address holds. Blank and repeated terms are dropped. */
export function readToolSearch(params: URLSearchParams): ToolSearch {
  const terms = [...new Set(params.getAll(TOOL_SEARCH_PARAM).map((term) => term.trim()).filter(Boolean))];
  return { terms, exact: params.get(TOOL_EXACT_PARAM) === "1" };
}

/** `params` with `search` written into it, leaving every other parameter alone. */
export function writeToolSearch(params: URLSearchParams, search: ToolSearch): URLSearchParams {
  const next = new URLSearchParams(params);
  next.delete(TOOL_SEARCH_PARAM);
  next.delete(TOOL_EXACT_PARAM);
  for (const term of search.terms) next.append(TOOL_SEARCH_PARAM, term);
  if (search.exact) next.set(TOOL_EXACT_PARAM, "1");
  return next;
}

/**
 * Whether a tool matches: by whole name when exact, otherwise by any part of its name or
 * description. Case-insensitive, and no terms matches everything.
 */
export function toolMatches(
  tool: { name: string; description?: string },
  { terms, exact }: ToolSearch,
): boolean {
  const needles = terms.map((term) => term.trim().toLowerCase()).filter(Boolean);
  if (needles.length === 0) return true;
  const name = tool.name.toLowerCase();
  if (exact) return needles.includes(name);
  const description = (tool.description ?? "").toLowerCase();
  return needles.some((needle) => name.includes(needle) || description.includes(needle));
}
