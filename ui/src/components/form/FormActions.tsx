import { useEffect, useId, useRef, type ReactNode } from "react";
import { Button, Space } from "antd";
import { useTheme } from "@emotion/react";
import { useNavigate } from "react-router-dom";
import { ValidationNotice } from "@/components/form/ValidationNotice";

interface FormActionsProps {
  /** "Create", "Save" or a more specific verb. */
  submitLabel: string;
  onSubmit: () => void;
  submitting?: boolean;
  /** A route to go back to, or a callback when the form is not its own page. */
  cancel: { to: string } | { onClick: () => void };
  /** A `SubmitError` or issue list, shown above the buttons. */
  error?: ReactNode;
  /** Invalid fields hold the submit back; a note beside it says why. */
  blocked?: boolean;
  /** Prefix for `-submit`, `-cancel` and `-blocked` test ids. */
  testIdPrefix?: string;
}

/** A form's footer: any error, then the primary action and Cancel. */
export function FormActions({
  submitLabel,
  onSubmit,
  submitting = false,
  cancel,
  error,
  blocked = false,
  testIdPrefix = "form",
}: FormActionsProps) {
  const theme = useTheme();
  const navigate = useNavigate();
  const footer = useRef<HTMLDivElement>(null);
  const noteId = useId();
  const hasError = error !== undefined && error !== null && error !== false;
  // aria-disabled keeps the button focusable, so keyboard and screen reader users reach the note.
  const disabledLook = {
    color: theme.color.textMuted,
    background: theme.color.border,
    borderColor: theme.color.border,
    boxShadow: "none",
    cursor: "not-allowed",
  };

  // The error pushes the buttons down; keep both in view. jsdom has no scrollIntoView.
  useEffect(() => {
    if (hasError) footer.current?.scrollIntoView?.({ block: "nearest" });
  }, [hasError]);

  return (
    <div ref={footer} css={{ display: "flex", flexDirection: "column", gap: theme.space(3) }}>
      {error}
      <Space size={8} wrap>
        <Button
          type="primary"
          data-testid={`${testIdPrefix}-submit`}
          loading={submitting}
          aria-disabled={blocked || undefined}
          aria-describedby={blocked ? noteId : undefined}
          onClick={blocked ? undefined : onSubmit}
          css={blocked ? { "&&&, &&&:hover, &&&:active, &&&:focus-visible": disabledLook } : undefined}
        >
          {submitLabel}
        </Button>
        <Button
          data-testid={`${testIdPrefix}-cancel`}
          disabled={submitting}
          onClick={() => ("to" in cancel ? void navigate(cancel.to) : cancel.onClick())}
        >
          Cancel
        </Button>
        {blocked ? <ValidationNotice id={noteId} data-testid={`${testIdPrefix}-blocked`} /> : null}
      </Space>
    </div>
  );
}
