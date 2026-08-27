import { Alert, Button, Empty, Skeleton, Space, Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Link, useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { PromptFragment } from "@/components/prompts/PromptFragment";
import { paths } from "@/router/routes";
import { isNotFound, usePrompt } from "@/api";
import { RefreshButton } from "@/components/table/RefreshButton";

const { Text } = Typography;

export function PromptDetailPage() {
  const theme = useTheme();
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const { data, isLoading, error, refresh } = usePrompt(namespace, name);

  // A 404 is a different answer from a failed request: the cluster told us this
  // library does not exist. Retrying would be pointless, so that branch offers a
  // way back to the list instead of a "Try again".
  const missing = error !== undefined && isNotFound(error);

  const fragments = Object.entries(data?.data ?? {});

  return (
    <PageFrame
      title={name ?? "Prompt library"}
      // Describing the contents of a library the cluster says does not exist
      // would contradict the alert directly below it.
      description={
        namespace && !missing
          ? `Prompt fragments in the ${namespace} namespace.`
          : undefined
      }
      actions={
        <Space size={8}>
          <Link to={paths.prompts}>
            <Button>Back to libraries</Button>
          </Link>
          <RefreshButton
            onRefresh={refresh}
            what="Prompt library"
            loading={isLoading}
            // Nothing to retry when the resource is genuinely absent.
            disabled={missing}
          />
        </Space>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        {missing ? (
          <Alert
            type="warning"
            showIcon
            title="This prompt library does not exist"
            description={`No library named ${namespace}/${name} was found in the cluster. It may have been deleted.`}
            data-testid="prompt-detail-not-found"
            action={
              <Link to={paths.prompts}>
                <Button size="small">Back to libraries</Button>
              </Link>
            }
          />
        ) : error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load this prompt library"
            description={error.message}
            data-testid="prompt-detail-error"
            action={
              <Button size="small" onClick={() => void refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        {isLoading ? (
          <Skeleton active paragraph={{ rows: 6 }} data-testid="prompt-detail-loading" />
        ) : null}

        {/* Only claim a count once a load has actually succeeded. */}
        {data ? (
          <Space size={8} data-testid="prompt-detail-meta">
            <Tag css={{ fontFamily: theme.font.mono }}>{data.namespace}</Tag>
            <Text css={{ color: theme.color.textMuted }}>
              {fragments.length} {fragments.length === 1 ? "fragment" : "fragments"}
            </Text>
          </Space>
        ) : null}

        {data && fragments.length === 0 ? (
          <Empty
            data-testid="prompt-detail-empty"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="This library has no fragments yet."
          />
        ) : null}

        {fragments.length > 0 ? (
          <Space
            orientation="vertical"
            size="middle"
            css={{ display: "flex" }}
            data-testid="prompt-fragments"
          >
            {fragments.map(([key, text]) => (
              <PromptFragment
                key={key}
                fragmentKey={key}
                library={data?.name ?? name ?? ""}
                text={text}
              />
            ))}
          </Space>
        ) : null}
      </Space>
    </PageFrame>
  );
}
