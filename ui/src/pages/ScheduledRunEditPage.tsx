import { Alert, Skeleton, Space } from "antd";
import { ReadError } from "@/components/common/ReadError";
import { useNavigate, useParams } from "react-router-dom";
import { BackLink } from "@/components/form/BackLink";
import { invoke } from "@/api/operations";
import { useApiResource } from "@/api/hooks/useApiResource";
import { PageFrame } from "@/components/Structure/PageFrame";
import { ScheduledRunForm } from "@/components/scheduled-runs/ScheduledRunForm";
import { buildPath, paths } from "@/router/routes";

/**
 * Change one schedule.
 *
 * The schedule is read before the form is shown rather than the form appearing empty
 * and filling in: `initialValues` is taken once, so a form mounted before the read
 * lands would keep the blanks — and a reader typing into it would be editing nothing.
 *
 * No polling here, unlike the detail page. A background re-read cannot reach a field
 * the reader has already changed, so all it could do is move the etag under an edit
 * in progress.
 */
export function ScheduledRunEditPage() {
  const navigate = useNavigate();
  const { id } = useParams();
  const run = useApiResource(id ? ["scheduledRuns.get", id] : null, () =>
    invoke("scheduledRuns.get", { scheduledRunId: id ?? "" }),
  );
  const schedule = run.data?.scheduledRun;
  const detail = id ? buildPath(paths.scheduledRun, { id }) : paths.scheduledRuns;

  /*
   * Re-read before leaving, so the detail page opens on the saved values rather than
   * on the ones just replaced — both pages read the same SWR key. A failed re-read is
   * swallowed: the save succeeded, and the detail page reads again on mount.
   */
  async function leaveSaved() {
    try {
      await run.refresh();
    } catch {
      /* Left to the detail page's own read. */
    }
    await navigate(detail);
  }

  return (
    <>
    <BackLink to={paths.scheduledRuns} resource="Schedules" />
    <PageFrame
      title={schedule?.config?.name ? `Edit ${schedule.config.name}` : "Edit schedule"}
      description="Changes take effect from the next firing. Executions already accepted continue."
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        {!id ? (
          <Alert type="error" showIcon title="Missing schedule ID" />
        ) : run.error ? (
          <ReadError
            resource="this schedule"
            error={run.error}
            testId="schedule-edit-load-error"
            onRetry={run.refresh}
          />
        ) : null}

        {run.isLoading ? (
          <Skeleton active paragraph={{ rows: 8 }} data-testid="schedule-edit-loading" />
        ) : null}

        {schedule ? (
          <ScheduledRunForm
            schedule={schedule}
            cancelTo={detail}
            onSaved={() => void leaveSaved()}
          />
        ) : null}
      </Space>
    </PageFrame>
    </>
  );
}
