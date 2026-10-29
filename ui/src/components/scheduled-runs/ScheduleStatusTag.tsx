import { Tag, Tooltip } from "antd";
import type { ScheduledRun } from "@/api/domain/scheduledRuns";
import { scheduleStatus } from "./scheduleStatus";

/** The status as a tag, with the controller's own words a hover away when it is not ready. */
export function ScheduleStatusTag({ schedule }: { schedule: ScheduledRun }) {
  const status = scheduleStatus(schedule);
  switch (status.kind) {
    case "paused": return <Tag color="warning">Paused</Tag>;
    case "active": return <Tag color="success">Active</Tag>;
    case "notReady":
      return <Tooltip title={status.message ?? status.reason ?? "The controller reports this schedule is not ready."}>
        <Tag color="error" data-testid="schedule-not-ready">Not ready</Tag>
      </Tooltip>;
    case "notReported":
      return <Tooltip title="The controller has recorded no Ready condition for this schedule yet. That is not a failure — a schedule it has not observed looks exactly like this.">
        <Tag>Not reported</Tag>
      </Tooltip>;
  }
}
