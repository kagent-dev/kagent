/**
 * JSON for display, with object keys in a fixed order.
 *
 * A tool's payload reaches us as a protobuf `Struct` flattened to plain JSON, and
 * the order of a `Struct`'s fields is whatever the sender happened to emit — for a
 * Go map, a different order on every marshal. Printed as-is, a payload that has
 * not changed at all reshuffles its lines each time the transcript is fetched, so
 * the value someone was reading moves out from under them mid-read.
 *
 * Sorting is only about the printed form: nothing downstream reads this string, so
 * a fixed order costs nothing and makes the same payload render the same way every
 * time. Array order is left alone — there it is the data, not an accident.
 */
export function stableJson(value: unknown): string {
  return JSON.stringify(withSortedKeys(value), null, 2);
}

function withSortedKeys(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(withSortedKeys);
  if (value === null || typeof value !== "object") return value;

  const record = value as Record<string, unknown>;
  return Object.fromEntries(
    Object.keys(record)
      .sort()
      .map((key) => [key, withSortedKeys(record[key])]),
  );
}
