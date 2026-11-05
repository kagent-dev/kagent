import { Button, Card, Form, Input, Radio, Select, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Trash2 } from "lucide-react";
import { isWildcardOrigin } from "@/api";
import { egressCopy } from "./egressPolicyCopy";
import type { AuthChoice, DestinationDraft } from "./egressPolicyDraft";

const { Text } = Typography;

// region Component

/** One destination: an origin and optional authentication. */
export function DestinationEditor({
  index,
  row,
  problems,
  onChange,
  onRemove,
  disabled,
}: {
  index: number;
  row: DestinationDraft;
  problems: string[] | undefined;
  onChange: (next: DestinationDraft) => void;
  onRemove?: () => void;
  disabled?: boolean;
}) {
  const theme = useTheme();
  const update = (patch: Partial<DestinationDraft>) => onChange({ ...row, ...patch });
  const wildcard = isWildcardOrigin(row.origin);
  const testId = (field: string) => `egress-destination-${index}-${field}`;

  return (
    <Card
      size="small"
      data-testid={`egress-destination-${index}`}
      title={egressCopy.form.destinationTitle(index + 1)}
      extra={
        onRemove ? (
          <Button
            type="text"
            danger
            size="small"
            icon={<Trash2 size={14} />}
            onClick={onRemove}
            disabled={disabled}
            aria-label={egressCopy.form.removeDestination(index + 1)}
            data-testid={testId("remove")}
          />
        ) : null
      }
    >
      <Form layout="vertical" component="div">
        <Form.Item label={egressCopy.form.origin} required extra={egressCopy.form.originHelp}>
          <Input
            data-testid={testId("origin")}
            value={row.origin}
            placeholder="https://api.example.com"
            disabled={disabled}
            onChange={(event) => update({ origin: event.target.value })}
            css={{ fontFamily: theme.font.mono }}
          />
        </Form.Item>

        <Form.Item
          label={egressCopy.form.authentication}
          extra={wildcard ? egressCopy.form.wildcardNoAuth : egressCopy.form.authenticationHelp}
        >
          <Radio.Group
            optionType="button"
            data-testid={testId("auth")}
            value={row.auth}
            disabled={disabled}
            onChange={(event) => update({ auth: event.target.value as AuthChoice })}
            options={[
              { value: "none", label: egressCopy.auth.none },
              { value: "secret", label: egressCopy.auth.secret, disabled: wildcard && row.auth !== "secret" },
              { value: "actorJwt", label: egressCopy.auth.actorJwt, disabled: wildcard && row.auth !== "actorJwt" },
            ]}
          />
        </Form.Item>

        {row.auth !== "none" ? (
          <div css={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", columnGap: theme.space(4) }}>
            <Form.Item label={egressCopy.form.header} required>
              <Input
                data-testid={testId("header")}
                value={row.header}
                placeholder="Authorization"
                disabled={disabled}
                onChange={(event) => update({ header: event.target.value })}
                css={{ fontFamily: theme.font.mono }}
              />
            </Form.Item>
            <Form.Item label={egressCopy.form.prefix} extra={egressCopy.form.prefixHelp}>
              <Input
                data-testid={testId("prefix")}
                value={row.prefix}
                placeholder="Bearer "
                disabled={disabled}
                onChange={(event) => update({ prefix: event.target.value })}
                css={{ fontFamily: theme.font.mono }}
              />
            </Form.Item>
            {row.auth === "secret" ? (
              <>
                <Form.Item label={egressCopy.form.secretName} required extra={egressCopy.form.secretHelp}>
                  <Input
                    data-testid={testId("secret-name")}
                    value={row.secretName}
                    placeholder="api-credentials"
                    disabled={disabled}
                    onChange={(event) => update({ secretName: event.target.value })}
                  />
                </Form.Item>
                <Form.Item label={egressCopy.form.secretKey} required>
                  <Input
                    data-testid={testId("secret-key")}
                    value={row.secretKey}
                    placeholder="token"
                    disabled={disabled}
                    onChange={(event) => update({ secretKey: event.target.value })}
                  />
                </Form.Item>
              </>
            ) : (
              <>
                <Form.Item label={egressCopy.form.audiences} required extra={egressCopy.form.audiencesHelp}>
                  <Select
                    mode="tags"
                    data-testid={testId("audiences")}
                    value={row.audiences}
                    disabled={disabled}
                    open={false}
                    tokenSeparators={[",", " "]}
                    placeholder="https://api.example.com"
                    onChange={(audiences: string[]) => update({ audiences })}
                    suffixIcon={null}
                  />
                </Form.Item>
                <Form.Item label={egressCopy.form.lifetime} extra={egressCopy.form.lifetimeHelp}>
                  <Input
                    data-testid={testId("lifetime")}
                    value={row.expirationSeconds}
                    inputMode="numeric"
                    placeholder="900"
                    disabled={disabled}
                    onChange={(event) => update({ expirationSeconds: event.target.value })}
                  />
                </Form.Item>
              </>
            )}
          </div>
        ) : null}
      </Form>

      {problems?.length ? (
        <ul
          data-testid={testId("problems")}
          role="alert"
          css={{ margin: 0, paddingLeft: theme.space(5), color: theme.color.dangerText }}
        >
          {problems.map((problem) => (
            <li key={problem}>
              <Text css={{ color: "inherit" }}>{problem}</Text>
            </li>
          ))}
        </ul>
      ) : null}
    </Card>
  );
}
