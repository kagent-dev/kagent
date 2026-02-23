"use client";

import { Settings, RefreshCw } from "lucide-react";
import { Button } from "./ui/button";
import { Switch } from "./ui/switch";
import { Label } from "./ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "./ui/popover";
import { useSettings } from "@/contexts/SettingsContext";
import { useAgents } from "./AgentsProvider";
import { ClientOnly } from "./ClientOnly";

const REFRESH_INTERVALS = [
  { label: "5 seconds", value: 5000 },
  { label: "10 seconds", value: 10000 },
  { label: "30 seconds", value: 30000 },
  { label: "1 minute", value: 60000 },
  { label: "5 minutes", value: 300000 },
];

function SettingsPanelComponent() {
  const { 
    autoRefreshEnabled, 
    autoRefreshInterval, 
    setAutoRefreshEnabled, 
    setAutoRefreshInterval 
  } = useSettings();
  const { refreshAgents } = useAgents();

  const handleManualRefresh = () => {
    // This will trigger SWR to refetch all agent data
    refreshAgents();
  };

  const getCurrentIntervalLabel = () => {
    const interval = REFRESH_INTERVALS.find(i => i.value === autoRefreshInterval);
    return interval ? interval.label : "30 seconds";
  };

  return (
    <div className="flex items-center gap-2">
      {/* Manual refresh button */}
      <Button
        variant="outline"
        size="sm"
        onClick={handleManualRefresh}
        className="flex items-center gap-2"
      >
        <RefreshCw className="h-4 w-4" />
        Refresh
      </Button>

      {/* Settings popover */}
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="outline" size="sm">
            <Settings className="h-4 w-4" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-80" align="end">
          <div className="space-y-4">
            <div className="space-y-2">
              <h4 className="font-medium leading-none">Auto-refresh Settings</h4>
              <p className="text-sm text-muted-foreground">
                Configure automatic data refresh preferences
              </p>
            </div>
            
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label htmlFor="auto-refresh">Auto-refresh</Label>
                  <div className="text-xs text-muted-foreground">
                    Automatically update data in the background
                  </div>
                </div>
                <Switch
                  id="auto-refresh"
                  checked={autoRefreshEnabled}
                  onCheckedChange={setAutoRefreshEnabled}
                />
              </div>

              {autoRefreshEnabled && (
                <div className="space-y-2">
                  <Label htmlFor="refresh-interval">Refresh Interval</Label>
                  <Select
                    value={autoRefreshInterval.toString()}
                    onValueChange={(value) => setAutoRefreshInterval(parseInt(value, 10))}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder={getCurrentIntervalLabel()} />
                    </SelectTrigger>
                    <SelectContent>
                      {REFRESH_INTERVALS.map((interval) => (
                        <SelectItem key={interval.value} value={interval.value.toString()}>
                          {interval.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <div className="text-xs text-muted-foreground">
                    How often to check for updates
                  </div>
                </div>
              )}
            </div>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}

export function SettingsPanel() {
  return (
    <ClientOnly>
      <SettingsPanelComponent />
    </ClientOnly>
  );
}
