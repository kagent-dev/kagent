import { LucideIcon } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";

interface StatCardProps {
  icon: LucideIcon;
  label: string;
  count: number;
}

export function StatCard({ icon: Icon, label, count }: StatCardProps) {
  return (
    <Card className="bg-card">
      <CardContent className="flex items-center gap-3 p-4">
        <Icon className="h-5 w-5 text-muted-foreground" />
        <div>
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{label}</p>
          <p className="text-2xl font-bold">{count}</p>
        </div>
      </CardContent>
    </Card>
  );
}
