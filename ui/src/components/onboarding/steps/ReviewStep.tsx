import React from 'react';
import { Button } from '@/components/ui/button';
import { CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
import { Textarea } from "@/components/ui/textarea";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Badge } from "@/components/ui/badge";
import { Loader2, FunctionSquare } from 'lucide-react';
import { AgentTool } from "@/types/datamodel";
import { getToolDisplayName, getToolIdentifier } from "@/lib/toolUtils";
import { K8S_AGENT_DEFAULTS } from '../OnboardingWizard';

interface OnboardingDataForReview {
    agentName?: string;
    agentDescription?: string;
    agentInstructions?: string;
    modelConfigName?: string;
    modelName?: string;
    selectedTools?: AgentTool[];
}

interface ReviewStepProps {
    onboardingData: OnboardingDataForReview;
    isLoading: boolean;
    onBack: () => void;
    onSubmit: () => void;
}

export function ReviewStep({ onboardingData, isLoading, onBack, onSubmit }: ReviewStepProps) {
    return (
        <>
            <CardHeader className="pt-8 pb-4 border-b">
                <CardTitle className="text-2xl">Step 4: Review Agent Configuration</CardTitle>
                <CardDescription className="text-md">Review your <span className="font-semibold">{onboardingData.agentName || K8S_AGENT_DEFAULTS.name}</span> configuration before creating it.</CardDescription>
            </CardHeader>
            <CardContent className="px-8 pt-6 pb-6 space-y-6">
                <div className="space-y-3">
                    <h3 className="font-semibold text-lg mb-2">Agent Details</h3>
                    <div className="grid grid-cols-3 gap-x-4 gap-y-2 text-sm">
                        <dt className="text-muted-foreground font-medium col-span-1">Name:</dt>
                        <dd className="col-span-2">{onboardingData.agentName || "(Not set)"}</dd>

                        <dt className="text-muted-foreground font-medium col-span-1">Description:</dt>
                        <dd className="col-span-2">{onboardingData.agentDescription || <span className="italic text-muted-foreground">(None provided)</span>}</dd>
                    </div>
                </div>

                <Separator />

                <div className="space-y-3">
                    <h3 className="font-semibold text-lg mb-2">Model Configuration</h3>
                    <div className="grid grid-cols-3 gap-x-4 gap-y-2 text-sm">
                        <dt className="text-muted-foreground font-medium col-span-1">Config Name:</dt>
                        <dd className="col-span-2">{onboardingData.modelConfigName || "(Not set)"}</dd>

                        <dt className="text-muted-foreground font-medium col-span-1">Model:</dt>
                        <dd className="col-span-2">{onboardingData.modelName || "(Not set)"}</dd> {/* Display model name */}
                    </div>
                </div>

                <Separator />

                <div className="space-y-2">
                    <h3 className="font-semibold text-lg">Instructions (System Prompt)</h3>
                    <Textarea
                        readOnly
                        value={onboardingData.agentInstructions || "(Not set)"}
                        className="min-h-[150px] bg-muted/50 text-muted-foreground text-xs font-mono border rounded-md p-3"
                    />
                </div>

                <Separator />

                <div className="space-y-2">
                    <h3 className="font-semibold text-lg">Selected Tools</h3>
                    {onboardingData.selectedTools && onboardingData.selectedTools.length > 0 ? (
                        <ScrollArea className="h-[100px] w-full rounded-md border p-3 bg-muted/50">
                            <div className="flex flex-wrap gap-2">
                                {onboardingData.selectedTools.map(tool => (
                                    <Badge variant="secondary" key={getToolIdentifier(tool)} className="flex items-center gap-1">
                                        <FunctionSquare className="h-3 w-3" />
                                        {getToolDisplayName(tool)}
                                    </Badge>
                                ))}
                            </div>
                        </ScrollArea>
                    ) : (
                        <p className="text-sm text-muted-foreground italic bg-muted/50 border rounded-md p-3">(No tools selected)</p>
                    )}
                </div>
            </CardContent>
            <CardFooter className="flex justify-between items-center pb-8 pt-2">
                <Button variant="outline" onClick={onBack} disabled={isLoading}>Back</Button>
                <Button onClick={onSubmit} disabled={isLoading}>
                    {isLoading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                    Create {onboardingData.agentName || K8S_AGENT_DEFAULTS.name} & Finish
                </Button>
            </CardFooter>
        </>
    );
} 