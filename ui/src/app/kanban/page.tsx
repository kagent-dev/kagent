import { Kanban } from "lucide-react";

export default function KanbanPage() {
  return (
    <div className="flex flex-col items-center justify-center h-full min-h-[400px] gap-4 text-muted-foreground">
      <Kanban className="h-12 w-12 opacity-30" />
      <p className="text-lg font-medium">Kanban</p>
      <p className="text-sm">Coming soon</p>
    </div>
  );
}
