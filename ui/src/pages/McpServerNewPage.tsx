import { useMemo, useState } from "react";
import {
  Checkbox,
  Form,
  Input,
  Segmented,
  Space,
  Tooltip,
  Typography,
} from "antd";
import { useTheme } from "@emotion/react";
import { useNavigate } from "react-router-dom";
import { Info } from "lucide-react";
import { PageFrame } from "@/components/Structure/PageFrame";
import { SubmitError } from "@/components/common/SubmitError";
import { BackLink } from "@/components/form/BackLink";
import { FormActions } from "@/components/form/FormActions";
import { FormField } from "@/components/form/FormField";
import { NamespaceSelect } from "@/components/form/NamespaceSelect";
import { toastSaved } from "@/components/form/saveToast";
import { UnsavedChangesModal } from "@/components/form/UnsavedChangesModal";
import { useSubmitValidation } from "@/components/form/useSubmitValidation";
import { useUnsavedChangesGuard } from "@/components/form/useUnsavedChangesGuard";
import {
  type McpServerFormValues,
  emptyMcpServerForm,
  schemeFor,
  stripScheme,
  suggestName,
  toCreateRequest,
  validateMcpServerForm,
} from "@/components/mcp/mcpServerRequest";
import { paths } from "@/router/routes";
import { apiClient, useInvalidateMcpServers } from "@/api";

const { Text } = Typography;

export function McpServerNewPage() {
  const theme = useTheme();
  const [values, setValues] = useState<McpServerFormValues>(emptyMcpServerForm);
  // Once the user names the server themselves, the suggestion stops overwriting
  // it — a name that keeps changing under the cursor is worse than no help.
  const [nameTouched, setNameTouched] = useState(false);
  const [baseline] = useState(() => JSON.stringify(values));
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  const navigate = useNavigate();
  const invalidateServers = useInvalidateMcpServers();

  const set = <K extends keyof McpServerFormValues>(
    key: K,
    value: McpServerFormValues[K],
  ) => {
    setValues((current) => {
      const next = { ...current, [key]: value };
      if (!nameTouched) next.name = suggestName(next);
      return next;
    });
    // A previous failure described values that no longer hold.
    setFailure(undefined);
  };

  /**
   * The name field writes itself, bypassing the suggestion above.
   *
   * Going through `set` would re-derive the name from the URL on the very
   * keystroke that marks it as touched — `setNameTouched(true)` has not applied
   * yet when the updater reads the flag, so the first character the user types
   * gets replaced by the suggestion and the field appears to reject typing.
   */
  const setName = (value: string) => {
    setNameTouched(true);
    setValues((current) => ({ ...current, name: value }));
    setFailure(undefined);
  };

  /**
   * The URL field, which holds the host without a scheme.
   *
   * The scheme is shown as a fixed prefix driven by the TLS toggle, so a scheme
   * pasted into the box is stripped rather than stored — and a pasted `https://`
   * or `http://` also flips the toggle to match, so pasting a full URL sets both
   * halves the way the reader plainly meant.
   */
  const setUrl = (raw: string) => {
    const trimmed = raw.trim();
    const wantHttps = /^https:\/\//i.test(trimmed);
    const wantHttp = !wantHttps && /^http:\/\//i.test(trimmed);
    setValues((current) => {
      const next = { ...current, url: stripScheme(raw) };
      if (wantHttps) next.tlsEnabled = true;
      else if (wantHttp) next.tlsEnabled = false;
      if (!nameTouched) next.name = suggestName(next);
      return next;
    });
    setFailure(undefined);
  };

  const validation = useSubmitValidation(useMemo(() => validateMcpServerForm(values), [values]));

  const isDirty = JSON.stringify(values) !== baseline;
  const { blocker, release } = useUnsavedChangesGuard(isDirty);

  const submit = async () => {
    setFailure(undefined);
    if (!validation.attempt()) return;

    setSaving(true);
    try {
      await apiClient.mcpServers.create(toCreateRequest(values));
      // Refreshes any list still on screen; SWR does not fetch a key with no mounted
      // subscriber, so the one navigated to re-reads on mount. Guarded, not load-bearing.
      await invalidateServers().catch(() => {});
      toastSaved("created", "MCP server", values.name.trim());
      release();
      await navigate(paths.mcpServers);
    } catch (error) {
      setFailure(error);
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
    <BackLink to={paths.mcpServers} resource="MCP servers" />
    <PageFrame
      title="New MCP server"
      description="Register a tool server so agents can call the tools it exposes."
    >
      <Space
        orientation="vertical"
        size="middle"
        css={{ display: "flex", maxWidth: 720 }}
      >
        <Text css={{ color: theme.color.textMuted }}>
          The controller connects to an MCP endpoint you already host.
        </Text>

        <Form layout="vertical" component="div">
          <FormField
            label="Name"
            required
            {...validation.field("name")}
            help="Filled in from the URL. You can change it."
          >
            <Input
              data-testid="mcp-name"
              placeholder="my-tool-server"
              value={values.name}
              onChange={(event) => setName(event.target.value)}
            />
          </FormField>

          <FormField
            label="Namespace"
            required
            {...validation.field("namespace")}
            help="Pick the namespace to register the server in."
          >
            <NamespaceSelect
              data-testid="mcp-namespace"
              value={values.namespace}
              onChange={(namespace) => set("namespace", namespace)}
            />
          </FormField>

          <FormField
            label="Server URL"
            required
            {...validation.field("url")}
            help="The MCP endpoint the controller will connect to. The scheme comes from the TLS setting below."
          >
            {/* The scheme is a fixed prefix the TLS toggle writes, so what is on screen
                reads as the one URL the form submits. */}
            <Input
              data-testid="mcp-url"
              prefix={
                <Text type="secondary" css={{ whiteSpace: "nowrap" }} data-testid="mcp-url-scheme">
                  {schemeFor(values.tlsEnabled)}
                </Text>
              }
              placeholder="mcp.example.com/sse"
              value={values.url}
              onChange={(event) => setUrl(event.target.value)}
            />
          </FormField>

          {/* TLS is its own control rather than a hand-typed scheme: it drives the
              prefix above. */}
          <FormField
            label="TLS"
            required={false}
            help="Whether the controller dials the upstream over TLS. HTTPS verifies the upstream's certificate; HTTP is plaintext."
          >
            <Segmented<string>
              data-testid="mcp-tls"
              value={values.tlsEnabled ? "https" : "http"}
              onChange={(value) => set("tlsEnabled", value === "https")}
              options={[
                { label: "HTTP", value: "http" },
                { label: "HTTPS", value: "https" },
              ]}
            />
          </FormField>

          <Form.Item>
            <Space size={6}>
              <Checkbox
                data-testid="mcp-streamable"
                checked={values.streamableHttp}
                onChange={(event) => set("streamableHttp", event.target.checked)}
              >
                Use streamable HTTP instead of SSE
              </Checkbox>
              {/* Named as an example rather than a list, which would go stale silently. */}
              <Tooltip title="Some harnesses, like Codex, only accept streamable HTTP.">
                <Info
                  size={14}
                  data-testid="mcp-streamable-info"
                  aria-label="Some harnesses, like Codex, only accept streamable HTTP."
                  tabIndex={0}
                  css={{
                    display: "block",
                    color: theme.color.textMuted,
                    cursor: "help",
                    "&:hover, &:focus-visible": { color: theme.color.text },
                  }}
                />
              </Tooltip>
            </Space>
          </Form.Item>

          <FormField
            label="Headers sent to the server"
            required={false}
            {...validation.field("headersJson")}
            help="A JSON object. Leave empty to send none."
          >
            <Input.TextArea
              data-testid="mcp-headers"
              placeholder='{"Authorization": "Bearer …"}'
              value={values.headersJson}
              onChange={(event) => set("headersJson", event.target.value)}
              autoSize={{ minRows: 2, maxRows: 8 }}
              css={{ fontFamily: theme.font.mono }}
            />
          </FormField>

          <FormField label="Connection timeout" required={false} help="Time allowed to connect, like 5s or 1m.">
            <Input
              data-testid="mcp-timeout"
              value={values.timeout}
              onChange={(event) => set("timeout", event.target.value)}
            />
          </FormField>

          {/* Only SSE has a read timeout, so the field disappears when streamable
              HTTP is chosen. */}
          {!values.streamableHttp ? (
            <FormField label="SSE read timeout" required={false} help="How long to wait for data on the stream, like 300s.">
              <Input
                data-testid="mcp-sse-timeout"
                value={values.sseReadTimeout}
                onChange={(event) => set("sseReadTimeout", event.target.value)}
              />
            </FormField>
          ) : null}

          <Form.Item>
            <Checkbox
              data-testid="mcp-terminate"
              checked={values.terminateOnClose}
              onChange={(event) => set("terminateOnClose", event.target.checked)}
            >
              Terminate the connection when the session closes
            </Checkbox>
          </Form.Item>
        </Form>

        <FormActions
          submitLabel="Create"
          onSubmit={() => void submit()}
          submitting={saving}
          cancel={{ to: paths.mcpServers }}
          testIdPrefix="mcp"
          blocked={!validation.valid}
          error={
            failure !== undefined ? (
              <SubmitError
                data-testid="mcp-submit-error"
                error={failure}
                what="MCP server"
                onRetry={() => void submit()}
              />
            ) : null
          }
        />

        <UnsavedChangesModal blocker={blocker} testIdPrefix="mcp">
          This form has edits that have not been saved. Leaving throws them away.
        </UnsavedChangesModal>
      </Space>
    </PageFrame>
    </>
  );
}
