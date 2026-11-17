import type { ReactNode } from "react";
import { Form, type FormItemProps } from "antd";
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
}

/** A field label that always says whether the field is required. */
export function FieldLabel({ children, required }: FieldLabelProps) {
  const theme = useTheme();
  return (
    <span css={{ display: "inline-flex", alignItems: "baseline", gap: theme.space(1) }}>
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
};

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
  children,
  ...rest
}: FormFieldProps) {
  const theme = useTheme();
  // A hint above stays visible with an error, which still shows below the control.
  const above = hintPlacement === "above" && help !== undefined;
  return (
    <Form.Item
      {...rest}
      // antd's own mark is replaced by FieldLabel's.
      required={false}
      label={<FieldLabel required={required}>{label}</FieldLabel>}
      validateStatus={error ? "error" : validateStatus}
      // A hint goes in `extra`: antd's `help` grows in from zero height and shifts the controls below.
      help={error}
      extra={!above && help !== undefined && !error ? help : extra}
    >
      {above ? (
        <>
          <div className="ant-form-item-extra" css={{ marginBottom: theme.space(2) }}>
            {help}
          </div>
          {children}
        </>
      ) : (
        children
      )}
    </Form.Item>
  );
}
