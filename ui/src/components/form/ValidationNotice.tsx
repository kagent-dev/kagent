import { useTheme } from "@emotion/react";
import { FIX_FIELDS_MESSAGE } from "@/components/form/useSubmitValidation";

/** Says why the submit button is disabled. Nothing is sent until the fields pass. */
export function ValidationNotice({ id, "data-testid": testId }: { id?: string; "data-testid"?: string }) {
  const theme = useTheme();
  return (
    <span id={id} data-testid={testId} css={{ color: theme.color.textMuted }}>
      {FIX_FIELDS_MESSAGE}
    </span>
  );
}
