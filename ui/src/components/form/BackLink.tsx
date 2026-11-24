import { useTheme } from "@emotion/react";
import { ArrowLeft } from "lucide-react";
import type { MouseEvent } from "react";
import { Link } from "react-router-dom";
import { linkInk } from "@/components/common/linkStyles";
import type { useGoBack } from "@/router/backLink";

interface BackLinkProps {
  to: string;
  /** The list being returned to, plural and capitalised: "Models". */
  resource: string;
  onClick?: (event: MouseEvent<HTMLAnchorElement>) => void;
  "data-testid"?: string;
}

/** The breadcrumb above a form's title: "← Models". */
export function BackLink({ to, resource, onClick, "data-testid": testId = "form-back-link" }: BackLinkProps) {
  const theme = useTheme();
  return (
    <Link
      to={to}
      onClick={onClick}
      data-testid={testId}
      css={{
        ...linkInk(theme),
        display: "inline-flex",
        alignItems: "center",
        gap: theme.space(1),
        marginBottom: theme.space(2),
        fontSize: 13,
      }}
    >
      <ArrowLeft size={14} aria-hidden />
      {resource}
    </Link>
  );
}

/**
 * The breadcrumb for a form that may have been opened from somewhere other than its
 * list: it names and returns to that page, stepping back through history, and falls
 * back to the list when the form was visited directly.
 */
export function ReturnBackLink({ back }: { back: ReturnType<typeof useGoBack> }) {
  return (
    <BackLink
      to={back.to}
      resource={back.label}
      onClick={(event) => {
        if (!back.isReturn) return;
        event.preventDefault();
        back.goBack();
      }}
    />
  );
}
