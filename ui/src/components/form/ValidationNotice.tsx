import { Alert } from "antd";
import { FIX_FIELDS_MESSAGE, FIX_FIELDS_TITLE } from "@/components/form/useSubmitValidation";

/** Shown when invalid fields stop a submit. Nothing was sent, so there is no retry. */
export function ValidationNotice({ "data-testid": testId }: { "data-testid"?: string }) {
  return <Alert type="warning" showIcon title={FIX_FIELDS_TITLE} description={FIX_FIELDS_MESSAGE} data-testid={testId} />;
}
