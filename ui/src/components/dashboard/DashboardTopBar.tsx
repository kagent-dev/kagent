"use client";

import { Wifi, LogOut } from "lucide-react";
import { Button } from "@/components/ui/button";
import { NamespaceSelector } from "@/components/sidebars/NamespaceSelector";
import { useNamespace } from "@/lib/namespace-context";

export function DashboardTopBar() {
  const { namespace, setNamespace } = useNamespace();

  return (
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-2">
        <span className="text-sm text-muted-foreground">Namespace:</span>
        <NamespaceSelector value={namespace} onValueChange={setNamespace} />
      </div>
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-2 text-sm">
          <span className="h-2 w-2 rounded-full bg-green-500" />
          <Wifi className="h-4 w-4 text-green-500" />
          <span className="text-muted-foreground">Stream Connected</span>
        </div>
        <Button variant="ghost" size="icon">
          <LogOut className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}
