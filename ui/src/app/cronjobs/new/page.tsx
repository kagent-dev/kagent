"use client";
import React, { useState, useEffect, Suspense } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Loader2 } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { LoadingState } from "@/components/LoadingState";
import { createCronJob, updateCronJob, getCronJob } from "@/app/actions/cronjobs";
import { getAgents } from "@/app/actions/agents";
import { toast } from "sonner";
import { isResourceNameValid } from "@/lib/utils";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { AgentResponse } from "@/types";

interface ValidationErrors {
  name?: string;
  namespace?: string;
  schedule?: string;
  agentRef?: string;
  prompt?: string;
}

function CronJobFormContent() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const isEditMode = searchParams.get("edit") === "true";
  const editName = searchParams.get("name");
  const editNamespace = searchParams.get("namespace");

  const [name, setName] = useState("");
  const [namespace, setNamespace] = useState("default");
  const [schedule, setSchedule] = useState("");
  const [agentRef, setAgentRef] = useState("");
  const [prompt, setPrompt] = useState("");
  const [agents, setAgents] = useState<AgentResponse[]>([]);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [errors, setErrors] = useState<ValidationErrors>({});

  useEffect(() => {
    const fetchData = async () => {
      setIsLoading(true);
      try {
        const agentsResponse = await getAgents();
        if (!agentsResponse.error && agentsResponse.data) {
          setAgents(agentsResponse.data);
        }

        if (isEditMode && editName && editNamespace) {
          const cronJobResponse = await getCronJob(editNamespace, editName);
          if (cronJobResponse.error || !cronJobResponse.data) {
            throw new Error(cronJobResponse.error || "Failed to fetch cron job");
          }
          const cronJob = cronJobResponse.data;
          setName(cronJob.metadata.name);
          setNamespace(cronJob.metadata.namespace || "default");
          setSchedule(cronJob.spec.schedule);
          setAgentRef(cronJob.spec.agentRef);
          setPrompt(cronJob.spec.prompt);
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : "Failed to load data";
        toast.error(errorMessage);
      } finally {
        setIsLoading(false);
      }
    };
    fetchData();
  }, [isEditMode, editName, editNamespace]);

  const validateForm = (): boolean => {
    const newErrors: ValidationErrors = {};

    if (!name.trim()) {
      newErrors.name = "Name is required";
    } else if (!isResourceNameValid(name)) {
      newErrors.name = "Name must be a valid RFC 1123 subdomain name";
    }

    if (!namespace.trim()) {
      newErrors.namespace = "Namespace is required";
    }

    if (!schedule.trim()) {
      newErrors.schedule = "Schedule is required";
    } else if (!/^\S+\s+\S+\s+\S+\s+\S+\s+\S+$/.test(schedule.trim())) {
      newErrors.schedule = "Schedule must be a valid 5-field cron expression (e.g. */5 * * * *)";
    }

    if (!agentRef.trim()) {
      newErrors.agentRef = "Agent is required";
    }

    if (!prompt.trim()) {
      newErrors.prompt = "Prompt is required";
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async () => {
    if (!validateForm()) {
      toast.error("Please fill in all required fields and correct any errors.");
      return;
    }

    setIsSubmitting(true);

    const cronJobPayload = {
      metadata: {
        name: name.trim(),
        namespace: namespace.trim(),
      },
      spec: {
        schedule: schedule.trim(),
        agentRef: agentRef.trim(),
        prompt: prompt.trim(),
      },
    };

    try {
      let response;
      if (isEditMode && editName && editNamespace) {
        response = await updateCronJob(editNamespace, editName, cronJobPayload);
      } else {
        response = await createCronJob(cronJobPayload);
      }

      if (response.error) {
        throw new Error(response.error);
      }

      toast.success(`Cron job ${isEditMode ? "updated" : "created"} successfully!`);
      router.push("/cronjobs");
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : `Failed to ${isEditMode ? "update" : "create"} cron job`;
      toast.error(errorMessage);
    } finally {
      setIsSubmitting(false);
    }
  };

  if (isLoading) {
    return <LoadingState />;
  }

  return (
    <div className="min-h-screen p-8">
      <div className="max-w-3xl mx-auto">
        <h1 className="text-2xl font-bold mb-8">
          {isEditMode ? "Edit Cron Job" : "Create New Cron Job"}
        </h1>

        <div className="space-y-6">
          <div>
            <Label className="text-base mb-2 block font-bold">Name</Label>
            <p className="text-xs mb-2 block text-muted-foreground">
              A unique name for this cron job. Must be a valid Kubernetes resource name.
            </p>
            <Input
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (errors.name) setErrors((prev) => ({ ...prev, name: undefined }));
              }}
              placeholder="e.g. daily-cluster-check"
              disabled={isSubmitting || isEditMode}
              className={errors.name ? "border-red-500" : ""}
            />
            {errors.name && <p className="text-red-500 text-sm mt-1">{errors.name}</p>}
          </div>

          <div>
            <Label className="text-base mb-2 block font-bold">Namespace</Label>
            <p className="text-xs mb-2 block text-muted-foreground">
              The Kubernetes namespace for the cron job.
            </p>
            <NamespaceCombobox
              value={namespace}
              onValueChange={(value) => {
                setNamespace(value);
                if (errors.namespace) setErrors((prev) => ({ ...prev, namespace: undefined }));
              }}
              disabled={isSubmitting || isEditMode}
            />
            {errors.namespace && <p className="text-red-500 text-sm mt-1">{errors.namespace}</p>}
          </div>

          <div>
            <Label className="text-base mb-2 block font-bold">Schedule</Label>
            <p className="text-xs mb-2 block text-muted-foreground">
              A standard 5-field cron expression: minute hour day month weekday.
            </p>
            <Input
              value={schedule}
              onChange={(e) => {
                setSchedule(e.target.value);
                if (errors.schedule) setErrors((prev) => ({ ...prev, schedule: undefined }));
              }}
              placeholder="*/5 * * * *"
              disabled={isSubmitting}
              className={errors.schedule ? "border-red-500" : ""}
            />
            {errors.schedule && <p className="text-red-500 text-sm mt-1">{errors.schedule}</p>}
            <p className="text-xs text-muted-foreground mt-1">
              Examples: <code>*/5 * * * *</code> (every 5 min), <code>0 9 * * *</code> (daily 9am), <code>0 0 * * 1</code> (weekly Monday midnight)
            </p>
          </div>

          <div>
            <Label className="text-base mb-2 block font-bold">Agent</Label>
            <p className="text-xs mb-2 block text-muted-foreground">
              The agent to invoke on each scheduled run. Must be in the same namespace.
            </p>
            <Select
              value={agentRef}
              onValueChange={(value) => {
                setAgentRef(value);
                if (errors.agentRef) setErrors((prev) => ({ ...prev, agentRef: undefined }));
              }}
              disabled={isSubmitting}
            >
              <SelectTrigger className={errors.agentRef ? "border-red-500" : ""}>
                <SelectValue placeholder="Select an agent" />
              </SelectTrigger>
              <SelectContent>
                {agents.map((agentResp) => {
                  const agentName = agentResp.agent.metadata.name;
                  const agentNs = agentResp.agent.metadata.namespace || "default";
                  return (
                    <SelectItem key={`${agentNs}/${agentName}`} value={agentName}>
                      {agentNs}/{agentName}
                    </SelectItem>
                  );
                })}
              </SelectContent>
            </Select>
            {errors.agentRef && <p className="text-red-500 text-sm mt-1">{errors.agentRef}</p>}
            {agents.length === 0 && !isLoading && (
              <p className="text-xs text-muted-foreground mt-1">
                No agents found. Create an agent first.
              </p>
            )}
          </div>

          <div>
            <Label className="text-base mb-2 block font-bold">Prompt</Label>
            <p className="text-xs mb-2 block text-muted-foreground">
              The message sent to the agent on each scheduled run.
            </p>
            <Textarea
              value={prompt}
              onChange={(e) => {
                setPrompt(e.target.value);
                if (errors.prompt) setErrors((prev) => ({ ...prev, prompt: undefined }));
              }}
              placeholder="Check the health of all pods in the cluster and report any issues."
              disabled={isSubmitting}
              className={`min-h-[120px] ${errors.prompt ? "border-red-500" : ""}`}
            />
            {errors.prompt && <p className="text-red-500 text-sm mt-1">{errors.prompt}</p>}
          </div>
        </div>

        <div className="flex justify-between pt-6">
          <Button
            variant="outline"
            onClick={() => router.push("/cronjobs")}
            disabled={isSubmitting}
          >
            Cancel
          </Button>
          <Button
            variant="default"
            onClick={handleSubmit}
            disabled={isSubmitting}
          >
            {isSubmitting ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                {isEditMode ? "Updating..." : "Creating..."}
              </>
            ) : isEditMode ? (
              "Update Cron Job"
            ) : (
              "Create Cron Job"
            )}
          </Button>
        </div>
      </div>
    </div>
  );
}

export default function CronJobFormPage() {
  return (
    <Suspense fallback={<LoadingState />}>
      <CronJobFormContent />
    </Suspense>
  );
}
