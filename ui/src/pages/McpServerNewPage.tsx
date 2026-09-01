import { Fragment, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Checkbox,
  Form,
  Input,
  Segmented,
  Select,
  Space,
  Typography,
  Upload,
} from "antd";
import { useTheme } from "@emotion/react";
import { Link, useNavigate } from "react-router-dom";
import { Plus, Trash2, Upload as UploadIcon } from "lucide-react";
import { PageFrame } from "@/components/Structure/PageFrame";
import { SubmitError } from "@/components/common/SubmitError";
import {
  COMMAND_EXECUTORS,
  type CommandExecutor,
  type EnvRow,
  type McpServerFormValues,
  emptyMcpServerForm,
  previewCommand,
  schemeFor,
  stripScheme,
  suggestName,
  toCreateRequest,
  validateMcpServerForm,
} from "@/components/mcp/mcpServerRequest";
import { paths } from "@/router/routes";
import { apiClient } from "@/api";

const { Text } = Typography;

let envCounter = 0;
const newEnvRow = (): EnvRow => {
  envCounter += 1;
  return { id: `env-${envCounter}`, name: "", value: "" };
};

export function McpServerNewPage() {
  const theme = useTheme();
  const [values, setValues] = useState<McpServerFormValues>(emptyMcpServerForm);
  // Once the user names the server themselves, the suggestion stops overwriting
  // it — a name that keeps changing under the cursor is worse than no help.
  const [nameTouched, setNameTouched] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<unknown>();
  // The uploaded CA file's name and any problem reading it live here; its
  // contents live in `values.caCertPem`.
  const [caCertFileName, setCaCertFileName] = useState<string>();
  const [caCertError, setCaCertError] = useState<string>();
  const navigate = useNavigate();

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

  /**
   * Loads a PEM CA bundle from the chosen file into the form.
   *
   * Read in the browser rather than uploaded: the certificate is public material
   * that only becomes a Secret when the server is created, so there is nothing to
   * send until then. Returning `false` stops antd from trying to upload it.
   */
  const readCaCert = (file: File): boolean => {
    const reader = new FileReader();
    reader.onload = () => {
      const text = String(reader.result ?? "");
      if (!text.includes("-----BEGIN CERTIFICATE-----")) {
        setCaCertError("That file is not a PEM-encoded certificate.");
        return;
      }
      setCaCertError(undefined);
      setCaCertFileName(file.name);
      set("caCertPem", text);
    };
    reader.onerror = () => setCaCertError("That file could not be read.");
    reader.readAsText(file);
    return false;
  };

  const clearCaCert = () => {
    setCaCertError(undefined);
    setCaCertFileName(undefined);
    set("caCertPem", "");
  };

  const issues = useMemo(() => validateMcpServerForm(values), [values]);
  const issueFor = (field: keyof McpServerFormValues) =>
    submitted ? issues.find((issue) => issue.field === field)?.message : undefined;

  const command = previewCommand(values);

  const submit = async () => {
    setSubmitted(true);
    setFailure(undefined);
    if (issues.length > 0) return;

    setSaving(true);
    try {
      await apiClient.mcpServers.create(toCreateRequest(values));
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
        <Segmented<string>
          data-testid="mcp-kind"
          value={values.kind}
          onChange={(value) => {
            // The other branch's fields have never been submitted, so carrying
            // the flag over would greet the user with errors on inputs they have
            // not had the chance to fill in yet.
            setSubmitted(false);
            set("kind", value as McpServerFormValues["kind"]);
          }}
          options={[
            { label: "Remote URL", value: "url" },
            { label: "Command", value: "command" },
          ]}
        />
        <Text css={{ color: theme.color.textMuted }}>
          {values.kind === "url"
            ? "A RemoteMCPServer: the controller connects out to a URL you already host."
            : "An MCPServer: the controller runs the server itself as a Deployment and talks to it over stdio."}
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

          {/* Keyed so switching kind unmounts one branch and mounts the other.
              Without distinct keys React pairs the two branches' Form.Items by
              position and reuses the DOM: the URL field's help text and error
              styling survive onto whichever Command field lands in that slot. */}
          {values.kind === "url" ? (
            <Fragment key="url-fields">
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

              {/* Only over TLS is a CA bundle meaningful, so the field appears
                  only for HTTPS. With one the upstream is verified against it;
                  without one the system trust store is used. */}
              {values.tlsEnabled ? (
                <Form.Item
                  label="CA certificate"
                  validateStatus={caCertError ? "error" : undefined}
                  help={
                    caCertError ??
                    "Optional. Upload a PEM CA bundle to verify the upstream against a private CA. Leave blank to use the system trust store."
                  }
                >
                  <Upload.Dragger
                    accept=".crt,.cer,.pem"
                    maxCount={1}
                    beforeUpload={readCaCert}
                    onRemove={clearCaCert}
                    fileList={
                      caCertFileName
                        ? [
                            {
                              uid: "ca-cert",
                              name: caCertFileName,
                              status: "done" as const,
                            },
                          ]
                        : []
                    }
                  >
                    <p
                      className="ant-upload-drag-icon"
                      css={{ marginBottom: theme.space(2) }}
                    >
                      <UploadIcon
                        size={24}
                        aria-hidden
                        css={{ color: theme.color.textMuted }}
                      />
                    </p>
                    <p
                      data-testid="mcp-ca-upload"
                      className="ant-upload-text"
                    >
                      Drop a CA certificate here, or click to browse
                    </p>
                    <p className="ant-upload-hint">PEM, CRT or CER</p>
                  </Upload.Dragger>
                </Form.Item>
              ) : null}

              <Form.Item>
                <Checkbox
                  data-testid="mcp-streamable"
                  checked={values.streamableHttp}
                  onChange={(event) => set("streamableHttp", event.target.checked)}
                >
                  Use streamable HTTP instead of SSE
                </Checkbox>
              </Form.Item>

              <Form.Item
                label="Headers"
                validateStatus={issueFor("headersJson") ? "error" : undefined}
                help={
                  issueFor("headersJson") ??
                  'A JSON object, for example {"Authorization": "Bearer …"}.'
                }
              >
                <Input.TextArea
                  data-testid="mcp-headers"
                  placeholder="{}"
                  value={values.headersJson}
                  onChange={(event) => set("headersJson", event.target.value)}
                  autoSize={{ minRows: 2, maxRows: 8 }}
                  css={{ fontFamily: theme.font.mono }}
                />
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
            </Fragment>
          ) : (
            <Fragment key="command-fields">
              <Form.Item label="Executor">
                <Select<CommandExecutor>
                  data-testid="mcp-executor"
                  value={values.executor}
                  onChange={(value) => set("executor", value)}
                  options={Object.entries(COMMAND_EXECUTORS).map(([value, meta]) => ({
                    value: value as CommandExecutor,
                    label: `${meta.label} — ${meta.image}`,
                  }))}
                />
              </Form.Item>

              <Form.Item
                label="Package"
                required
                validateStatus={issueFor("packageName") ? "error" : undefined}
                help={issueFor("packageName")}
              >
                <Input
                  data-testid="mcp-package"
                  placeholder="@modelcontextprotocol/server-filesystem"
                  value={values.packageName}
                  onChange={(event) => set("packageName", event.target.value)}
                />
              </Form.Item>

              <Form.Item
                label="Arguments before the package"
                help="Split on spaces, for example -y."
              >
                <Input
                  data-testid="mcp-command-prefix"
                  value={values.commandPrefix}
                  onChange={(event) => set("commandPrefix", event.target.value)}
                />
              </Form.Item>

              <Form.Item label="Arguments">
                <Space orientation="vertical" size={8} css={{ display: "flex" }}>
                  {values.args.map((arg, index) => (
                    <Space key={index} size={8} css={{ display: "flex" }}>
                      <Input
                        data-testid="mcp-arg"
                        aria-label={`Argument ${index + 1}`}
                        value={arg}
                        onChange={(event) =>
                          set(
                            "args",
                            values.args.map((existing, at) =>
                              at === index ? event.target.value : existing,
                            ),
                          )
                        }
                      />
                      <Button
                        aria-label={`Remove argument ${index + 1}`}
                        icon={<Trash2 size={14} />}
                        onClick={() =>
                          set(
                            "args",
                            values.args.filter((_, at) => at !== index),
                          )
                        }
                      />
                    </Space>
                  ))}
                  <Button
                    data-testid="mcp-add-arg"
                    icon={<Plus size={14} />}
                    onClick={() => set("args", [...values.args, ""])}
                  >
                    Add argument
                  </Button>
                </Space>
              </Form.Item>

              <Form.Item label="Environment variables">
                <Space orientation="vertical" size={8} css={{ display: "flex" }}>
                  {values.env.map((row, index) => (
                    <Space key={row.id} size={8} css={{ display: "flex" }}>
                      <Input
                        data-testid="mcp-env-name"
                        aria-label={`Environment variable ${index + 1} name`}
                        placeholder="NAME"
                        value={row.name}
                        onChange={(event) =>
                          set(
                            "env",
                            values.env.map((existing) =>
                              existing.id === row.id
                                ? { ...existing, name: event.target.value }
                                : existing,
                            ),
                          )
                        }
                      />
                      <Input
                        data-testid="mcp-env-value"
                        aria-label={`Environment variable ${index + 1} value`}
                        placeholder="value"
                        value={row.value}
                        onChange={(event) =>
                          set(
                            "env",
                            values.env.map((existing) =>
                              existing.id === row.id
                                ? { ...existing, value: event.target.value }
                                : existing,
                            ),
                          )
                        }
                      />
                      <Button
                        aria-label={`Remove environment variable ${index + 1}`}
                        icon={<Trash2 size={14} />}
                        onClick={() =>
                          set(
                            "env",
                            values.env.filter((existing) => existing.id !== row.id),
                          )
                        }
                      />
                    </Space>
                  ))}
                  <Button
                    data-testid="mcp-add-env"
                    icon={<Plus size={14} />}
                    onClick={() => set("env", [...values.env, newEnvRow()])}
                  >
                    Add variable
                  </Button>
                </Space>
              </Form.Item>

              {/* What will actually run, assembled in the order the executor
                  expects — the part that is easy to get wrong and hard to debug
                  once it is only visible in a crashing pod. */}
              <Form.Item label="Command preview">
                <pre
                  data-testid="mcp-command-preview"
                  css={{
                    margin: 0,
                    padding: theme.space(3),
                    background: theme.color.bgElevated,
                    border: `1px solid ${theme.color.border}`,
                    borderRadius: theme.radius.md,
                    fontFamily: theme.font.mono,
                    color: command ? theme.color.text : theme.color.textMuted,
                    whiteSpace: "pre-wrap",
                    wordBreak: "break-all",
                  }}
                >
                  {command || "Enter a package to see the command."}
                </pre>
              </Form.Item>
            </Fragment>
          )}
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
