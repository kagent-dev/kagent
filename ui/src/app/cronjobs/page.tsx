import { Clock } from "lucide-react";

export default function CronJobsPage() {
  return (
    <div className="flex flex-col items-center justify-center h-full min-h-[400px] gap-4 text-muted-foreground">
      <Clock className="h-12 w-12 opacity-30" />
      <p className="text-lg font-medium">Cron Jobs</p>
      <p className="text-sm">Coming soon</p>
    </div>
  );
}
