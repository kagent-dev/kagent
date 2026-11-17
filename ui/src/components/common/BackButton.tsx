import { Button } from "antd";
import { ArrowLeft } from "lucide-react";
import type { useGoBack } from "@/router/backLink";

/** The shared back control. `onClick` replaces the navigation, for a page that must ask first. */
export function BackButton({
  back,
  onClick,
  testId,
}: {
  back: ReturnType<typeof useGoBack>;
  onClick?: () => void;
  testId?: string;
}) {
  return (
    <Button icon={<ArrowLeft size={14} aria-hidden />} onClick={onClick ?? back.goBack} data-testid={testId}>
      {back.label}
    </Button>
  );
}
