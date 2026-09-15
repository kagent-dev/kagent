import { Button, Tooltip } from "antd";
import { Plus } from "lucide-react";
import { Link } from "react-router-dom";

/**
 * The create action a list page carries, which explains itself when the caller
 * may not use it.
 *
 * Shared by every list that offers a create, so the button and the reason for
 * refusing it are worded the same way wherever they appear. Written per page,
 * the allowed and refused states drift apart: the same markup ends up duplicated
 * once inside a `Link` and once disabled beside it.
 *
 * A bare disabled button is the state to avoid. It stops the wasted form-fill,
 * which is the point, but leaves the reader unable to tell a permission they
 * lack from a page still loading or a control that is broken — and a disabled
 * button takes no focus, so a keyboard or screen reader gets no signal at all.
 * The tooltip is what makes the refusal legible.
 */
export function CreateResourceButton({
  kind,
  to,
  allowed,
  label,
  testId,
}: {
  /** What this creates, article included: "a model configuration". */
  kind: string;
  /** Where the create form lives. */
  to: string;
  /** Whether the server reported the caller may create one. */
  allowed: boolean;
  /** The visible label, e.g. "New model". */
  label: string;
  testId: string;
}) {
  const button = (
    <Button type="primary" icon={<Plus size={14} />} data-testid={testId} disabled={!allowed}>
      {label}
    </Button>
  );

  if (!allowed) {
    return (
      <Tooltip title={`You do not have permission to create ${kind}`}>
        {/* antd disables pointer events on a disabled button, so the tooltip needs
            an enabled element of its own to sit on. */}
        <span>{button}</span>
      </Tooltip>
    );
  }
  return <Link to={to}>{button}</Link>;
}
