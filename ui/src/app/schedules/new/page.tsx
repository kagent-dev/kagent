"use client";

import { useState, useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Clock, Loader2 } from "lucide-react";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { ScheduledRun, AgentResponse, ScheduledRunTargetKind } from "@/types";
import {
  createScheduledRun,
  updateScheduledRun,
  getScheduledRun,
} from "@/app/actions/scheduledRuns";
import { getAgents } from "@/app/actions/agents";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { isResourceNameValid } from "@/lib/utils";
import { toast } from "sonner";

interface FormState {
  name: string;
  namespace: string;
  schedule: string;
  timeZone: string;
  targetName: string;
  targetNamespace: string;
  targetKind: ScheduledRunTargetKind;
  prompt: string;
  suspended: boolean;
  allowSessionInteraction: boolean;
  executionTimeout: string;
  recentExecutionsLimit: string;
  isSubmitting: boolean;
  isLoading: boolean;
}

interface ValidationErrors {
  name?: string;
  namespace?: string;
  schedule?: string;
  target?: string;
  prompt?: string;
  executionTimeout?: string;
  recentExecutionsLimit?: string;
}

const CRON_FIELD_COUNT = 5;

function validateCronExpression(expr: string): string | undefined {
  const trimmed = expr.trim();
  if (!trimmed) return "Schedule is required";
  const fields = trimmed.split(/\s+/);
  if (fields.length !== CRON_FIELD_COUNT) {
    return `Cron expression must have exactly ${CRON_FIELD_COUNT} fields (minute hour day month weekday)`;
  }
  return undefined;
}


function getScheduledRunTargetKind(agent: AgentResponse): ScheduledRunTargetKind | undefined {
  const kind = agent.agent.kind;
  return kind === "Agent" || kind === "SandboxAgent" ? kind : undefined;
}

function targetSelectValue(kind: ScheduledRunTargetKind, namespace: string, name: string): string {
  return `${kind}/${namespace}/${name}`;
}

function ScheduledRunFormContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const isEditMode = searchParams.get("edit") === "true";
  const editName = searchParams.get("name");
  const editNamespace = searchParams.get("namespace");

  const [agents, setAgents] = useState<AgentResponse[]>([]);
  const [state, setState] = useState<FormState>({
    name: "",
    namespace: "",
    schedule: "",
    timeZone: "UTC",
    targetName: "",
    targetNamespace: "",
    targetKind: "Agent",
    prompt: "",
    suspended: false,
    allowSessionInteraction: false,
    executionTimeout: "15m",
    recentExecutionsLimit: "10",
    isSubmitting: false,
    isLoading: isEditMode && Boolean(editName && editNamespace),
  });
  const [errors, setErrors] = useState<ValidationErrors>({});
  const [loadError, setLoadError] = useState<string | null>(null);

  // Fetch agents list
  useEffect(() => {
    const loadAgents = async () => {
      try {
        const response = await getAgents();
        if (response.error) {
          toast.error(`Failed to load agents: ${response.error}`);
          return;
        }
        if (response.data) {
          setAgents(response.data.filter((agent) => getScheduledRunTargetKind(agent) !== undefined));
        }
      } catch (err) {
        const msg = err instanceof Error ? err.message : "Failed to load agents";
        toast.error(msg);
      }
    };
    loadAgents();
  }, []);

  // Fetch existing data in edit mode
  useEffect(() => {
    const fetchExisting = async () => {
      if (isEditMode && editName && editNamespace) {
        try {
          setState((prev) => ({ ...prev, isLoading: true }));
          const response = await getScheduledRun(editName, editNamespace);
          if (response.error || !response.data) {
            const msg = response.error || "Scheduled Run not found";
            toast.error(msg);
            setLoadError(msg);
            setState((prev) => ({ ...prev, isLoading: false }));
            return;
          }
          const sr = response.data;
          setState((prev) => ({
            ...prev,
            name: sr.metadata.name,
            namespace: sr.metadata.namespace || "",
            schedule: sr.spec.schedule,
            timeZone: sr.spec.timeZone || "UTC",
            targetName: sr.spec.targetRef.name,
            targetNamespace: sr.metadata.namespace || "",
            targetKind: sr.spec.targetRef.kind,
            prompt: sr.spec.prompt,
            suspended: sr.spec.suspended ?? false,
            allowSessionInteraction: sr.spec.allowSessionInteraction ?? false,
            executionTimeout: sr.spec.executionTimeout ?? "15m",
            recentExecutionsLimit: String(sr.spec.recentExecutionsLimit ?? 10),
            isLoading: false,
          }));
        } catch (err) {
          const msg = err instanceof Error ? err.message : "Failed to load Scheduled Run data";
          toast.error(msg);
          setLoadError(msg);
          setState((prev) => ({ ...prev, isLoading: false }));
        }
      }
    };
    fetchExisting();
  }, [isEditMode, editName, editNamespace]);

  const validateForm = (): boolean => {
    const newErrors: ValidationErrors = {};

    if (!state.name.trim()) {
      newErrors.name = "Name is required";
    } else if (!isResourceNameValid(state.name)) {
      newErrors.name = "Name must be a valid RFC 1123 subdomain (lowercase alphanumeric, '-' or '.', max 253 characters)";
    }

    if (!state.namespace.trim()) {
      newErrors.namespace = "Namespace is required";
    }

    const cronError = validateCronExpression(state.schedule);
    if (cronError) {
      newErrors.schedule = cronError;
    }

    if (!state.targetName) {
      newErrors.target = "Target is required";
    } else if (state.targetNamespace !== state.namespace) {
      newErrors.target = "Scheduled Run targets must be in the same namespace";
    }

    if (!state.prompt.trim()) {
      newErrors.prompt = "Prompt is required";
    } else if (state.prompt.length > 32768) {
      newErrors.prompt = "Prompt must not exceed 32,768 characters";
    }

    const executionTimeout = state.executionTimeout.trim();
    if (
      !/^(?:\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h))+$/.test(executionTimeout) ||
      !/[1-9]/.test(executionTimeout)
    ) {
      newErrors.executionTimeout = "Use a positive duration such as 30s, 15m, or 1h30m";
    }

    const recentExecutionsLimitInput = state.recentExecutionsLimit.trim();
    const recentExecutionsLimit = Number.parseInt(recentExecutionsLimitInput, 10);
    if (!/^\d+$/.test(recentExecutionsLimitInput) || recentExecutionsLimit < 1 || recentExecutionsLimit > 100) {
      newErrors.recentExecutionsLimit = "Must be between 1 and 100";
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async () => {
    if (!validateForm()) return;

    setState((prev) => ({ ...prev, isSubmitting: true }));

    try {
      const recentExecutionsLimit = Number.parseInt(state.recentExecutionsLimit, 10);
      const sr: ScheduledRun = {
        apiVersion: "kagent.dev/v1alpha2",
        kind: "ScheduledRun",
        metadata: {
          name: state.name,
          namespace: state.namespace,
        },
        spec: {
          schedule: state.schedule.trim(),
          timeZone: state.timeZone.trim() || "UTC",
          targetRef: {
            apiGroup: "kagent.dev",
            kind: state.targetKind,
            name: state.targetName,
          },
          prompt: state.prompt,
          suspended: state.suspended,
          allowSessionInteraction: state.allowSessionInteraction,
          executionTimeout: state.executionTimeout.trim(),
          recentExecutionsLimit,
        },
      };

      const response = isEditMode
        ? await updateScheduledRun(sr)
        : await createScheduledRun(sr);

      if (response.error) {
        throw new Error(response.error);
      }

      toast.success(
        isEditMode
          ? "Scheduled Run updated successfully"
          : "Scheduled Run created successfully"
      );
      router.push("/schedules");
    } catch (err) {
      const errorMessage =
        err instanceof Error
          ? err.message
          : `Failed to ${isEditMode ? "update" : "create"} Scheduled Run`;
      toast.error(errorMessage);
      setState((prev) => ({ ...prev, isSubmitting: false }));
    }
  };

  const isFormDisabled = state.isSubmitting || state.isLoading;

  if (state.isSubmitting) {
    return <LoadingState />;
  }

  if (loadError) {
    return <ErrorState message={loadError} />;
  }

  if (isEditMode && (!editName || !editNamespace)) {
    return <ErrorState message="Scheduled Run edit URL is missing name or namespace" />;
  }

  return (
    <div className="min-h-screen p-4 md:p-8">
      <div className="max-w-3xl mx-auto">
        <h1 className="text-2xl font-bold mb-8">
          {isEditMode ? "Edit Scheduled Run" : "Create Scheduled Run"}
        </h1>

        <fieldset disabled={isFormDisabled} className="space-y-6 border-0 p-0 m-0">
          {/* Basic Information */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-xl font-bold">
                <Clock className="h-5 w-5" />
                Basic Information
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <Label className="text-base mb-2 block font-bold">Name</Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  Scheduled Run name can only contain lowercase alphanumeric characters, &quot;-&quot; or &quot;.&quot;, and must start and end with an alphanumeric character.
                </p>
                <Input
                  value={state.name}
                  onChange={(e) =>
                    setState((prev) => ({ ...prev, name: e.target.value }))
                  }
                  placeholder="e.g. daily-report"
                  disabled={isFormDisabled || isEditMode}
                  className={errors.name ? "border-red-500" : ""}
                />
                {errors.name && (
                  <p className="text-red-500 text-sm mt-1">{errors.name}</p>
                )}
              </div>

              <div>
                <Label className="text-base mb-2 block font-bold">Namespace</Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  Kubernetes namespace for this Scheduled Run.
                </p>
                <NamespaceCombobox
                  value={state.namespace}
                  onValueChange={(value) =>
                    setState((prev) => ({
                      ...prev,
                      namespace: value,
                      targetName: "",
                      targetNamespace: value,
                      targetKind: "Agent",
                    }))
                  }
                  disabled={isFormDisabled || isEditMode}
                />
                {errors.namespace && (
                  <p className="text-red-500 text-sm mt-1">{errors.namespace}</p>
                )}
              </div>
            </CardContent>
          </Card>

          {/* Schedule Configuration */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-xl font-bold">
                <Clock className="h-5 w-5" />
                Schedule Configuration
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <Label className="text-base mb-2 block font-bold">
                  Schedule
                </Label>
                <Input
                  value={state.schedule}
                  onChange={(e) =>
                    setState((prev) => ({ ...prev, schedule: e.target.value }))
                  }
                  placeholder="e.g. 0 9 * * 1-5"
                  className={`font-mono ${errors.schedule ? "border-red-500" : ""}`}
                  disabled={isFormDisabled}
                />
                {errors.schedule && (
                  <p className="text-red-500 text-sm mt-1">{errors.schedule}</p>
                )}
              </div>

              <div>
                <Label className="text-base mb-2 block font-bold">
                  Time Zone
                </Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  Optional IANA time-zone name (e.g. <code>America/Los_Angeles</code>, <code>Asia/Shanghai</code>). Leave blank to interpret the schedule in UTC.
                </p>
                <Input
                  value={state.timeZone}
                  onChange={(e) =>
                    setState((prev) => ({ ...prev, timeZone: e.target.value }))
                  }
                  placeholder="UTC"
                  className="font-mono"
                  disabled={isFormDisabled}
                />
              </div>

              <div>
                <Label className="text-base mb-2 block font-bold">Target</Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  Select the Agent or SandboxAgent that receives each execution.
                </p>
                <Select
                  value={
                    state.targetName
                      ? targetSelectValue(state.targetKind, state.targetNamespace, state.targetName)
                      : ""
                  }
                  onValueChange={(value) => {
                    const parts = value.split("/");
                    if (parts.length === 3) {
                      setState((prev) => ({
                        ...prev,
                        targetKind: parts[0] as ScheduledRunTargetKind,
                        targetNamespace: parts[1],
                        targetName: parts[2],
                        namespace: parts[1],
                      }));
                    }
                  }}
                  disabled={isFormDisabled}
                >
                  <SelectTrigger
                    className={errors.target ? "border-red-500" : ""}
                  >
                    <SelectValue placeholder="Select a target" />
                  </SelectTrigger>
                  <SelectContent>
                    {agents.map((agent) => {
                      const kind = getScheduledRunTargetKind(agent);
                      if (!kind) return null;
                      const namespace = agent.agent.metadata.namespace || "";
                      const name = agent.agent.metadata.name;
                      const value = targetSelectValue(kind, namespace, name);
                      return (
                        <SelectItem key={value} value={value}>
                          {namespace}/{name} ({kind})
                        </SelectItem>
                      );
                    })}
                  </SelectContent>
                </Select>
                {errors.target && (
                  <p className="text-red-500 text-sm mt-1">{errors.target}</p>
                )}
              </div>

              <div>
                <Label className="text-base mb-2 block font-bold">Prompt</Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  The prompt message sent to the target for each execution.
                </p>
                <Textarea
                  value={state.prompt}
                  onChange={(e) =>
                    setState((prev) => ({ ...prev, prompt: e.target.value }))
                  }
                  placeholder="Enter the prompt for the target..."
                  maxLength={32768}
                  className={`min-h-[120px] ${errors.prompt ? "border-red-500" : ""}`}
                  disabled={isFormDisabled}
                />
                {errors.prompt && (
                  <p className="text-red-500 text-sm mt-1">{errors.prompt}</p>
                )}
              </div>
            </CardContent>
          </Card>

          {/* Advanced Settings */}
          <Card>
            <CardHeader>
              <CardTitle className="text-xl font-bold">
                Advanced Settings
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between rounded-md border p-4">
                <div className="space-y-1">
                  <Label htmlFor="suspend-toggle" className="text-sm font-medium">
                    Suspend
                  </Label>
                  <p className="text-xs text-muted-foreground">
                    When enabled, scheduled executions are paused.
                  </p>
                </div>
                <Switch
                  id="suspend-toggle"
                  checked={state.suspended}
                  onCheckedChange={(checked) =>
                    setState((prev) => ({ ...prev, suspended: checked }))
                  }
                  disabled={isFormDisabled}
                />
              </div>

              <div className="flex items-center justify-between rounded-md border p-4">
                <div className="space-y-1">
                  <Label htmlFor="session-interaction-toggle" className="text-sm font-medium">
                    Allow session interaction
                  </Label>
                  <p className="text-xs text-muted-foreground">
                    Let authorized users continue sessions created by this schedule.
                  </p>
                </div>
                <Switch
                  id="session-interaction-toggle"
                  checked={state.allowSessionInteraction}
                  onCheckedChange={(checked) =>
                    setState((prev) => ({ ...prev, allowSessionInteraction: checked }))
                  }
                  disabled={isFormDisabled}
                />
              </div>

              <div>
                <Label className="text-sm mb-2 block font-bold">
                  Execution Timeout
                </Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  Maximum total time allowed for target dispatch and execution completion.
                </p>
                <Input
                  value={state.executionTimeout}
                  onChange={(e) => {
                    setState((prev) => ({ ...prev, executionTimeout: e.target.value }));
                  }}
                  placeholder="15m"
                  className={errors.executionTimeout ? "border-red-500" : ""}
                  disabled={isFormDisabled}
                />
                {errors.executionTimeout && (
                  <p className="text-red-500 text-sm mt-1">
                    {errors.executionTimeout}
                  </p>
                )}
              </div>

              <div>
                <Label className="text-sm mb-2 block font-bold">
                  Recent Completed Executions
                </Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  Number of completed executions shown with this Scheduled Run (1-100). Older executions remain available in execution history.
                </p>
                <Input
                  type="number"
                  min={1}
                  max={100}
                  value={state.recentExecutionsLimit}
                  onChange={(e) => {
                    setState((prev) => ({ ...prev, recentExecutionsLimit: e.target.value }));
                  }}
                  className={errors.recentExecutionsLimit ? "border-red-500" : ""}
                  disabled={isFormDisabled}
                />
                {errors.recentExecutionsLimit && (
                  <p className="text-red-500 text-sm mt-1">
                    {errors.recentExecutionsLimit}
                  </p>
                )}
              </div>
            </CardContent>
          </Card>

          <div className="flex justify-end">
            <Button
              className="bg-violet-500 hover:bg-violet-600"
              onClick={handleSubmit}
              disabled={isFormDisabled}
            >
              {state.isSubmitting ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  {isEditMode ? "Updating Scheduled Run..." : "Creating Scheduled Run..."}
                </>
              ) : isEditMode ? (
                "Update Scheduled Run"
              ) : (
                "Create Scheduled Run"
              )}
            </Button>
          </div>
        </fieldset>
      </div>
    </div>
  );
}

export default function ScheduledRunFormPage() {
  return <ScheduledRunFormContent />;
}
