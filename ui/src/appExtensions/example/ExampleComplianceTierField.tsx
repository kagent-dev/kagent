import { Select, Typography } from "antd";
import type { ExtensionFormFieldProps } from "@/appExtensions";
import { TIERS } from "./exampleComplianceTier";
import type { ComplianceTierValue } from "./exampleComplianceTier";

const { Text } = Typography;

/** The field's renderer, supplied whole by the extension. */
export function ExampleComplianceTierField({
  id,
  value,
  onChange,
  error,
  disabled,
}: ExtensionFormFieldProps<ComplianceTierValue>) {
  return (
    <label htmlFor={id} css={{ display: "block" }}>
      <Text css={{ display: "block", marginBottom: 6 }}>
        Example compliance tier
      </Text>
      <Select
        id={id}
        // Empty means unanswered, which antd shows as the placeholder rather
        // than as a selected blank option.
        value={value === "" ? undefined : value}
        placeholder="Select a tier"
        disabled={disabled}
        onChange={onChange}
        status={error ? "error" : undefined}
        data-testid="example-compliance-tier"
        css={{ width: 240 }}
        options={TIERS.map((tier) => ({ value: tier, label: tier }))}
      />
      {error ? (
        <Text type="danger" css={{ display: "block", marginTop: 4 }}>
          {error}
        </Text>
      ) : null}
    </label>
  );
}
