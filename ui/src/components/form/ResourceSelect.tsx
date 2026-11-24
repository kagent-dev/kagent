import { useState, type ReactNode } from "react";
import { Select } from "antd";

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
  /** Also offers a typed name that is not in the list, for one created after the list was read. */
  allowTyped?: boolean;
  disabled?: boolean;
  id?: string;
  "aria-invalid"?: boolean;
  "aria-describedby"?: string;
  "data-testid"?: string;
}

/** "a harness", "an agent template": the article by the noun's first letter. */
const withArticle = (noun: string): string => `${/^[aeiou]/i.test(noun) ? "an" : "a"} ${noun}`;

/** Picks one resource by name from a list the caller has read; typing filters the list. */
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
  allowTyped = false,
  disabled,
  id,
  "aria-invalid": ariaInvalid,
  "aria-describedby": ariaDescribedBy,
  "data-testid": testId,
}: ResourceSelectProps) {
  const [search, setSearch] = useState("");
  const typed = search.trim();
  const options = names.map((name) => ({ value: name, label: name }));
  // A chosen name the list lacks still displays as itself.
  if (value && !names.includes(value)) options.push({ value, label: value });
  if (allowTyped && typed !== "" && typed !== value && !names.includes(typed)) {
    options.push({ value: typed, label: `Use "${typed}"` });
  }

  return (
    <Select
      id={id}
      data-testid={testId}
      placeholder={placeholder}
      value={value}
      onChange={(next: string) => {
        // antd clears its search on select without calling onSearch.
        setSearch("");
        onChange(next);
      }}
      onOpenChange={(open) => {
        if (!open) setSearch("");
      }}
      disabled={disabled}
      loading={loading}
      status={error ? "error" : undefined}
      aria-invalid={error ? true : ariaInvalid}
      aria-describedby={ariaDescribedBy}
      autoComplete="off"
      showSearch={allowTyped ? { onSearch: setSearch } : true}
      options={options}
      notFoundContent={
        loading ? `Loading ${plural}…` : error ? `Could not load ${plural}: ${error}` : (empty ?? `No ${plural} found`)
      }
    />
  );
}
