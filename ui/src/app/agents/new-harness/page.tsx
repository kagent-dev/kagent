"use client";

import React, { Suspense, useCallback, useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { defaultOpenClawSandboxFormSlice, type OpenClawSandboxFormSlice } from "@/lib/openClawSandboxForm";
import type { ModelConfig } from "@/types";
import { ModelSelectionSection } from "@/components/create/ModelSelectionSection";
import { useRouter } from "next/navigation";
import { useAgents } from "@/components/AgentsProvider";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import type { AgentFormData } from "@/components/AgentsProvider";
import { toast } from "sonner";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { FormSection, FieldRoot, FieldLabel, FieldError } from "@/components/agent-form/form-primitives";
import { OpenClawSandboxFields } from "@/components/agent-form/OpenClawSandboxFields";
import type { AgentFormValidationErrors } from "@/components/agent-form/agent-form-types";
import { focusFirstFormError } from "@/components/agent-form/focusFirstFormError";
import { PageHeader } from "@/components/layout/PageHeader";

const HARNESS_OPTIONS = [{ value: "nemoclaw-openclaw", label: "NemoClaw (OpenClaw)" }] as const;

function AgentHarnessPageContent() {
  const router = useRouter();
  const { models, loading, error, createNewAgent, validateAgentData } = useAgents();

  type SelectedModelType = ModelConfig;

  interface FormState {
    name: string;
    namespace: string;
    description: string;
    harnessType: (typeof HARNESS_OPTIONS)[number]["value"];
    selectedModel: SelectedModelType | null;
    openClaw: OpenClawSandboxFormSlice;
    isSubmitting: boolean;
    errors: AgentFormValidationErrors;
  }

  const [formDirty, setFormDirty] = useState(false);

  const [state, setState] = useState<FormState>({
    name: "",
    namespace: "default",
    description: "",
    harnessType: HARNESS_OPTIONS[0].value,
    selectedModel: null,
    openClaw: defaultOpenClawSandboxFormSlice(),
    isSubmitting: false,
    errors: {},
  });

  const disabled = state.isSubmitting;

  useEffect(() => {
    if (!formDirty) {
      return;
    }
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [formDirty]);

  const validateForm = () => {
    if (state.harnessType !== "nemoclaw-openclaw") {
      toast.error("Unsupported harness type.");
      return false;
    }

    const formData: Partial<AgentFormData> = {
      name: state.name,
      namespace: state.namespace,
      description: state.description,
      type: "OpenClawSandbox",
      modelName: state.selectedModel?.ref || "",
      openClawSandbox: state.openClaw,
    };

    const newErrors = validateAgentData(formData);
    setState((prev) => ({ ...prev, errors: newErrors }));
    const valid = Object.keys(newErrors).length === 0;
    if (!valid) {
      requestAnimationFrame(() => {
        focusFirstFormError(newErrors, { byoSectionsActive: false });
      });
    }
    return valid;
  };

  const validateField = useCallback(
    (fieldName: keyof AgentFormValidationErrors, value: string) => {
      const formData: Partial<AgentFormData> = {
        type: "OpenClawSandbox",
        openClawSandbox: state.openClaw,
      };

      switch (fieldName) {
        case "name":
          formData.name = value;
          break;
        case "namespace":
          formData.namespace = value;
          break;
        case "description":
          formData.description = value;
          break;
        case "model":
          formData.modelName = value;
          break;
        default:
          return;
      }

      const fieldErrors = validateAgentData(formData);
      const valueForField = (fieldErrors as Record<string, string | undefined>)[fieldName as string];
      setState((prev) => {
        const nextErrors: AgentFormValidationErrors = {
          ...prev.errors,
          [fieldName]: valueForField,
        };
        nextErrors.openClawSandbox = fieldErrors.openClawSandbox;
        return { ...prev, errors: nextErrors };
      });
    },
    [state.openClaw, validateAgentData],
  );

  const handleSaveAgent = async () => {
    if (!validateForm()) {
      return;
    }

    if (state.harnessType !== "nemoclaw-openclaw") {
      toast.error("Unsupported harness type.");
      return;
    }

    try {
      setState((prev) => ({ ...prev, isSubmitting: true }));

      if (!state.selectedModel?.ref) {
        throw new Error("Model config is required for this harness.");
      }

      const ocPayload: AgentFormData = {
        name: state.name,
        namespace: state.namespace,
        description: state.description,
        type: "OpenClawSandbox",
        tools: [],
        modelName: state.selectedModel.ref,
        openClawSandbox: state.openClaw,
      };
      const ocResult = await createNewAgent(ocPayload);
      if (ocResult.error) {
        throw new Error(ocResult.error);
      }
      setFormDirty(false);
      router.push(`/agents`);
    } catch (e) {
      console.error("Error creating agent harness:", e);
      const errorMessage = e instanceof Error ? e.message : "Failed to create agent harness. Please try again.";
      toast.error(errorMessage);
      setState((prev) => ({ ...prev, isSubmitting: false }));
    }
  };

  const renderPageContent = () => {
    if (error) {
      return <ErrorState message={error} />;
    }

    return (
      <div className="relative min-h-screen touch-manipulation bg-gradient-to-b from-background via-background to-muted/15">
        <a
          href="#agent-harness-form-main"
          className="absolute -left-full top-0 z-[100] whitespace-nowrap p-2 text-sm text-primary focus:left-4 focus:top-4 focus:rounded-md focus:bg-primary focus:px-3 focus:py-2 focus:text-primary-foreground"
        >
          Skip to form
        </a>
        <div className="mx-auto max-w-3xl px-4 py-10 sm:px-6">
          <PageHeader titleId="agent-harness-form-page-title" title="New Agent Harness" />

          <main
            id="agent-harness-form-main"
            className="scroll-mt-8 outline-none"
            tabIndex={-1}
            aria-labelledby="agent-harness-form-page-title"
          >
            <form
              className="space-y-8"
              noValidate
              onInput={() => {
                if (!formDirty) {
                  setFormDirty(true);
                }
              }}
              onSubmit={(e) => {
                e.preventDefault();
                void handleSaveAgent();
              }}
            >
              <p className="sr-only" role="status" aria-live="polite" aria-atomic>
                {state.isSubmitting ? "Creating…" : ""}
              </p>

              <FormSection
                title="Identity"
                description="Name, namespace, harness type, and a short note shown in the agents list."
              >
                <FieldRoot>
                  <FieldLabel htmlFor="agent-harness-field-name">Agent name</FieldLabel>
                  <Input
                    id="agent-harness-field-name"
                    name="agentName"
                    value={state.name}
                    onChange={(e) => setState((prev) => ({ ...prev, name: e.target.value }))}
                    onBlur={() => validateField("name", state.name)}
                    className={state.errors.name ? "border-destructive" : ""}
                    placeholder="e.g. my-openclaw-bot"
                    autoComplete="off"
                    spellCheck={false}
                    translate="no"
                    disabled={disabled}
                    aria-invalid={!!state.errors.name}
                  />
                  <FieldError>{state.errors.name}</FieldError>
                </FieldRoot>

                <FieldRoot>
                  <FieldLabel htmlFor="agent-harness-field-namespace">Namespace</FieldLabel>
                  <NamespaceCombobox
                    id="agent-harness-field-namespace"
                    value={state.namespace}
                    onValueChange={(value) => {
                      setState((prev) => ({ ...prev, selectedModel: null, namespace: value }));
                      validateField("namespace", value);
                    }}
                    disabled={disabled}
                  />
                </FieldRoot>

                <FieldRoot>
                  <FieldLabel htmlFor="agent-harness-field-type">Harness type</FieldLabel>
                  <Select
                    value={state.harnessType}
                    onValueChange={(val) =>
                      setState((prev) => ({
                        ...prev,
                        harnessType: val as FormState["harnessType"],
                      }))
                    }
                    disabled={disabled}
                  >
                    <SelectTrigger id="agent-harness-field-type" className="w-full">
                      <SelectValue placeholder="Select harness…" />
                    </SelectTrigger>
                    <SelectContent>
                      {HARNESS_OPTIONS.map((opt) => (
                        <SelectItem key={opt.value} value={opt.value}>
                          {opt.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </FieldRoot>

                <FieldRoot>
                  <FieldLabel htmlFor="agent-harness-desc">Description (optional)</FieldLabel>
                  <Textarea
                    id="agent-harness-desc"
                    name="description"
                    value={state.description}
                    onChange={(e) => setState((prev) => ({ ...prev, description: e.target.value }))}
                    onBlur={() => validateField("description", state.description)}
                    className={`min-h-[96px] ${state.errors.description ? "border-destructive" : ""}`}
                    placeholder="What this harness is for…"
                    autoComplete="off"
                    disabled={disabled}
                    aria-invalid={!!state.errors.description}
                  />
                  <FieldError>{state.errors.description}</FieldError>
                </FieldRoot>
              </FormSection>

              <FormSection
                title="Model & behavior"
                description="Model for this harness."
              >
                <ModelSelectionSection
                  allModels={models}
                  selectedModel={state.selectedModel}
                  setSelectedModel={(model) => {
                    setState((prev) => ({ ...prev, selectedModel: model as ModelConfig | null }));
                  }}
                  error={state.errors.model}
                  isSubmitting={disabled}
                  onChange={(modelRef) => validateField("model", modelRef)}
                  agentNamespace={state.namespace}
                />
              </FormSection>

              <OpenClawSandboxFields
                value={state.openClaw}
                onChange={(openClaw) =>
                  setState((prev) => ({
                    ...prev,
                    openClaw,
                    errors: { ...prev.errors, openClawSandbox: undefined },
                  }))
                }
                disabled={disabled}
                validationError={state.errors.openClawSandbox}
              />

              <div className="flex justify-end border-t border-border/50 pt-6">
                <Button type="submit" size="lg" disabled={disabled} className="min-w-[10rem]" aria-busy={state.isSubmitting}>
                  {state.isSubmitting ? (
                    <>
                      <Loader2 className="mr-2 h-4 w-4 shrink-0 animate-spin" aria-hidden />
                      Creating…
                    </>
                  ) : (
                    "Create harness"
                  )}
                </Button>
              </div>
            </form>
          </main>
        </div>
      </div>
    );
  };

  return (
    <>
      {loading && <LoadingState />}
      {renderPageContent()}
    </>
  );
}

export default function NewAgentHarnessPage() {
  return (
    <Suspense fallback={<LoadingState />}>
      <AgentHarnessPageContent />
    </Suspense>
  );
}
