import { Alert } from "antd";

/** Why a copy form is blank, or that a copy source arriving late was left unapplied. */
export function CopyNotice({
  kind,
  from,
  error,
  loaded,
  mountedBlank,
  testId,
}: {
  kind: string;
  from?: { namespace: string; name: string };
  error?: string;
  loaded: boolean;
  mountedBlank: boolean;
  testId: string;
}) {
  const css = { marginBottom: 16 };
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
