import { useState, type ClipboardEvent, type KeyboardEvent } from "react";
import { Button, Input, Tooltip } from "antd";
import { useTheme } from "@emotion/react";
import { Plus } from "lucide-react";
import { DataPill } from "./DataPill";
import { scanListText, type ListInputSplit } from "./listInputText";

interface ListInputProps {
  value: readonly string[];
  onChange: (next: string[]) => void;
  split: ListInputSplit;
  /** Drops an item already in the list. Leave off where repeats mean something, as in argv. */
  unique?: boolean;
  placeholder?: string;
  disabled?: boolean;
  id?: string;
  "aria-invalid"?: boolean;
  "aria-describedby"?: string;
  "data-testid"?: string;
}

/** A text box that turns what is typed into removable pills, with an icon-only Add button beside it. */
export function ListInput({
  value,
  onChange,
  split,
  unique,
  placeholder,
  disabled,
  id,
  "aria-invalid": ariaInvalid,
  "aria-describedby": ariaDescribedBy,
  "data-testid": testId,
}: ListInputProps) {
  const theme = useTheme();
  const [text, setText] = useState("");

  const add = (items: string[]) => {
    const kept = unique ? items.filter((item, at) => !value.includes(item) && items.indexOf(item) === at) : items;
    if (kept.length > 0) onChange([...value, ...kept]);
  };
  // Adds everything typed, including an item not yet closed by a separator.
  const flush = (all: string) => {
    const { items, trailing } = scanListText(all, split);
    add(trailing?.value ? [...items, trailing.value] : items);
    setText("");
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") {
      event.preventDefault();
      flush(text);
    } else if (event.key === "Backspace" && text === "" && value.length > 0) {
      onChange(value.slice(0, -1));
    }
  };
  const onPaste = (event: ClipboardEvent<HTMLInputElement>) => {
    const pasted = event.clipboardData.getData("text");
    if (scanListText(pasted, split).items.length === 0) return;
    event.preventDefault();
    flush(text + pasted);
  };

  return (
    <div data-testid={testId}>
      <div css={{ display: "flex", gap: theme.space(2) }}>
        <Input
          id={id}
          data-testid={testId ? `${testId}-input` : undefined}
          value={text}
          placeholder={placeholder}
          disabled={disabled}
          status={ariaInvalid ? "error" : undefined}
          aria-invalid={ariaInvalid}
          aria-describedby={ariaDescribedBy}
          onChange={(event) => {
            const { items, trailing } = scanListText(event.target.value, split);
            add(items);
            setText(items.length > 0 ? (trailing?.raw ?? "") : event.target.value);
          }}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
          onBlur={() => flush(text)}
          css={{ flex: 1, minWidth: 0, fontFamily: theme.font.mono }}
        />
        <Tooltip title="Add">
          <Button
            aria-label="Add"
            icon={<Plus size={16} aria-hidden />}
            disabled={disabled || text.trim() === ""}
            onClick={() => flush(text)}
            data-testid={testId ? `${testId}-add` : undefined}
            css={{ flexShrink: 0 }}
          />
        </Tooltip>
      </div>
      {value.length > 0 ? (
        <ul
          data-testid={testId ? `${testId}-list` : undefined}
          css={{
            display: "flex",
            flexWrap: "wrap",
            gap: theme.space(1),
            margin: `${theme.space(2)} 0 0`,
            padding: 0,
            listStyle: "none",
          }}
        >
          {value.map((item, at) => (
            // Repeats are allowed, so the position is part of the key.
            <li key={`${at}:${item}`} css={{ minWidth: 0 }}>
              <DataPill
                wrap
                disabled={disabled}
                onRemove={() => onChange(value.filter((_, other) => other !== at))}
              >
                {item}
              </DataPill>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
