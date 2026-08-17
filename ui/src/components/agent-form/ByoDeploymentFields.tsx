"use client";

import * as React from "react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ChevronDown, PlusCircle, Trash2 } from "lucide-react";
import { FieldError, FieldHint, FieldLabel, FieldRoot } from "./form-primitives";
import { cn } from "@/lib/utils";
import { AgentFormValidationErrors } from "./agent-form-types";

type EnvPair = {
  name: string;
  value?: string;
  isSecret?: boolean;
  secretName?: string;
  secretKey?: string;
  optional?: boolean;
};

export function ByoDeploymentFields({
  byoImage,
  commandRequired = false,
  byoCmd,
  byoArgs,
  envPairs,
  errors,
  disabled,
  onByoImageChange,
  onByoCmdChange,
  onByoArgsChange,
  onEnvPairChange,
  onAddEnvPair,
  onRemoveEnvPair,
  onValidateByoImage,
}: {
  byoImage: string;
  /** When true (BYO on Agent Substrate), the command is required and the label reflects that. */
  commandRequired?: boolean;
  byoCmd: string;
  byoArgs: string;
  envPairs: EnvPair[];
  errors: Pick<AgentFormValidationErrors, "model" | "byoCmd">;
  disabled: boolean;
  onByoImageChange: (v: string) => void;
  onByoCmdChange: (v: string) => void;
  onByoArgsChange: (v: string) => void;
  onEnvPairChange: (index: number, next: EnvPair) => void;
  onAddEnvPair: () => void;
  onRemoveEnvPair: (index: number) => void;
  onValidateByoImage: () => void;
}) {
  const [opsOpen, setOpsOpen] = useState(false);

  return (
    <div className="space-y-6">
      <FieldRoot>
        <FieldLabel>Container image</FieldLabel>
        <FieldHint>Container image the workload runs (required for BYO).</FieldHint>
        <Input
          id="agent-field-byo-image"
          name="byoImage"
          value={byoImage}
          onChange={(e) => onByoImageChange(e.target.value)}
          onBlur={() => {
            onValidateByoImage();
          }}
          placeholder="e.g. ghcr.io/org/agent:v1.0.0"
          autoComplete="off"
          spellCheck={false}
          translate="no"
          disabled={disabled}
          className={errors.model ? "border-destructive" : ""}
          aria-invalid={!!errors.model}
        />
        <FieldError>{errors.model}</FieldError>
      </FieldRoot>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <FieldRoot>
          <FieldLabel>{commandRequired ? "Command (required)" : "Command (optional)"}</FieldLabel>
          {commandRequired && (
            <FieldHint>
              Required on Agent Substrate: it copies the command verbatim and does not fall back to
              the image entrypoint.
            </FieldHint>
          )}
          <Input
            value={byoCmd}
            onChange={(e) => onByoCmdChange(e.target.value)}
            placeholder="/app/start"
            disabled={disabled}
            className={errors.byoCmd ? "border-destructive" : ""}
            aria-invalid={!!errors.byoCmd}
          />
          <FieldError>{errors.byoCmd}</FieldError>
        </FieldRoot>
        <FieldRoot>
          <FieldLabel>Args (space-separated)</FieldLabel>
          <Input
            value={byoArgs}
            onChange={(e) => onByoArgsChange(e.target.value)}
            placeholder="--port 8080 --flag"
            disabled={disabled}
          />
        </FieldRoot>
      </div>

      <Collapsible open={opsOpen} onOpenChange={setOpsOpen} className="space-y-3">
        <CollapsibleTrigger
          className="flex w-full items-center justify-between gap-2 rounded-md border border-dashed border-border/80 bg-muted/20 px-3 py-2 text-left text-sm font-medium text-foreground transition-colors hover:bg-muted/40"
          type="button"
        >
          <span>Environment</span>
          <ChevronDown
            className={cn("h-4 w-4 shrink-0 text-muted-foreground transition-transform", opsOpen && "rotate-180")}
            aria-hidden
          />
        </CollapsibleTrigger>
        <CollapsibleContent className="space-y-4 pt-1 data-[state=open]:border-t-0">
          <div className="space-y-2">
            <FieldLabel>Environment variables</FieldLabel>
            {envPairs.map((pair, index) => (
              <div key={index} className="flex flex-col gap-2 rounded-md border border-border/70 bg-background/50 p-3">
                <div className="flex flex-wrap items-center gap-2">
                  <Input
                    placeholder="Name (e.g. API_KEY)"
                    value={pair.name}
                    onChange={(e) => onEnvPairChange(index, { ...pair, name: e.target.value })}
                    className="min-w-0 flex-1"
                    disabled={disabled}
                  />
                  <div className="flex items-center gap-2">
                    <Checkbox
                      id={`env-secret-${index}`}
                      checked={!!pair.isSecret}
                      onCheckedChange={(checked) => onEnvPairChange(index, { ...pair, isSecret: !!checked })}
                      disabled={disabled}
                    />
                    <Label htmlFor={`env-secret-${index}`} className="text-xs whitespace-nowrap">
                      From secret
                    </Label>
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="shrink-0"
                    onClick={() => onRemoveEnvPair(index)}
                    disabled={envPairs.length === 1}
                  >
                    <span className="sr-only">Remove</span>
                    <Trash2 className="h-4 w-4 text-destructive" aria-hidden />
                  </Button>
                </div>
                {!pair.isSecret ? (
                  <Input
                    placeholder="Value"
                    value={pair.value ?? ""}
                    onChange={(e) => onEnvPairChange(index, { ...pair, value: e.target.value })}
                    disabled={disabled}
                  />
                ) : (
                  <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
                    <Input
                      placeholder="Secret name"
                      value={pair.secretName ?? ""}
                      onChange={(e) => onEnvPairChange(index, { ...pair, secretName: e.target.value })}
                      disabled={disabled}
                    />
                    <Input
                      placeholder="Secret key"
                      value={pair.secretKey ?? ""}
                      onChange={(e) => onEnvPairChange(index, { ...pair, secretKey: e.target.value })}
                      disabled={disabled}
                    />
                    <div className="flex items-center gap-2 sm:col-span-1">
                      <Checkbox
                        id={`env-optional-${index}`}
                        checked={!!pair.optional}
                        onCheckedChange={(checked) => onEnvPairChange(index, { ...pair, optional: !!checked })}
                        disabled={disabled}
                      />
                      <Label htmlFor={`env-optional-${index}`} className="text-xs">
                        Optional key
                      </Label>
                    </div>
                  </div>
                )}
              </div>
            ))}
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="w-full"
              onClick={onAddEnvPair}
              disabled={disabled}
            >
              <PlusCircle className="mr-2 h-4 w-4" />
              Add variable
            </Button>
          </div>
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}
