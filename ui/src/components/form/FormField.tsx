import { Fragment, cloneElement, isValidElement, useId, type ReactElement, type ReactNode } from "react";
import { Checkbox, Form, Radio, Segmented, type FormItemProps } from "antd";
import { useTheme } from "@emotion/react";

const visuallyHidden = {
  position: "absolute",
  width: 1,
  height: 1,
  overflow: "hidden",
  clip: "rect(0 0 0 0)",
  whiteSpace: "nowrap",
} as const;

interface FieldLabelProps {
  children: ReactNode;
  /** Required shows a red asterisk; optional shows "(optional)". */
  required: boolean;
  id?: string;
}

/** A field label that always says whether the field is required. */
export function FieldLabel({ children, required, id }: FieldLabelProps) {
  const theme = useTheme();
  return (
    <span id={id} css={{ display: "inline-flex", alignItems: "baseline", gap: theme.space(1) }}>
      {children}
      {required ? (
        <span data-testid="field-required">
          <span aria-hidden css={{ color: theme.color.dangerText }}>*</span>
          <span css={visuallyHidden}>(required)</span>
        </span>
      ) : (
        <span data-testid="field-optional" css={{ color: theme.color.textMuted, fontWeight: "normal" }}>
          (optional)
        </span>
      )}
    </span>
  );
}

type FormFieldProps = Omit<FormItemProps, "label" | "required" | "requiredMark"> & {
  label: ReactNode;
  required: boolean;
  /** Message shown under the control and marks it invalid. */
  error?: string;
  /** Called when the user leaves the field or types a value into it (a picker's search box does not count); from `validation.field`. */
  onTouch?: () => void;
};

type ControlProps = {
  id?: string;
  "aria-describedby"?: string;
  "aria-invalid"?: boolean;
  "aria-labelledby"?: string;
};

// antd puts these groups' id on a div, which a label's htmlFor cannot name.
const GROUP_TYPES: unknown[] = [Radio.Group, Checkbox.Group, Segmented];

function isControl(child: ReactElement<ControlProps>, htmlFor: string | undefined): boolean {
  if (child.type === Fragment) return false;
  if (typeof child.type === "string" && !["input", "select", "textarea"].includes(child.type)) return false;
  return htmlFor === undefined || child.props.id === htmlFor;
}

/**
 * A Form.Item with the shared required/optional label.
 * `required` has no default, so every field states which it is.
 */
export function FormField({
  label,
  required,
  error,
  validateStatus,
  help,
  extra,
  onTouch,
  children,
  ...rest
}: FormFieldProps) {
  const theme = useTheme();
  const autoId = useId();
  // `help` and `extra` are both supporting text: it sits between label and control, and the error below.
  const hasHint = help !== undefined || extra !== undefined;

  // A lone control gets the label's id and points at its hint and error. Fragments, plain wrapper
  // elements and children whose id differs from the caller's htmlFor are left as they are.
  const control = isValidElement<ControlProps>(children) && isControl(children, rest.htmlFor) ? children : undefined;
  const controlId = rest.htmlFor ?? control?.props.id ?? autoId;
  const isGroup = control !== undefined && GROUP_TYPES.includes(control.type);
  const labelId = `${controlId}-label`;
  const errorId = `${controlId}-error`;
  const hintId = `${controlId}-hint`;
  const describedBy = [control?.props["aria-describedby"], hasHint ? hintId : undefined, error ? errorId : undefined]
    .filter(Boolean)
    .join(" ");

  const field = control
    ? cloneElement(control, {
        id: control.props.id ?? controlId,
        "aria-invalid": error ? true : control.props["aria-invalid"],
        "aria-describedby": describedBy || undefined,
        ...(isGroup ? { "aria-labelledby": labelId } : {}),
      })
    : children;
  const body = hasHint && typeof field !== "function" ? (
    <>
      <div id={hintId} className="ant-form-item-extra" css={{ marginBottom: theme.space(2) }}>
        {help}
        {help !== undefined && extra !== undefined ? " " : null}
        {extra}
      </div>
      {field}
    </>
  ) : (
    field
  );

  return (
    <Form.Item
      // Outside an antd Form, Form.Item would default to horizontal with a colon.
      layout="vertical"
      colon={false}
      {...rest}
      htmlFor={control && !isGroup ? controlId : rest.htmlFor}
      // antd's own mark is replaced by FieldLabel's.
      required={false}
      label={
        <FieldLabel required={required} id={isGroup ? labelId : undefined}>
          {label}
        </FieldLabel>
      }
      validateStatus={error ? "error" : validateStatus}
      css={error ? { ".ant-form-item-explain": { paddingBottom: theme.space(2) } } : undefined}
      help={error ? <span id={errorId}>{error}</span> : undefined}
    >
      {onTouch && typeof body !== "function" ? (
        <div
          css={{ display: "contents" }}
          onBlur={onTouch}
          onInput={(event) => {
            const target = event.target as Partial<HTMLInputElement>;
            // Typing in a picker's search box filters its list; it does not touch the field.
            if (target.getAttribute?.("role") === "combobox") return;
            const value = target.value;
            if (typeof value === "string" && value.trim() !== "") onTouch();
          }}
        >
          {body}
        </div>
      ) : (
        body
      )}
    </Form.Item>
  );
}
