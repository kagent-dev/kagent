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
  /** Message shown under the field and marks it invalid. */
  error?: string;
  /** "above" puts the hint between label and control, for sections of repeated rows. */
  hintPlacement?: "below" | "above";
  /** Called when the user leaves the field or types a value into it; from `validation.field`. */
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
  hintPlacement = "below",
  onTouch,
  children,
  ...rest
}: FormFieldProps) {
  const theme = useTheme();
  const autoId = useId();
  // A hint above stays visible with an error, which still shows below the control.
  const above = hintPlacement === "above" && help !== undefined;
  const hintBelow = !above && help !== undefined && !error;

  // A lone control gets the label's id and points at its error or hint. Fragments, plain wrapper
  // elements and children whose id differs from the caller's htmlFor are left as they are.
  const control = !above && isValidElement<ControlProps>(children) && isControl(children, rest.htmlFor)
    ? children
    : undefined;
  const controlId = rest.htmlFor ?? control?.props.id ?? autoId;
  const isGroup = control !== undefined && GROUP_TYPES.includes(control.type);
  const labelId = `${controlId}-label`;
  const errorId = `${controlId}-error`;
  const hintId = `${controlId}-hint`;
  const describedBy = [control?.props["aria-describedby"], error ? errorId : hintBelow ? hintId : undefined]
    .filter(Boolean)
    .join(" ");

  const body = control ? (
    cloneElement(control, {
      id: control.props.id ?? controlId,
      "aria-invalid": error ? true : control.props["aria-invalid"],
      "aria-describedby": describedBy || undefined,
      ...(isGroup ? { "aria-labelledby": labelId } : {}),
    })
  ) : above ? (
    <>
      <div className="ant-form-item-extra" css={{ marginBottom: theme.space(2) }}>
        {help}
      </div>
      {children}
    </>
  ) : (
    children
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
      // A hint goes in `extra`: antd's `help` grows in from zero height and shifts the controls below.
      help={error ? <span id={errorId}>{error}</span> : undefined}
      extra={
        hintBelow ? (
          <>
            <span id={hintId}>{help}</span>
            {extra}
          </>
        ) : (
          extra
        )
      }
    >
      {onTouch && typeof body !== "function" ? (
        <div
          css={{ display: "contents" }}
          onBlur={onTouch}
          onInput={(event) => {
            const value = (event.target as Partial<HTMLInputElement>).value;
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
