"use client";

import React, { useState } from 'react';
import { Card } from '@/components/ui/card';
import { toast } from 'sonner';
import { useAgents, AgentFormData } from "@/components/AgentsProvider";
import type { Tool } from "@/types";
import { WelcomeStep } from './steps/WelcomeStep';
import { ModelConfigStep } from './steps/ModelConfigStep';
import { AgentSetupStep, AgentSetupFormData } from './steps/AgentSetupStep';
import { ToolSelectionStep } from './steps/ToolSelectionStep';
import { ReviewStep } from './steps/ReviewStep';
import { FinishStep } from './steps/FinishStep';
import { k8sRefUtils } from '@/lib/k8sUtils';

interface OnboardingWizardProps {
  onOnboardingComplete: () => void;
  onSkip: () => void;
}

interface OnboardingStateData {
    modelConfigRef?: string;
    modelName?: string;
    agentRef?: string;
    agentDescription?: string;
    agentInstructions?: string;
    selectedTools?: Tool[];
}

export const K8S_AGENT_DEFAULTS = {
    name: "my-first-k8s-agent",
    namespace: "default",
    description: "This agent can interact with the Kubernetes API to get information about the cluster.",
    instructions: `You're a friendly and helpful agent that uses Kubernetes tools to answer users questions about the cluster.

# Instructions
- If user question is unclear, ask for clarification before running any tools
- Always be helpful and friendly
- If you don't know how to answer the question DO NOT make things up
  respond with "Sorry, I don't know how to answer that" and ask the user to further clarify the question
  If you are unable to help, or something goes wrong, refer the user to https://kagent.dev for more information or support.

# Response format
- ALWAYS format your response as Markdown
- Your response will include a summary of actions you took and an explanation of the result`
};

export function OnboardingWizard({ onOnboardingComplete, onSkip }: OnboardingWizardProps) {
  const [currentStep, setCurrentStep] = useState(0);
  const [isLoading, setIsLoading] = useState(false);
  const [onboardingData, setOnboardingData] = useState<OnboardingStateData>({
      agentRef: k8sRefUtils.toRef(K8S_AGENT_DEFAULTS.namespace, K8S_AGENT_DEFAULTS.name),
      agentDescription: K8S_AGENT_DEFAULTS.description,
      agentInstructions: K8S_AGENT_DEFAULTS.instructions,
      selectedTools: [],
  });

  const {
      models: existingModels,
      loading: loadingExistingModels,
      error: errorExistingModels,
      createNewAgent,
      tools: availableTools,
      loading: loadingTools,
      error: errorTools
    } = useAgents();

  const handleNextFromWelcome = () => {
      setCurrentStep(1);
  };

  const handleNextFromModelConfig = (modelConfigRef: string, modelName: string) => {
      setOnboardingData(prev => ({
          ...prev,
          modelConfigRef: modelConfigRef,
          modelName: modelName
      }));
      setCurrentStep(2);
  };

  const handleNextFromAgentSetup = (data: AgentSetupFormData) => {
      setOnboardingData(prev => ({
          ...prev,
          agentRef: k8sRefUtils.toRef(data.agentNamespace || K8S_AGENT_DEFAULTS.namespace, data.agentName),
          agentDescription: data.agentDescription,
          agentInstructions: data.agentInstructions,
      }));
      setCurrentStep(3);
  };

  const handleNextFromToolSelection = (selectedTools: Tool[]) => {
      setOnboardingData(prev => ({
          ...prev,
          selectedTools: selectedTools,
      }));
      setCurrentStep(4);
  };

  const handleBack = () => {
      setCurrentStep(prev => Math.max(0, prev - 1));
  };

  const handleFinalSubmit = async () => {
      if (!onboardingData.modelConfigRef || !onboardingData.agentRef || !onboardingData.agentInstructions) {
          toast.error("Some agent details are missing. Please review previous steps.");
          return;
      }
      setIsLoading(true);
      try {
          const agentRef = k8sRefUtils.fromRef(onboardingData.agentRef);
          const agentPayload: AgentFormData = {
              name: agentRef.name,
              namespace: agentRef.namespace,
              description: onboardingData.agentDescription || "",
              systemPrompt: onboardingData.agentInstructions,
              modelName: onboardingData.modelConfigRef || "",
              tools: onboardingData.selectedTools || [],
          };
          const result = await createNewAgent(agentPayload);
          if (!result.error) {
              toast.success(`Agent '${onboardingData.agentRef}' created successfully!`);
              setCurrentStep(5);
          } else {
              const errorMessage = result.error || 'Failed to create agent.';
              throw new Error(errorMessage);
          }
      } catch (error) {
          console.error("Error creating agent:", error);
          toast.error(error instanceof Error ? error.message : String(error));
      } finally {
          setIsLoading(false);
      }
  };

  const handleFinish = () => {
      onOnboardingComplete();
  };

  const shareOnTwitter = (text: string) => {
    const kagentUrl = "https://kagent.dev";
    const twitterUrl = `https://twitter.com/intent/tweet?text=${encodeURIComponent(text)}&url=${encodeURIComponent(kagentUrl)}`;
    window.open(twitterUrl, '_blank', 'noopener,noreferrer');
  };

  const shareOnLinkedIn = () => {
     const kagentUrl = "https://kagent.dev";
     const linkedInUrl = `https://www.linkedin.com/sharing/share-offsite/?url=${encodeURIComponent(kagentUrl)}`;
     window.open(linkedInUrl, '_blank', 'noopener,noreferrer');
  };

  const renderCurrentStep = () => {
      switch (currentStep) {
          case 0:
              return <WelcomeStep onNext={handleNextFromWelcome} />;
          case 1:
              return <ModelConfigStep
                          existingModels={existingModels}
                          loadingExistingModels={loadingExistingModels}
                          errorExistingModels={errorExistingModels}
                          onNext={handleNextFromModelConfig}
                          onBack={handleBack}
                     />;
          case 2:
              return <AgentSetupStep
                          initialData={{
                              agentName: k8sRefUtils.fromRef(onboardingData.agentRef || "").name,
                              agentNamespace: k8sRefUtils.fromRef(onboardingData.agentRef || "").namespace,
                              agentDescription: onboardingData.agentDescription,
                              agentInstructions: onboardingData.agentInstructions
                          }}
                          onNext={handleNextFromAgentSetup}
                          onBack={handleBack}
                     />;
          case 3:
              return <ToolSelectionStep
                          availableTools={availableTools}
                          loadingTools={loadingTools}
                          errorTools={errorTools}
                          initialSelectedTools={onboardingData.selectedTools || []}
                          onNext={handleNextFromToolSelection}
                          onBack={handleBack}
                      />;
          case 4:
              return <ReviewStep
                          onboardingData={onboardingData}
                          isLoading={isLoading}
                          onBack={handleBack}
                          onSubmit={handleFinalSubmit}
                     />;
          case 5:
              return <FinishStep
                          agentName={onboardingData.agentRef}
                          onFinish={handleFinish}
                          shareOnTwitter={shareOnTwitter}
                          shareOnLinkedIn={shareOnLinkedIn}
                     />;
          default:
              return <WelcomeStep onNext={handleNextFromWelcome} />;
      }
  };

  return (
    <div className="flex items-center justify-center min-h-screen bg-background p-4">
      <Card className="w-full max-w-2xl relative">
        {renderCurrentStep()}
        <div className="flex justify-between items-center px-6 pb-4 pt-2 w-full">
          <button
            onClick={onSkip}
            className="text-sm text-muted-foreground hover:text-primary underline cursor-pointer"
          >
            Skip wizard
          </button>
        </div>
      </Card>
    </div>
  );
}
