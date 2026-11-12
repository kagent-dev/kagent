import { Alert } from "antd";

/** Why a copy form is blank, or that a copy source arriving late was left unapplied. */
export function CopyNotice({
  kind,
  from,
  error,
  notFound = false,
  loaded,
  mountedBlank,
  testId,
}: {
  kind: string;
  from?: { namespace: string; name: string };
  error?: string;
  /** The source was looked for and is not there. */
  notFound?: boolean;
  loaded: boolean;
  mountedBlank: boolean;
  testId: string;
}) {
  const css = { marginBottom: 16 };
  if (notFound)
    return (
      <Alert
        type="warning"
        showIcon
        title={`${kind.charAt(0).toUpperCase()}${kind.slice(1)} not found`}
        description={`No ${kind} ${from?.namespace}/${from?.name} to copy. Starting from a blank form.`}
        data-testid={`${testId}-not-found`}
        css={css}
      />
    );
  if (mountedBlank && loaded)
    return (
      <Alert
        type="warning"
        showIcon
        title={`The ${kind} to copy has loaded`}
        description={`Copy source ${from?.namespace}/${from?.name} has loaded, but it was not applied so your edits are kept. Reload the page to start from the copy.`}
        data-testid={`${testId}-late`}
        css={css}
      />
    );
  if (error && !loaded)
    return (
      <Alert
        type="error"
        showIcon
        title={`Could not load the ${kind} to copy`}
        description={`${error} Starting from a blank form.`}
        data-testid={testId}
        css={css}
      />
    );
  return null;
}
