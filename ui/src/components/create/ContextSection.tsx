import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import type { ContextConfig } from "@/types";

interface ContextSectionProps {
  context: ContextConfig | undefined;
  onChange: (context: ContextConfig | undefined) => void;
  isSubmitting?: boolean;
}

export function ContextSection({
  context,
  onChange,
  isSubmitting,
}: ContextSectionProps) {
  const compactionEnabled = !!context?.compaction;

  const toggleCompaction = (enabled: boolean) => {
    if (enabled) {
      onChange({
        compaction: {
          compactionInterval: 5,
          overlapSize: 2,
        },
      });
    } else {
      onChange(undefined);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <Label className="text-sm">Event Compaction</Label>
          <p className="text-xs text-muted-foreground mt-1">
            Compress older conversation events to reduce context size while preserving key information.
          </p>
        </div>
        <Switch
          checked={compactionEnabled}
          onCheckedChange={toggleCompaction}
          disabled={isSubmitting}
        />
      </div>

      {compactionEnabled && context?.compaction && (
        <div className="space-y-3 pl-4 border-l-2 border-muted">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <Label className="text-xs mb-1 block">Compaction Interval</Label>
              <Input
                type="number"
                min={1}
                value={context.compaction.compactionInterval ?? 5}
                onChange={(e) =>
                  onChange({
                    ...context,
                    compaction: {
                      ...context.compaction!,
                      compactionInterval: parseInt(e.target.value) || 1,
                    },
                  })
                }
                disabled={isSubmitting}
              />
              <p className="text-xs text-muted-foreground mt-1">
                Number of invocations before triggering compaction.
              </p>
            </div>
            <div>
              <Label className="text-xs mb-1 block">Overlap Size</Label>
              <Input
                type="number"
                min={0}
                value={context.compaction.overlapSize ?? 2}
                onChange={(e) =>
                  onChange({
                    ...context,
                    compaction: {
                      ...context.compaction!,
                      overlapSize: parseInt(e.target.value) || 0,
                    },
                  })
                }
                disabled={isSubmitting}
              />
              <p className="text-xs text-muted-foreground mt-1">
                Number of preceding invocations to overlap for context.
              </p>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <Label className="text-xs mb-1 block">Token Threshold</Label>
              <Input
                type="number"
                min={0}
                value={context.compaction.tokenThreshold ?? ""}
                onChange={(e) => {
                  const val = e.target.value ? parseInt(e.target.value) : undefined;
                  onChange({
                    ...context,
                    compaction: {
                      ...context.compaction!,
                      tokenThreshold: val,
                    },
                  });
                }}
                placeholder="Optional"
                disabled={isSubmitting}
              />
              <p className="text-xs text-muted-foreground mt-1">
                Trigger compaction when prompt tokens exceed this threshold.
              </p>
            </div>
            <div>
              <Label className="text-xs mb-1 block">Event Retention Size</Label>
              <Input
                type="number"
                min={0}
                value={context.compaction.eventRetentionSize ?? ""}
                onChange={(e) => {
                  const val = e.target.value ? parseInt(e.target.value) : undefined;
                  onChange({
                    ...context,
                    compaction: {
                      ...context.compaction!,
                      eventRetentionSize: val,
                    },
                  });
                }}
                placeholder="Optional"
                disabled={isSubmitting}
              />
              <p className="text-xs text-muted-foreground mt-1">
                Number of most recent events to always retain.
              </p>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
