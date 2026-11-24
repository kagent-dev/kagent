import { useLayoutEffect, useRef } from "react";
import { Input, type InputProps, type InputRef } from "antd";
import { sanitizeName } from "@/components/common/resourceName";

type NameInputProps = Omit<InputProps, "value" | "onChange"> & {
  value: string;
  onChange: (value: string) => void;
  /** Object names allow dots; namespaces and labels do not. */
  dots: boolean;
};

/** A resource name box that rewrites typed and pasted text to a name's characters as it goes. */
export function NameInput({ value, onChange, dots, ...rest }: NameInputProps) {
  const input = useRef<InputRef>(null);
  const caret = useRef<number | undefined>(undefined);

  // A rewritten value moves the caret to the end on render; put it back where the user was typing.
  useLayoutEffect(() => {
    if (caret.current === undefined) return;
    input.current?.input?.setSelectionRange(caret.current, caret.current);
    caret.current = undefined;
  });

  return (
    <Input
      {...rest}
      ref={input}
      value={value}
      onChange={(event) => {
        // antd hands over a copy of the input, so the caret is read from and set on the real one.
        const element = input.current?.input;
        const pasted = (event.nativeEvent as Partial<InputEvent>).inputType === "insertFromPaste";
        const raw = pasted ? event.target.value.trimEnd() : event.target.value;
        const next = sanitizeName(raw, dots);
        caret.current = undefined;
        if (element && next !== event.target.value) {
          const at = sanitizeName(raw.slice(0, element.selectionStart ?? raw.length), dots).length;
          // A changed value renders and moves the caret, so the effect restores it; an unchanged one never renders.
          if (next !== value) caret.current = at;
          element.setRangeText(next, 0, element.value.length);
          element.setSelectionRange(at, at);
        }
        onChange(next);
      }}
    />
  );
}
