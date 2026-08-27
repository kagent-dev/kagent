import { useState } from "react";
import { Card, Descriptions, Typography } from "antd";
import { css, useTheme } from "@emotion/react";
import { PageFrame } from "@/components/Structure/PageFrame";
import {
  applyVendorFieldValues,
  initialVendorFieldValues,
  useVendorFormFields,
  validateVendorFieldValues,
} from "@/vendorExtensions";
import { useExampleTenant } from "./exampleTenant";

const { Paragraph } = Typography;

/** The payload a core form would be building before vendor fields fold in. */
const basePayload = {
  apiVersion: "kagent.dev/v1alpha2",
  kind: "Agent",
  metadata: { name: "example-agent", namespace: "default" },
};

/**
 * A whole page contributed by the extension and merged into the router.
 *
 * It doubles as the live proof for two capabilities that have no visible home
 * until the core pages are rebuilt: the app-level provider (the tenant strip
 * reads from a context the extension itself installed) and the form-field
 * contract (the field renders, and the payload preview updates as its value
 * maps into the request body).
 */
export function ExampleInsightsPage() {
  const theme = useTheme();
  const tenant = useExampleTenant();
  const fields = useVendorFormFields("app_agents_agentNew_agentForm");
  const [values, setValues] = useState(() => initialVendorFieldValues(fields));

  const errors = validateVendorFieldValues(fields, values);
  const payload = applyVendorFieldValues(fields, basePayload, values);

  return (
    <PageFrame
      title="Example Insights"
      description="A page injected by the Example vendor extension through the global config."
    >
      <div
        css={css`
          display: grid;
          gap: ${theme.space(5)};
          max-width: 760px;
        `}
        data-testid="example-insights-page"
      >
        <Card size="small" title="Tenant (from the extension's own provider)">
          <Descriptions
            size="small"
            column={2}
            data-testid="example-tenant"
            items={[
              { key: "tenantId", label: "Tenant", children: tenant.tenantId },
              { key: "plan", label: "Plan", children: tenant.plan },
            ]}
          />
        </Card>

        <Card size="small" title="Contributed form field">
          <Paragraph
            css={css`
              color: ${theme.color.textMuted};
            `}
          >
            The same field the extension adds to the core “new agent” form.
            Changing it rewrites the request payload below.
          </Paragraph>
          {fields.map((field) => {
            const Field = field.Component;
            return (
              <Field
                key={field.id}
                id={field.id}
                value={values[field.id]}
                error={errors[field.id]}
                disabled={false}
                onChange={(next) =>
                  setValues((current) => ({ ...current, [field.id]: next }))
                }
              />
            );
          })}
        </Card>

        <Card size="small" title="Resulting request payload">
          <pre
            data-testid="example-payload-preview"
            css={css`
              margin: 0;
              padding: ${theme.space(3)};
              border-radius: ${theme.radius.sm};
              background: ${theme.color.bg};
              font-family: ${theme.font.mono};
              font-size: 12px;
            `}
          >
            {JSON.stringify(payload, null, 2)}
          </pre>
        </Card>
      </div>
    </PageFrame>
  );
}
