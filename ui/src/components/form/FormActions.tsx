import { useEffect, useRef, type ReactNode } from "react";
import { Button, Space } from "antd";
import { useTheme } from "@emotion/react";
import { useNavigate } from "react-router-dom";

interface FormActionsProps {
  /** "Create", "Save" or a more specific verb. */
  submitLabel: string;
  onSubmit: () => void;
  submitting?: boolean;
  /** A route to go back to, or a callback when the form is not its own page. */
  cancel: { to: string } | { onClick: () => void };
  /** A `SubmitError` or issue list, shown above the buttons. */
  error?: ReactNode;
  /** Prefix for `-submit` and `-cancel` test ids. */
  testIdPrefix?: string;
}

/** A form's footer: any error, then the primary action and Cancel. */
export function FormActions({
  submitLabel,
  onSubmit,
  submitting = false,
  cancel,
  error,
  testIdPrefix = "form",
}: FormActionsProps) {
  const theme = useTheme();
  const navigate = useNavigate();
  const footer = useRef<HTMLDivElement>(null);
  const hasError = error !== undefined && error !== null && error !== false;

  // The error pushes the buttons down; keep both in view. jsdom has no scrollIntoView.
  useEffect(() => {
    if (hasError) footer.current?.scrollIntoView?.({ block: "nearest" });
  }, [hasError]);

  return (
    <div ref={footer} css={{ display: "flex", flexDirection: "column", gap: theme.space(3) }}>
      {error}
      <Space size={8} wrap>
        <Button type="primary" data-testid={`${testIdPrefix}-submit`} loading={submitting} onClick={onSubmit}>
          {submitLabel}
        </Button>
        <Button
          data-testid={`${testIdPrefix}-cancel`}
          disabled={submitting}
          onClick={() => ("to" in cancel ? void navigate(cancel.to) : cancel.onClick())}
        >
          Cancel
        </Button>
      </Space>
    </div>
  );
}
