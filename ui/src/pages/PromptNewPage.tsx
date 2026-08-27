import { useMemo, useState } from "react";
import { Alert, Button, Form, Input, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Link, useNavigate } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { SubmitError } from "@/components/common/SubmitError";
import { FragmentEditor } from "@/components/prompts/FragmentEditor";
import {
  type FragmentRow,
  findDuplicateKey,
  fragmentsToData,
  newFragmentRow,
} from "@/components/prompts/fragmentRows";
import { DEFAULT_NAMESPACE, RFC1123_SUBDOMAIN } from "@/components/common/resourceName";
import { paths } from "@/router/routes";
import { apiClient } from "@/api";

const { Text } = Typography;

export function PromptNewPage() {
  const theme = useTheme();
  const navigate = useNavigate();
  const [namespace, setNamespace] = useState(DEFAULT_NAMESPACE);
  const [name, setName] = useState("");
  const [rows, setRows] = useState<FragmentRow[]>(() => [newFragmentRow()]);
  const [submitted, setSubmitted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<unknown>();

  const data = useMemo(() => fragmentsToData(rows), [rows]);
  const duplicate = useMemo(() => findDuplicateKey(rows), [rows]);

  const issues = useMemo(() => {
    const found: string[] = [];
    if (!namespace.trim()) found.push("A namespace is required.");
    else if (!RFC1123_SUBDOMAIN.test(namespace.trim())) {
      found.push("The namespace must be a valid Kubernetes name.");
    }
    if (!name.trim()) found.push("A library name is required.");
    else if (!RFC1123_SUBDOMAIN.test(name.trim())) {
      found.push(
        "The name must use lowercase letters, numbers and hyphens, starting and ending with a letter or number.",
      );
    }
    if (Object.keys(data).length === 0) found.push("Add at least one fragment key.");
    // A duplicate would silently overwrite: the map has one slot per key, so the
    // second value wins and the first fragment vanishes without a word.
    if (duplicate) found.push(`Two fragments share the key "${duplicate}".`);
    return found;
  }, [namespace, name, data, duplicate]);

  const invalidName = submitted && !RFC1123_SUBDOMAIN.test(name.trim());

  const submit = async () => {
    setSubmitted(true);
    setFailure(undefined);
    if (issues.length > 0) return;

    setSaving(true);
    try {
      await apiClient.prompts.create({
        namespace: namespace.trim(),
        name: name.trim(),
        data,
      });
      // Straight to the list, where the new library can actually be seen — a
      // success message on a form the user is still looking at proves less than
      // the row itself.
      await navigate(paths.prompts);
    } catch (error) {
      setFailure(error);
    } finally {
      setSaving(false);
    }
  };

  return (
    <PageFrame
      title="New prompt library"
      description="A library is a named bag of prompt fragments agents can include by key."
      actions={
        <Link to={paths.prompts}>
          <Button>Back to libraries</Button>
        </Link>
      }
    >
      <Space
        orientation="vertical"
        size="middle"
        css={{ display: "flex", maxWidth: 720 }}
      >
        <Form layout="vertical" component="div">
          <Form.Item
            label="Namespace"
            required
            validateStatus={submitted && !namespace.trim() ? "error" : undefined}
            help={submitted && !namespace.trim() ? "A namespace is required." : undefined}
          >
            <Input
              data-testid="prompt-namespace"
              placeholder="kagent"
              value={namespace}
              onChange={(event) => {
                setNamespace(event.target.value);
                setFailure(undefined);
              }}
            />
          </Form.Item>

          <Form.Item
            label="Name"
            required
            validateStatus={submitted && (!name.trim() || invalidName) ? "error" : undefined}
            help={
              submitted && !name.trim()
                ? "A library name is required."
                : invalidName
                  ? "Lowercase letters, numbers and hyphens only."
                  : undefined
            }
          >
            <Input
              data-testid="prompt-name"
              placeholder="team-prompts"
              value={name}
              onChange={(event) => {
                setName(event.target.value);
                setFailure(undefined);
              }}
              css={{ fontFamily: theme.font.mono }}
            />
          </Form.Item>
        </Form>

        <div>
          <Text strong css={{ display: "block", marginBottom: theme.space(1) }}>
            Fragments
          </Text>
          <Text
            css={{ display: "block", marginBottom: theme.space(3), color: theme.color.textMuted }}
          >
            Each key becomes an include target. An agent pulls one in with the tag shown
            beside it.
          </Text>
          <FragmentEditor
            rows={rows}
            onChange={(next) => {
              setRows(next);
              setFailure(undefined);
            }}
            library={name.trim() || undefined}
          />
        </div>

        {submitted && issues.length > 0 ? (
          <Alert
            type="error"
            showIcon
            title="This library cannot be created yet"
            data-testid="prompt-form-errors"
            description={
              <ul css={{ margin: 0, paddingLeft: theme.space(5) }}>
                {issues.map((issue) => (
                  <li key={issue}>{issue}</li>
                ))}
              </ul>
            }
          />
        ) : null}

        {failure !== undefined ? (
          <SubmitError
            error={failure}
            what="prompt library"
            onRetry={() => void submit()}
            data-testid="prompt-submit-error"
          />
        ) : null}

        <Space size={8}>
          <Button
            type="primary"
            data-testid="prompt-submit"
            loading={saving}
            onClick={() => void submit()}
          >
            Create library
          </Button>
          <Link to={paths.prompts}>
            <Button>Cancel</Button>
          </Link>
        </Space>
      </Space>
    </PageFrame>
  );
}
