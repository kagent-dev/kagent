import { Select, Typography } from "antd";
import type { ExtensionFormFieldProps } from "@/appExtensions";
import { TEAMS } from "./exampleTeam";
import type { TeamValue } from "./exampleTeam";

const { Text } = Typography;

/** The field's renderer, supplied whole by the extension. */
export function ExampleTeamField({
  id,
  value,
  onChange,
  error,
  disabled,
}: ExtensionFormFieldProps<TeamValue>) {
  return (
    <label htmlFor={id} css={{ display: "block" }}>
      <Text css={{ display: "block", marginBottom: 6 }}>Example team</Text>
      <Select
        id={id}
        // Empty means unanswered, which antd shows as the placeholder rather
        // than as a selected blank option.
        value={value === "" ? undefined : value}
        placeholder="Select a team"
        disabled={disabled}
        onChange={onChange}
        status={error ? "error" : undefined}
        data-testid="example-team"
        css={{ width: 240 }}
        options={TEAMS.map((team) => ({ value: team, label: team }))}
      />
      {error ? (
        <Text type="danger" css={{ display: "block", marginTop: 4 }}>
          {error}
        </Text>
      ) : null}
    </label>
  );
}
