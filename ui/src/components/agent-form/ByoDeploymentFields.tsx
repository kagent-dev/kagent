"use client";

import * as React from "react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ChevronDown, PlusCircle, Trash2 } from "lucide-react";
import { ServiceAccountNameField } from "./ServiceAccountNameField";
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
  byoCmd,
  byoArgs,
  replicas,
  imagePullPolicy,
  imagePullSecrets,
  envPairs,
  serviceAccountName,
  errors,
  disabled,
  onByoImageChange,
  onByoCmdChange,
  onByoArgsChange,
  onReplicasChange,
  onImagePullPolicyChange,
  onImagePullSecretsUpdate,
  onAddImagePullSecret,
  onRemoveImagePullSecret,
  onEnvPairChange,
  onAddEnvPair,
  onRemoveEnvPair,
  onServiceAccountChange,
  onServiceAccountBlur,
  onValidateByoImage,
  serviceAccountInputId = "agent-field-service-account-byo",
}: {
  byoImage: string;
  byoCmd: string;
  byoArgs: string;
  replicas: string;
  imagePullPolicy: string;
  imagePullSecrets: string[];
  envPairs: EnvPair[];
  serviceAccountName: string;
  errors: Pick<AgentFormValidationErrors, "model" | "serviceAccountName">;
  disabled: boolean;
  onByoImageChange: (v: string) => void;
  onByoCmdChange: (v: string) => void;
  onByoArgsChange: (v: string) => void;
  onReplicasChange: (v: string) => void;
  onImagePullPolicyChange: (v: string) => void;
  onImagePullSecretsUpdate: (secrets: string[]) => void;
  onAddImagePullSecret: () => void;
  onRemoveImagePullSecret: (index: number) => void;
  onEnvPairChange: (index: number, next: EnvPair) => void;
  onAddEnvPair: () => void;
  onRemoveEnvPair: (index: number) => void;
  onServiceAccountChange: (v: string) => void;
  onServiceAccountBlur: () => void;
  onValidateByoImage: () => void;
  serviceAccountInputId?: string;
}) {
  const [opsOpen, setOpsOpen] = useState(false);

  return (
    <div className="space-y-6">
      <FieldRoot>
        <FieldLabel>Container image</FieldLabel>
        <FieldHint>Image the workload runs. For Sandbox with a custom image, this is required.</FieldHint>
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
          <FieldLabel>Command (optional)</FieldLabel>
          <Input
            value={byoCmd}
            onChange={(e) => onByoCmdChange(e.target.value)}
            placeholder="/app/start"
            disabled={disabled}
          />
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
          <span>Scheduling, registry &amp; environment</span>
          <ChevronDown
            className={cn("h-4 w-4 shrink-0 text-muted-foreground transition-transform", opsOpen && "rotate-180")}
            aria-hidden
          />
        </CollapsibleTrigger>
        <CollapsibleContent className="space-y-4 pt-1 data-[state=open]:border-t-0">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <FieldRoot>
              <FieldLabel>Replicas</FieldLabel>
              <Input
                type="number"
                inputMode="numeric"
                name="replicaCount"
                value={replicas}
                onChange={(e) => onReplicasChange(e.target.value)}
                placeholder="1"
                disabled={disabled}
                className="tabular-nums"
              />
            </FieldRoot>
            <FieldRoot>
              <FieldLabel>Image pull policy</FieldLabel>
              <Select
                value={imagePullPolicy}
                onValueChange={onImagePullPolicyChange}
                disabled={disabled}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select policy" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="Always">Always</SelectItem>
                  <SelectItem value="IfNotPresent">IfNotPresent</SelectItem>
                  <SelectItem value="Never">Never</SelectItem>
                </SelectContent>
              </Select>
            </FieldRoot>
          </div>

          <div className="space-y-2">
            <FieldLabel>Image pull secrets</FieldLabel>
            <FieldHint>One Kubernetes secret name per private registry the node must use.</FieldHint>
            {imagePullSecrets.map((name, idx) => (
              <div key={idx} className="flex flex-wrap items-center gap-2">
                <Input
                  placeholder="Secret name"
                  value={name}
                  onChange={(e) => {
                    const copy = [...imagePullSecrets];
                    copy[idx] = e.target.value;
                    onImagePullSecretsUpdate(copy);
                  }}
                  disabled={disabled}
                />
                <div className="flex gap-1">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={onAddImagePullSecret}
                    disabled={disabled}
                  >
                    Add secret
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => onRemoveImagePullSecret(idx)}
                    disabled={imagePullSecrets.length <= 1 || disabled}
                    aria-label={`Remove image pull secret row ${idx + 1}`}
                  >
                    Remove
                  </Button>
                </div>
              </div>
            ))}
          </div>

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

      <ServiceAccountNameField
        inputId={serviceAccountInputId}
        value={serviceAccountName}
        onChange={onServiceAccountChange}
        onBlur={onServiceAccountBlur}
        error={errors.serviceAccountName}
        disabled={disabled}
      />
    </div>
  );
}

