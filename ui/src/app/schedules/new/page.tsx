"use client";

import React, { useState, useEffect } from "react";
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
import { ScheduledRun, ConcurrencyPolicy, AgentResponse } from "@/types";
import {
  createScheduledRun,
  updateScheduledRun,
  getScheduledRun,
} from "@/app/actions/scheduledRuns";
import { getAgents } from "@/app/actions/agents";
import { LoadingState } from "@/components/LoadingState";
import { toast } from "sonner";

interface FormState {
  name: string;
  namespace: string;
  schedule: string;
  agentName: string;
  agentNamespace: string;
  prompt: string;
  suspend: boolean;
  concurrencyPolicy: ConcurrencyPolicy;
  maxRunHistory: number;
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

const RFC1123_REGEX = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;
const CRON_FIELD_COUNT = 5;

function validateCronExpression(expr: string): string | undefined {
  const trimmed = expr.trim();
  if (!trimmed) return "Schedule is required";
  const fields = trimmed.split(/\s+/);
  if (fields.length !== CRON_FIELD_COUNT) {
    return `Cron expression must have exactly ${CRON_FIELD_COUNT} fields (minute hour day month weekday)`;
  }

  // Minimum frequency check: reject schedules that fire more often than once per hour
  const [minute, hour] = fields;
  if (minute === "*" && hour === "*") {
    return "Schedule frequency too high: minimum interval is 1 hour";
  }
  if (minute.startsWith("*/") && hour === "*") {
    const interval = parseInt(minute.slice(2), 10);
    if (!isNaN(interval) && interval < 60) {
      return "Schedule frequency too high: minimum interval is 1 hour";
    }
  }

  return undefined;
}

function describeNextRuns(expr: string, count: number): string[] {
  // Simple heuristic preview for basic cron expressions
  // Full cron parsing would need a library; we show the raw expression instead
  const trimmed = expr.trim();
  const fields = trimmed.split(/\s+/);
  if (fields.length !== CRON_FIELD_COUNT) return [];

  const descriptions: string[] = [];
  const [minute, hour, dom, month, dow] = fields;

  // Build a human-readable hint
  let desc = "";
  if (minute === "*" && hour === "*") {
    desc = "Every minute";
  } else if (minute.startsWith("*/")) {
    desc = `Every ${minute.slice(2)} minutes`;
  } else if (hour === "*") {
    desc = `At minute ${minute} of every hour`;
  } else if (dom === "*" && month === "*" && dow === "*") {
    desc = `Daily at ${hour.padStart(2, "0")}:${minute.padStart(2, "0")}`;
  } else if (dow !== "*" && dom === "*" && month === "*") {
    const dayNames: Record<string, string> = { "0": "Sun", "1": "Mon", "2": "Tue", "3": "Wed", "4": "Thu", "5": "Fri", "6": "Sat", "7": "Sun" };
    const days = dow.split(",").map((d) => dayNames[d] || d).join(", ");
    desc = `At ${hour.padStart(2, "0")}:${minute.padStart(2, "0")} on ${days}`;
  } else {
    desc = `Cron: ${trimmed}`;
  }

  descriptions.push(desc);

  // Add note about count
  if (descriptions.length > 0 && count > 1) {
    descriptions.push(`(${CRON_FIELD_COUNT}-field cron: min hour dom month dow)`);
  }

  return descriptions.slice(0, count);
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
    namespace: "default",
    schedule: "",
    agentName: "",
    agentNamespace: "",
    prompt: "",
    suspend: false,
    concurrencyPolicy: "Forbid",
    maxRunHistory: 10,
    isSubmitting: false,
    isLoading: isEditMode,
  });
  const [errors, setErrors] = useState<ValidationErrors>({});

  // Fetch agents list
  useEffect(() => {
    const loadAgents = async () => {
      try {
        const response = await getAgents();
        if (response.data) {
          setAgents(response.data);
        }
      } catch (err) {
        console.error("Failed to load agents:", err);
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
          if (!response.data) {
            toast.error("Scheduled run not found");
            setState((prev) => ({ ...prev, isLoading: false }));
            return;
          }
          const sr = response.data;
          setState((prev) => ({
            ...prev,
            name: sr.metadata.name,
            namespace: sr.metadata.namespace || "",
            schedule: sr.spec.schedule,
            agentName: sr.spec.agentRef.name,
            agentNamespace: sr.spec.agentRef.namespace || "",
            prompt: sr.spec.prompt,
            suspend: sr.spec.suspend ?? false,
            concurrencyPolicy: sr.spec.concurrencyPolicy || "Forbid",
            maxRunHistory: sr.spec.maxRunHistory ?? 10,
            isLoading: false,
          }));
        } catch (err) {
          console.error("Error fetching scheduled run:", err);
          toast.error("Failed to load scheduled run data");
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
    } else if (!RFC1123_REGEX.test(state.name)) {
      newErrors.name = "Name must be a valid RFC 1123 label (lowercase alphanumeric and hyphens, max 63 chars)";
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

    if (state.maxRunHistory < 1 || state.maxRunHistory > 100) {
      newErrors.maxRunHistory = "Must be between 1 and 100";
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async () => {
    if (!validateForm()) return;

    setState((prev) => ({ ...prev, isSubmitting: true }));

    try {
      const sr: ScheduledRun = {
        apiVersion: "kagent.dev/v1alpha2",
        kind: "ScheduledRun",
        metadata: {
          name: state.name,
          namespace: state.namespace,
        },
        spec: {
          schedule: state.schedule.trim(),
          agentRef: {
            name: state.agentName,
            namespace: state.agentNamespace || undefined,
          },
          prompt: state.prompt,
          suspend: state.suspend,
          concurrencyPolicy: state.concurrencyPolicy,
          maxRunHistory: state.maxRunHistory,
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
  const cronPreview = state.schedule.trim() ? describeNextRuns(state.schedule, 3) : [];

  if (state.isSubmitting) {
    return <LoadingState />;
  }

  return (
    <div className="min-h-screen p-8">
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
                  Unique identifier for this scheduled run (RFC 1123 compliant).
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
                    setState((prev) => ({ ...prev, namespace: value }))
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
                {cronPreview.length > 0 && !errors.schedule && (
                  <div className="mt-2 text-xs text-muted-foreground space-y-0.5">
                    {cronPreview.map((line, i) => (
                      <p key={i}>{line}</p>
                    ))}
                  </div>
                )}
              </div>

              <div>
                <Label className="text-base mb-2 block font-bold">Agent</Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  Select the agent to run on this schedule.
                </p>
                <Select
                  value={
                    state.agentName
                      ? `${state.agentNamespace}/${state.agentName}`
                      : ""
                  }
                  onValueChange={(val) => {
                    const parts = val.split("/");
                    if (parts.length === 2) {
                      setState((prev) => ({
                        ...prev,
                        agentNamespace: parts[0],
                        agentName: parts[1],
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
                      const ns = a.agent.metadata.namespace || "";
                      const n = a.agent.metadata.name;
                      const val = `${ns}/${n}`;
                      return (
                        <SelectItem key={val} value={val}>
                          {val}
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
                    When enabled, no new runs will be triggered by this schedule.
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
                  Concurrency Policy
                </Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  Controls what happens when a new run is triggered while a previous one is still active.
                </p>
                <Select
                  value={state.concurrencyPolicy}
                  onValueChange={(val) =>
                    setState((prev) => ({
                      ...prev,
                      concurrencyPolicy: val as ConcurrencyPolicy,
                    }))
                  }
                  disabled={isFormDisabled}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="Forbid">
                      Forbid - Skip new run if previous is still active
                    </SelectItem>
                    <SelectItem value="Allow">
                      Allow - Run concurrently
                    </SelectItem>
                    <SelectItem value="Replace">
                      Replace - Cancel active run and start new one
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div>
                <Label className="text-sm mb-2 block font-bold">
                  Max Run History
                </Label>
                <p className="text-xs mb-2 block text-muted-foreground">
                  Number of completed run records to retain (1-100).
                </p>
                <Input
                  type="number"
                  min={1}
                  max={100}
                  value={state.maxRunHistory}
                  onChange={(e) => {
                    const val = parseInt(e.target.value, 10);
                    if (!isNaN(val)) {
                      setState((prev) => ({ ...prev, maxRunHistory: val }));
                    }
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
