import React from "react";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { ContextConfig, ModelConfig } from "@/types";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Settings } from "lucide-react";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";

interface ContextSectionProps {
  config: ContextConfig | undefined;
  onChange: (config: ContextConfig | undefined) => void;
  error?: string;
  disabled: boolean;
  models: ModelConfig[];
  agentNamespace: string;
}

export const ContextSection = ({ config, onChange, error, disabled, models, agentNamespace }: ContextSectionProps) => {
  const compaction = config?.compaction;
  const cache = config?.cache;

  const handleCompactionChange = (enabled: boolean) => {
    if (enabled) {
      onChange({
        ...config,
        compaction: {
          compactionInterval: 10,
          overlapSize: 2,
        },
      });
    } else {
      const newConfig = { ...config };
      delete newConfig.compaction;
      if (!newConfig.cache && Object.keys(newConfig).length === 0) {
        onChange(undefined);
      } else {
        onChange(newConfig);
      }
    }
  };

  const handleCacheChange = (enabled: boolean) => {
    if (enabled) {
      onChange({
        ...config,
        cache: {
          cacheIntervals: 10,
          ttlSeconds: 1800,
          minTokens: 0,
        },
      });
    } else {
      const newConfig = { ...config };
      delete newConfig.cache;
      if (!newConfig.compaction && Object.keys(newConfig).length === 0) {
        onChange(undefined);
      } else {
        onChange(newConfig);
      }
    }
  };

  const updateCompaction = (key: string, value: string | number | undefined) => {
    if (!compaction) return;
    onChange({
      ...config,
      compaction: {
        ...compaction,
        [key]: value,
      },
    });
  };

  const updateSummarizer = (key: string, value: string | undefined) => {
    if (!compaction) return;
    const currentSummarizer = compaction.summarizer || {};
    // If value is empty/undefined and promptTemplate is also empty, we could clear summarizer, but simpler to keep it if initialized
    onChange({
      ...config,
      compaction: {
        ...compaction,
        summarizer: {
          ...currentSummarizer,
          [key]: value,
        },
      },
    });
  };

  const updateCache = (key: string, value: number | undefined) => {
    if (!cache) return;
    onChange({
      ...config,
      cache: {
        ...cache,
        [key]: value,
      },
    });
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Settings className="h-5 w-5 text-purple-500" />
          Context Management
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Compaction Config */}
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <Label className="text-base font-bold">Event Compaction</Label>
              <p className="text-xs text-muted-foreground">
                Summarize older events to reduce context size.
              </p>
            </div>
            <Switch
              checked={!!compaction}
              onCheckedChange={handleCompactionChange}
              disabled={disabled}
            />
          </div>

          {compaction && (
            <div className="grid grid-cols-2 gap-4 pl-4 border-l-2 border-muted">
              <div>
                <Label>Compaction Interval (events)</Label>
                <Input
                  type="number"
                  value={compaction.compactionInterval}
                  onChange={(e) => updateCompaction("compactionInterval", parseInt(e.target.value) || 0)}
                  disabled={disabled}
                />
              </div>
              <div>
                <Label>Overlap Size (events)</Label>
                <Input
                  type="number"
                  value={compaction.overlapSize}
                  onChange={(e) => updateCompaction("overlapSize", parseInt(e.target.value) || 0)}
                  disabled={disabled}
                />
              </div>
              <div>
                <Label>Token Threshold (optional)</Label>
                <p className="text-xs text-muted-foreground">Requires Event Retention Size to be set.</p>
                <Input
                  type="number"
                  value={compaction.tokenThreshold || ""}
                  onChange={(e) => updateCompaction("tokenThreshold", e.target.value ? parseInt(e.target.value) : undefined)}
                  placeholder="e.g. 150000"
                  disabled={disabled}
                />
              </div>
              <div>
                <Label>Event Retention Size (optional)</Label>
                <Input
                  type="number"
                  value={compaction.eventRetentionSize || ""}
                  onChange={(e) => updateCompaction("eventRetentionSize", e.target.value ? parseInt(e.target.value) : undefined)}
                  placeholder="e.g. 3"
                  disabled={disabled}
                />
              </div>
              <div>
                <Label>Summarizer Model (optional)</Label>
                <Select
                  value={compaction.summarizer?.modelConfig || "default"}
                  onValueChange={(val) => {
                    const modelConfig = val === "default" ? undefined : val;
                    updateSummarizer("modelConfig", modelConfig);
                  }}
                  disabled={disabled}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Use agent model" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="default">Use agent model</SelectItem>
                    {models.filter(m => !agentNamespace || !m.ref.includes("/") || m.ref.startsWith(agentNamespace + "/")).map((model) => (
                      <SelectItem key={model.ref} value={model.ref}>
                        {model.model} ({model.ref})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="col-span-2">
                <Label>Summarizer Prompt Template (optional)</Label>
                <Textarea
                  value={compaction.summarizer?.promptTemplate || ""}
                  onChange={(e) => updateSummarizer("promptTemplate", e.target.value)}
                  placeholder="Custom prompt template..."
                  disabled={disabled}
                  className="min-h-[100px] font-mono"
                />
              </div>
            </div>
          )}
        </div>

        {/* Cache Config */}
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <Label className="text-base font-bold">Context Caching</Label>
              <p className="text-xs text-muted-foreground">
                Cache prefix context at the provider level.
              </p>
            </div>
            <Switch
              checked={!!cache}
              onCheckedChange={handleCacheChange}
              disabled={disabled}
            />
          </div>

          {cache && (
            <div className="grid grid-cols-2 gap-4 pl-4 border-l-2 border-muted">
              <div>
                <Label>Cache Intervals (events)</Label>
                <Input
                  type="number"
                  value={cache.cacheIntervals || ""}
                  onChange={(e) => updateCache("cacheIntervals", e.target.value ? parseInt(e.target.value) : undefined)}
                  placeholder="Default: 10"
                  disabled={disabled}
                />
              </div>
              <div>
                <Label>TTL Seconds</Label>
                <Input
                  type="number"
                  value={cache.ttlSeconds || ""}
                  onChange={(e) => updateCache("ttlSeconds", e.target.value ? parseInt(e.target.value) : undefined)}
                  placeholder="Default: 1800"
                  disabled={disabled}
                />
              </div>
              <div>
                <Label>Min Tokens</Label>
                <Input
                  type="number"
                  value={cache.minTokens || ""}
                  onChange={(e) => updateCache("minTokens", e.target.value ? parseInt(e.target.value) : undefined)}
                  placeholder="Default: 0"
                  disabled={disabled}
                />
              </div>
            </div>
          )}
        </div>
        {error && <p className="text-red-500 text-sm mt-2">{error}</p>}
      </CardContent>
    </Card>
  );
};
