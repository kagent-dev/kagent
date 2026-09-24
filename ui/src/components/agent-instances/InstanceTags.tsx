import { Tag, Tooltip, Typography } from "antd";
import { useTheme } from "@emotion/react";
import type { AgentInstanceOperation, AgentInstanceState } from "@/api";
import {
  NOT_REPORTED,
  operationAppearance,
  stateAppearance,
} from "./instanceLabels";

const { Text } = Typography;

/**
 * An instance's state, as the one control a reader looks at first.
 *
 * The tooltip carries the explanation rather than the page repeating it beside
 * every row: in a table of eight instances the sentence would be eight sentences,
 * and the detail page shows it in full anyway.
 */
export function StateTag({
  state,
  testId,
}: {
  state: AgentInstanceState;
  testId?: string;
}) {
  const { label, tone, meaning } = stateAppearance(state);

  return (
    <Tooltip title={meaning}>
      {/* The raw enum value rides along as an attribute so a test can assert on the
          state itself rather than on its wording — the wording is allowed to change,
          the state is not. */}
      <Tag color={tone} data-testid={testId} data-state={state}>
        {label}
      </Tag>
    </Tooltip>
  );
}

/**
 * What the controller is doing to an instance right now, if anything.
 *
 * Nothing in flight is rendered as muted text rather than as a tag, because a tag
 * reading "None in progress" on every healthy row is a column of noise — the eye
 * should be drawn to the rows where something *is* happening.
 */
export function OperationTag({
  operation,
  testId,
}: {
  operation: AgentInstanceOperation;
  testId?: string;
}) {
  const theme = useTheme();
  const { label, tone, inProgress } = operationAppearance(operation);

  if (!inProgress) {
    return (
      <Text
        css={{ color: theme.color.textMuted, fontSize: 12 }}
        data-testid={testId}
        data-operation={operation}
      >
        {label}
      </Text>
    );
  }

  return (
    <Tag color={tone} data-testid={testId} data-operation={operation}>
      {label}
    </Tag>
  );
}

/**
 * A value the API did not send, said out loud.
 *
 * Muted, because it is the absence of information rather than information — but
 * present, which an em dash is not. Every blank this feature could render goes
 * through here or through `orNotReported`, so "the cluster said nothing" always
 * looks the same and never looks like a rendering failure.
 */
export function NotReported({ testId }: { testId?: string }) {
  const theme = useTheme();

  return (
    <Text
      css={{ color: theme.color.textMuted, fontStyle: "italic" }}
      data-testid={testId}
      data-not-reported="true"
    >
      {NOT_REPORTED}
    </Text>
  );
}

/** A value if there is one, and the standard wording if there is not. */
export function ValueOrNotReported({
  value,
  mono = false,
  testId,
}: {
  value: string | undefined;
  /** For identifiers and refs, which are read character by character. */
  mono?: boolean;
  testId?: string;
}) {
  const theme = useTheme();

  if (!value || value.trim() === "") return <NotReported testId={testId} />;

  return (
    <Text
      css={mono ? { fontFamily: theme.font.mono, fontSize: 12 } : undefined}
      data-testid={testId}
    >
      {value}
    </Text>
  );
}
