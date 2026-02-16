import React from "react";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { ResumabilityConfig } from "@/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { History } from "lucide-react";

interface ResumabilitySectionProps {
  config: ResumabilityConfig | undefined;
  onChange: (config: ResumabilityConfig | undefined) => void;
  disabled: boolean;
}

export const ResumabilitySection = ({ config, onChange, disabled }: ResumabilitySectionProps) => {
  const isResumable = config?.isResumable || false;

  const handleChange = (checked: boolean) => {
    if (checked) {
      onChange({ isResumable: true });
    } else {
      onChange(undefined);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <History className="h-5 w-5 text-orange-500" />
          Resumability
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex items-center justify-between">
          <div>
            <Label className="text-base font-bold">Enable Resumability</Label>
            <p className="text-xs text-muted-foreground">
              Allow the agent to pause and resume long-running invocations.
            </p>
          </div>
          <Switch
            checked={isResumable}
            onCheckedChange={handleChange}
            disabled={disabled}
          />
        </div>
      </CardContent>
    </Card>
  );
};
