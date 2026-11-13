import { Alert, Button } from "antd";
import { isPermissionDenied } from "@/api/ApiError";

interface ReadErrorProps {
  resource: string;
  error: Error;
  testId: string;
  title?: string;
  description?: string;
  onRetry?: () => Promise<void>;
  type?: "error" | "warning";
}

export function ReadError({ resource, error, testId, title, description, onRetry, type = "error" }: ReadErrorProps) {
  const denied = isPermissionDenied(error);
  return (
    <Alert
      type={denied ? "warning" : type}
      showIcon
      title={denied ? `You do not have access to ${resource}` : (title ?? `Could not load ${resource}`)}
      description={denied ? undefined : (description ?? error.message)}
      data-testid={testId}
      action={!denied && onRetry ? (
        <Button size="small" onClick={() => void onRetry().catch(() => {
          // The resource reports a failed retry through its error state.
        })}>
          Try again
        </Button>
      ) : undefined}
    />
  );
}
