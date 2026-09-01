import { Input } from "antd";
import { useTheme } from "@emotion/react";
import { Search } from "lucide-react";

/**
 * The search box above a list.
 *
 * One component because every list needs the same thing and had been writing its own: a
 * bare input, each with its own placeholder wording and its own idea of how wide to be.
 * The differences were not decisions — they were four separate defaults.
 *
 * A magnifier and the word "Search" are the affordance. A field that only says what it
 * filters ("Filter by server, tool name or description…") describes its own behaviour to
 * somebody who has not yet worked out that it is a search box.
 */
export function SearchInput({
  value,
  onChange,
  placeholder,
  label,
  testId,
  maxWidth = 380,
}: {
  value: string;
  onChange: (next: string) => void;
  /** What searching here covers, in the reader's words. */
  placeholder: string;
  /** For screen readers, since the placeholder disappears once typing starts. */
  label: string;
  testId: string;
  maxWidth?: number;
}) {
  const theme = useTheme();

  return (
    <Input
      allowClear
      value={value}
      onChange={(event) => onChange(event.target.value)}
      placeholder={placeholder}
      aria-label={label}
      data-testid={testId}
      prefix={<Search size={14} color={theme.color.textMuted} aria-hidden />}
      css={{ maxWidth }}
    />
  );
}
