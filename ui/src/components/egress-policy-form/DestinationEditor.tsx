import type { Ref } from "react";
import { Form, Input, Radio, type InputRef } from "antd";
import { useTheme } from "@emotion/react";
import { isWildcardOrigin } from "@/api";
import { FormField } from "@/components/form/FormField";
import { ListInput } from "@/components/form/ListInput";
import { FieldRow } from "@/components/form/FieldRow";
import { OptionPanel } from "@/components/form/OptionPanel";
import type { FieldValidation } from "@/components/form/useSubmitValidation";
import { egressCopy } from "./egressPolicyCopy";
import { destinationFieldKey, type AuthChoice, type DestinationDraft, type DestinationField } from "./egressPolicyDraft";

/** The fields of one destination: an origin and optional authentication. */
export function DestinationEditor({
  index,
  row,
  fieldFor,
  onChange,
  disabled,
  originRef,
}: {
  index: number;
  row: DestinationDraft;
  /** The inline message for one of this row's fields, once the form was submitted. */
  fieldFor: (key: string) => FieldValidation;
  onChange: (next: DestinationDraft) => void;
  disabled?: boolean;
  originRef?: Ref<InputRef>;
}) {
  const theme = useTheme();
  const update = (patch: Partial<DestinationDraft>) => onChange({ ...row, ...patch });
  const wildcard = isWildcardOrigin(row.origin);
  const testId = (field: string) => `egress-destination-${index}-${field}`;
  // Keyed on the row id, not the index, so an id stays with its row after a removal.
  const fieldId = (field: string) => `egress-destination-${row.id}-${field}-input`;
  const field = (name: DestinationField) => fieldFor(destinationFieldKey(row.id, name));
  const error = (name: DestinationField) => field(name).error;
  const mono = { fontFamily: theme.font.mono };

  return (
    <Form layout="vertical" component="div">
      <FormField
        htmlFor={fieldId("origin")}
        label={egressCopy.form.origin}
        required
        help={egressCopy.form.originHelp}
        {...field("origin")}
      >
        <Input
          id={fieldId("origin")}
          ref={originRef}
          data-testid={testId("origin")}
          value={row.origin}
          placeholder="https://api.example.com"
          disabled={disabled}
          status={error("origin") ? "error" : undefined}
          onChange={(event) => update({ origin: event.target.value })}
          css={mono}
        />
      </FormField>

      <FormField
        htmlFor={fieldId("auth")}
        label={egressCopy.form.authentication}
        required={false}
        {...field("auth")}
        help={wildcard ? egressCopy.form.wildcardNoAuth : egressCopy.form.authenticationHelp}
      >
        <Radio.Group
          optionType="button"
          id={fieldId("auth")}
          aria-label={egressCopy.form.authentication}
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
      </FormField>

      {row.auth !== "none" ? (
        <OptionPanel label={egressCopy.form.authSettings(egressCopy.auth[row.auth])} data-testid={testId("auth-panel")}>
          <FormField
            htmlFor={fieldId("header")}
            label={egressCopy.form.header}
            required
            {...field("header")}
            help={egressCopy.form.headerHelp}
          >
            <Input
              id={fieldId("header")}
              data-testid={testId("header")}
              value={row.header}
              placeholder="Authorization"
              disabled={disabled}
              status={error("header") ? "error" : undefined}
              onChange={(event) => update({ header: event.target.value })}
              css={mono}
            />
          </FormField>
          <FormField
            htmlFor={fieldId("prefix")}
            label={egressCopy.form.prefix}
            required={false}
            {...field("prefix")}
            help={egressCopy.form.prefixHelp}
          >
            <Input
              id={fieldId("prefix")}
              data-testid={testId("prefix")}
              value={row.prefix}
              placeholder="Bearer "
              disabled={disabled}
              status={error("prefix") ? "error" : undefined}
              onChange={(event) => update({ prefix: event.target.value })}
              css={mono}
            />
          </FormField>
          {row.auth === "secret" ? (
            <>
              <FormField
                htmlFor={fieldId("secret-name")}
                label={egressCopy.form.secretName}
                required
                {...field("secretName")}
                help={egressCopy.form.secretHelp}
              >
                <Input
                  id={fieldId("secret-name")}
                  data-testid={testId("secret-name")}
                  value={row.secretName}
                  placeholder="api-credentials"
                  disabled={disabled}
                  status={error("secretName") ? "error" : undefined}
                  onChange={(event) => update({ secretName: event.target.value })}
                />
              </FormField>
              <FormField
                htmlFor={fieldId("secret-key")}
                label={egressCopy.form.secretKey}
                required
                {...field("secretKey")}
                help={egressCopy.form.secretKeyHelp}
              >
                <Input
                  id={fieldId("secret-key")}
                  data-testid={testId("secret-key")}
                  value={row.secretKey}
                  placeholder="token"
                  disabled={disabled}
                  status={error("secretKey") ? "error" : undefined}
                  onChange={(event) => update({ secretKey: event.target.value })}
                />
              </FormField>
            </>
          ) : (
            <>
              <div css={{ gridColumn: "1 / -1" }}>
<FieldRow columns="minmax(0, 2fr) minmax(0, 1fr)">
              <FormField
                                htmlFor={fieldId("audiences")}
                label={egressCopy.form.audiences}
                required
                {...field("audiences")}
                help={egressCopy.form.audiencesHelp}
              >
                <ListInput
                  id={fieldId("audiences")}
                  data-testid={testId("audiences")}
                  split="commasAndSpaces"
                  unique
                  value={row.audiences}
                  disabled={disabled}
                  placeholder="https://api.example.com"
                  onChange={(audiences) => update({ audiences })}
                />
              </FormField>
              <FormField
                                htmlFor={fieldId("lifetime")}
                label={egressCopy.form.lifetime}
                required={false}
                {...field("lifetime")}
                help={egressCopy.form.lifetimeHelp}
              >
                <Input
                  id={fieldId("lifetime")}
                  data-testid={testId("lifetime")}
                  value={row.expirationSeconds}
                  inputMode="numeric"
                  placeholder="900"
                  disabled={disabled}
                  status={error("lifetime") ? "error" : undefined}
                  onChange={(event) => update({ expirationSeconds: event.target.value })}
                />
              </FormField>
              </FieldRow>
              </div>
            </>
          )}
        </OptionPanel>
      ) : null}
    </Form>
  );
}
