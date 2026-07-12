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
  agentName: string;
  agentNamespace: string;
  agentKind: ScheduledRunTargetKind;
  prompt: string;
  suspend: boolean;
  maxRunHistory: string;
  isSubmitting: boolean;
  isLoading: boolean;
}

interface ValidationErrors {
  name?: string;
  namespace?: string;
  schedule?: string;
  agent?: string;
  prompt?: string;
  maxRunHistory?: string;
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


function getSchedulableAgentKind(agent: AgentResponse): ScheduledRunTargetKind | undefined {
  const kind = agent.agent.kind;
  return kind === "Agent" || kind === "SandboxAgent" ? kind : undefined;
}

function agentSelectValue(kind: ScheduledRunTargetKind, namespace: string, name: string): string {
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
    agentName: "",
    agentNamespace: "",
    agentKind: "Agent",
    prompt: "",
    suspend: false,
    maxRunHistory: "10",
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
          setAgents(response.data.filter((agent) => getSchedulableAgentKind(agent) !== undefined));
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
            const msg = response.error || "Scheduled run not found";
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
            agentName: sr.spec.targetRef.name,
            agentNamespace: sr.spec.targetRef.namespace || sr.metadata.namespace || "",
            agentKind: sr.spec.targetRef.kind,
            prompt: sr.spec.prompt,
            suspend: sr.spec.suspend ?? false,
            maxRunHistory: String(sr.spec.maxRunHistory ?? 10),
            isLoading: false,
          }));
        } catch (err) {
          const msg = err instanceof Error ? err.message : "Failed to load scheduled run data";
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

    if (!state.agentName) {
      newErrors.agent = "Agent is required";
    }

    if (!state.prompt.trim()) {
      newErrors.prompt = "Prompt is required";
    }

    const maxRunHistoryInput = state.maxRunHistory.trim();
    const maxRunHistory = Number.parseInt(maxRunHistoryInput, 10);
    if (!/^\d+$/.test(maxRunHistoryInput) || maxRunHistory < 1 || maxRunHistory > 100) {
      newErrors.maxRunHistory = "Must be between 1 and 100";
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async () => {
    if (!validateForm()) return;

    setState((prev) => ({ ...prev, isSubmitting: true }));

    try {
      const maxRunHistory = Number.parseInt(state.maxRunHistory, 10);
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
            kind: state.agentKind,
            namespace: state.agentNamespace,
            name: state.agentName,
          },
          prompt: state.prompt,
          suspend: state.suspend,
          maxRunHistory,
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
          ? "Scheduled run updated successfully"
          : "Scheduled run created successfully"
      );
      router.push("/schedules");
    } catch (err) {
      const errorMessage =
        err instanceof Error
          ? err.message
          : `Failed to ${isEditMode ? "update" : "create"} scheduled run`;
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
    return <ErrorState message="Scheduled run edit URL is missing name or namespace" />;
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
                  Schedule name can only contain lowercase alphanumeric characters, &quot;-&quot; or &quot;.&quot;, and must start and end with an alphanumeric character.
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
                  Kubernetes namespace for this scheduled run.
                </p>
                <NamespaceCombobox
                  value={state.namespace}
                  onValueChange={(value) =>
                    setState((prev) => ({
                      ...prev,
                      namespace: value,
                      agentName: "",
                      agentNamespace: value,
                      agentKind: "Agent",
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
                  Cron Schedule
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
                <Label className="text-base mb-2 block font-bold">Agent</Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  Select the agent to run on this schedule.
                </p>
                <Select
                  value={
                    state.agentName
                      ? agentSelectValue(state.agentKind, state.agentNamespace, state.agentName)
                      : ""
                  }
                  onValueChange={(val) => {
                    const parts = val.split("/");
                    if (parts.length === 3) {
                      setState((prev) => ({
                        ...prev,
                        agentKind: parts[0] as ScheduledRunTargetKind,
                        agentNamespace: parts[1],
                        agentName: parts[2],
                      }));
                    }
                  }}
                  disabled={isFormDisabled}
                >
                  <SelectTrigger
                    className={errors.agent ? "border-red-500" : ""}
                  >
                    <SelectValue placeholder="Select an agent" />
                  </SelectTrigger>
                  <SelectContent>
                    {agents.map((a) => {
                      const kind = getSchedulableAgentKind(a);
                      if (!kind) return null;
                      const ns = a.agent.metadata.namespace || "";
                      const n = a.agent.metadata.name;
                      const val = agentSelectValue(kind, ns, n);
                      return (
                        <SelectItem key={val} value={val}>
                          {ns}/{n} ({kind})
                        </SelectItem>
                      );
                    })}
                  </SelectContent>
                </Select>
                {errors.agent && (
                  <p className="text-red-500 text-sm mt-1">{errors.agent}</p>
                )}
              </div>

              <div>
                <Label className="text-base mb-2 block font-bold">Prompt</Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  The prompt message sent to the agent on each scheduled run.
                </p>
                <Textarea
                  value={state.prompt}
                  onChange={(e) =>
                    setState((prev) => ({ ...prev, prompt: e.target.value }))
                  }
                  placeholder="Enter the prompt for the agent..."
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
                    When enabled, automatic cron runs are paused.
                  </p>
                </div>
                <Switch
                  id="suspend-toggle"
                  checked={state.suspend}
                  onCheckedChange={(checked) =>
                    setState((prev) => ({ ...prev, suspend: checked }))
                  }
                  disabled={isFormDisabled}
                />
              </div>

              <div>
                <Label className="text-sm mb-2 block font-bold">
                  Max Run History
                </Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  Maximum number of run records to retain (1-100).
                </p>
                <Input
                  type="number"
                  min={1}
                  max={100}
                  value={state.maxRunHistory}
                  onChange={(e) => {
                    setState((prev) => ({ ...prev, maxRunHistory: e.target.value }));
                  }}
                  className={errors.maxRunHistory ? "border-red-500" : ""}
                  disabled={isFormDisabled}
                />
                {errors.maxRunHistory && (
                  <p className="text-red-500 text-sm mt-1">
                    {errors.maxRunHistory}
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
                  {isEditMode ? "Updating..." : "Creating..."}
                </>
              ) : isEditMode ? (
                "Update Schedule"
              ) : (
                "Create Schedule"
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
