import { useMemo, useState } from "react";
import {
  Alert,
  Button,
  Checkbox,
  Form,
  Input,
  Segmented,
  Space,
  Tooltip,
  Typography,
} from "antd";
import { useTheme } from "@emotion/react";
import { Link, useNavigate } from "react-router-dom";
import { Info } from "lucide-react";
import { PageFrame } from "@/components/Structure/PageFrame";
import { CredentialHeadersEditor } from "@/components/common/CredentialHeadersEditor";
import { SubmitError } from "@/components/common/SubmitError";
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
  const [submitted, setSubmitted] = useState(false);
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

  const issues = useMemo(() => validateMcpServerForm(values), [values]);
  const issueFor = (field: keyof McpServerFormValues) =>
    submitted ? issues.find((issue) => issue.field === field)?.message : undefined;

  const submit = async () => {
    setSubmitted(true);
    setFailure(undefined);
    if (issues.length > 0) return;

    setSaving(true);
    try {
      await apiClient.mcpServers.create(toCreateRequest(values));
      // Refreshes any list still on screen; SWR does not fetch a key with no mounted
      // subscriber, so the one navigated to re-reads on mount. Guarded, not load-bearing.
      await invalidateServers().catch(() => {});
      // Straight to the list, which is where the new server can actually be
      // seen — a success message on a form the user is still looking at proves
      // less than the row itself.
      await navigate(paths.mcpServers);
    } catch (error) {
      setFailure(error);
    } finally {
      setSaving(false);
    }
  };

  return (
    <PageFrame
      title="New MCP server"
      description="Register a tool server so agents can call the tools it exposes."
      actions={
        <Link to={paths.mcpServers}>
          <Button>Back to servers</Button>
        </Link>
      }
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
          <Form.Item
            label="Name"
            required
            validateStatus={issueFor("name") ? "error" : undefined}
            help={issueFor("name")}
          >
            <Input
              data-testid="mcp-name"
              placeholder="my-tool-server"
              value={values.name}
              onChange={(event) => setName(event.target.value)}
            />
          </Form.Item>

          <Form.Item
            label="Namespace"
            validateStatus={issueFor("namespace") ? "error" : undefined}
            help={issueFor("namespace")}
          >
            <Input
              data-testid="mcp-namespace"
              placeholder="kagent"
              value={values.namespace}
              onChange={(event) => set("namespace", event.target.value)}
            />
          </Form.Item>

              <Form.Item
                label="Server URL"
                required
                validateStatus={issueFor("url") ? "error" : undefined}
                help={
                  issueFor("url") ??
                  "The MCP endpoint the controller will connect to. The scheme comes from the TLS setting below."
                }
              >
                {/* The scheme is an affix rather than an addon: `addonBefore`
                    is deprecated in favour of `Space.Compact`, which wants a
                    control beside the field, and this is not one — it is a fixed
                    label the TLS toggle writes and the reader cannot edit.
                    `prefix` renders it inside the field, so what is on screen
                    reads as the one URL the form submits. */}
                <Input
                  data-testid="mcp-url"
                  prefix={
                    <Text
                      type="secondary"
                      css={{ whiteSpace: "nowrap" }}
                      data-testid="mcp-url-scheme"
                    >
                      {schemeFor(values.tlsEnabled)}
                    </Text>
                  }
                  placeholder="mcp.example.com/sse"
                  value={values.url}
                  onChange={(event) => setUrl(event.target.value)}
                />
              </Form.Item>

              {/* TLS as its own control, not a scheme the reader hand-types: the
                  controller derives the effective scheme from whether spec.tls is
                  present, so the toggle is the single source of that intent and it
                  drives the grayed prefix above and the CA field below. */}
              <Form.Item
                label="TLS"
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
              </Form.Item>

              <Form.Item>
                <Space size={6}>
                  <Checkbox
                    data-testid="mcp-streamable"
                    checked={values.streamableHttp}
                    onChange={(event) => set("streamableHttp", event.target.checked)}
                  >
                    Use streamable HTTP instead of SSE
                  </Checkbox>
                  {/* Named as an example rather than a list: which harnesses refuse SSE
                      is theirs to change, and a list here would go stale silently. */}
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

              <Form.Item label="Credential headers" help={issueFor("credentialHeaders")} validateStatus={issueFor("credentialHeaders") ? "error" : undefined}>
                <CredentialHeadersEditor value={values.credentialHeaders} onChange={(headers) => set("credentialHeaders", headers)} />
              </Form.Item>
              <Form.Item label="Connection timeout">
                <Input
                  data-testid="mcp-timeout"
                  value={values.timeout}
                  onChange={(event) => set("timeout", event.target.value)}
                />
              </Form.Item>

              {/* Only SSE has a read timeout, so the field disappears rather than
                  sitting there inert when streamable HTTP is chosen. */}
              {!values.streamableHttp ? (
                <Form.Item label="SSE read timeout">
                  <Input
                    data-testid="mcp-sse-timeout"
                    value={values.sseReadTimeout}
                    onChange={(event) => set("sseReadTimeout", event.target.value)}
                  />
                </Form.Item>
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

        {submitted && issues.length > 0 ? (
          <Alert
            type="error"
            showIcon
            title="This server cannot be created yet"
            data-testid="mcp-form-errors"
            description={
              <ul css={{ margin: 0, paddingLeft: theme.space(5) }}>
                {issues.map((issue) => (
                  <li key={`${issue.field}-${issue.message}`}>{issue.message}</li>
                ))}
              </ul>
            }
          />
        ) : null}

        {failure !== undefined ? (
          <SubmitError
            data-testid="mcp-submit-error"
            error={failure}
            what="MCP server"
            onRetry={() => void submit()}
          />
        ) : null}

        <Space size={8}>
          <Button
            type="primary"
            data-testid="mcp-submit"
            loading={saving}
            onClick={() => void submit()}
          >
            Create server
          </Button>
          <Link to={paths.mcpServers}>
            <Button>Cancel</Button>
          </Link>
        </Space>
      </Space>
    </PageFrame>
  );
}
