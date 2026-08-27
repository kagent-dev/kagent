import { useState } from "react";
import { Card, Typography } from "antd";
import { css, useTheme } from "@emotion/react";
import { PageFrame } from "@/components/Structure/PageFrame";
import {
  applyExtensionFieldValues,
  initialExtensionFieldValues,
  useExtensionFormFields,
  validateExtensionFieldValues,
} from "@/appExtensions";
import { useExampleTenant } from "./exampleTenant";

const { Paragraph, Text } = Typography;

/** The payload a core form would be building before contributed fields fold in. */
const basePayload = {
  apiVersion: "kagent.dev/v1alpha2",
  kind: "Agent",
  metadata: { name: "example-agent", namespace: "default" },
};

/**
 * A whole page contributed by the extension and merged into the router.
 *
 * One card, because one card is all it needs. The page exists to make the
 * form-field contract observable — the field renders here exactly as it does in
 * the core form, and the payload updates as its value maps into the request body,
 * which is the half of that contract nothing else on screen would show. The
 * workspace line under the title is the app-level provider proving it ran.
 */
export function ExamplePage() {
  const theme = useTheme();
  const tenant = useExampleTenant();
  const fields = useExtensionFormFields("app_agents_agentNew_agentForm");
  const [values, setValues] = useState(() => initialExtensionFieldValues(fields));

  const errors = validateExtensionFieldValues(fields, values);
  const payload = applyExtensionFieldValues(fields, basePayload, values);

  return (
    <PageFrame
      title="Example"
      description="A page contributed by the Example App Extension."
    >
      <div
        css={css`
          display: grid;
          gap: ${theme.space(5)};
          max-width: 760px;
        `}
        data-testid="example-page"
      >
        <Text data-testid="example-tenant" css={{ color: theme.color.textMuted }}>
          Workspace <strong>{tenant.tenantId}</strong> on the {tenant.plan} plan,
          read from a React context the extension installed itself.
        </Text>

        <Card size="small" title="A field this extension adds to the new-agent form">
          <Paragraph css={{ color: theme.color.textMuted }}>
            Choosing a value rewrites the request the form would send. The field
            has no idea where its value lands — that is the mapper's job.
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

          <pre
            data-testid="example-payload-preview"
            css={css`
              margin: ${theme.space(4)} 0 0;
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
