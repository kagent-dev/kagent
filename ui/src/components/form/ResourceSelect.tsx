import type { ReactNode } from "react";
import { Select } from "antd";

/** Lists longer than this (antd's open list shows about eight rows) offer type-to-search. */
export const SEARCH_MIN_OPTIONS = 8;

export interface ResourceSelectProps {
  /** Singular noun for placeholder and messages: "harness". */
  kind: string;
  /** Defaults to `${kind}s`. */
  plural?: string;
  names: readonly string[];
  loading: boolean;
  /** A failed read; the field is marked invalid and the list says why. */
  error?: string;
  value?: string;
  onChange: (next: string) => void;
  /** Shown in the open list when the read succeeded with nothing in it. */
  empty?: ReactNode;
  placeholder?: string;
  disabled?: boolean;
  id?: string;
  "data-testid"?: string;
}

/** "a harness", "an agent template": the article by the noun's first letter. */
const withArticle = (noun: string): string => `${/^[aeiou]/i.test(noun) ? "an" : "a"} ${noun}`;

/** Picks one resource by name from a list the caller has read. */
export function ResourceSelect({
  kind,
  plural = `${kind}s`,
  names,
  loading,
  error,
  value,
  onChange,
  empty,
  placeholder = `Choose ${withArticle(kind)}`,
  disabled,
  id,
  "data-testid": testId,
}: ResourceSelectProps) {
  return (
    <Select
      id={id}
      data-testid={testId}
      placeholder={placeholder}
      value={value}
      onChange={onChange}
      disabled={disabled}
      loading={loading}
      status={error ? "error" : undefined}
      aria-invalid={error ? true : undefined}
      showSearch={names.length > SEARCH_MIN_OPTIONS}
      options={names.map((name) => ({ value: name, label: name }))}
      notFoundContent={
        loading ? `Loading ${plural}…` : error ? `Could not load ${plural}: ${error}` : (empty ?? `No ${plural} found`)
      }
    />
  );
}
