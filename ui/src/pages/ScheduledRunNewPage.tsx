import { Button } from "antd";
import { Link, useNavigate } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { ScheduledRunForm } from "@/components/scheduled-runs/ScheduledRunForm";
import { buildPath, paths } from "@/router/routes";

/**
 * Create a schedule.
 *
 * The fields live in `ScheduledRunForm`, shared with the edit page. This page owns
 * only where the reader goes afterwards: the new schedule, which is the one thing a
 * reader wants next — its cadence, its next firing, and a Run button.
 */
export function ScheduledRunNewPage() {
  const navigate = useNavigate();

  return (
    <PageFrame
      title="New schedule"
      description="Run an agent automatically. Each execution starts a new conversation."
      actions={
        <Link to={paths.scheduledRuns}>
          <Button>Back to schedules</Button>
        </Link>
      }
    >
      <ScheduledRunForm
        onCancel={() => void navigate(paths.scheduledRuns)}
        onSaved={(saved) => void navigate(buildPath(paths.scheduledRun, { id: saved.id }))}
      />
    </PageFrame>
  );
}
