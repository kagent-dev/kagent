import React from 'react';
import { Button } from '@/components/ui/button';
import { CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
import { Separator } from "@/components/ui/separator";
import { Linkedin, X, CheckCircle2 } from 'lucide-react';
import { K8S_AGENT_DEFAULTS } from '../OnboardingWizard';


interface FinishStepProps {
    agentName?: string;
    onFinish: () => void;
    shareOnTwitter: (text: string) => void;
    shareOnLinkedIn: () => void;
}

export function FinishStep({ agentName, onFinish, shareOnTwitter, shareOnLinkedIn }: FinishStepProps) {
    const shareTextTemplate = `🎉 I deployed my first AI agent with kagent using model ${agentName || 'N/A'}. Create yours at https://kagent.dev!`;

    return (
        <>
            <CardHeader className="items-center text-center pt-10 pb-6 border-b">
                <CheckCircle2 className="h-16 w-16 text-green-500 mb-4" />
                <CardTitle className="text-3xl font-bold mb-2">Setup Complete!</CardTitle>
                <CardDescription className="text-lg">Your <span className="font-semibold text-primary">{agentName || K8S_AGENT_DEFAULTS.name}</span> is ready.</CardDescription>
            </CardHeader>
            <CardContent className="px-8 pt-8 pb-6 space-y-6">
                <div className="text-center space-y-3">
                    <h4 className='text-md font-medium'>Share Your Success!</h4>
                    <blockquote className="mt-1 border-l-2 pl-3 text-left text-sm text-foreground/80 bg-muted/50 p-3 rounded-md max-w-md mx-auto font-mono">
                        {shareTextTemplate}
                    </blockquote>
                    <div className="flex justify-center gap-3 pt-2">
                        <Button variant="outline" size="sm" onClick={() => shareOnTwitter(shareTextTemplate)}>
                            <X className="w-4 h-4 mr-2" /> Share on X
                        </Button>
                        <Button variant="outline" size="sm" onClick={shareOnLinkedIn}>
                            <Linkedin className="w-4 h-4 mr-2" /> Share on LinkedIn
                        </Button>
                    </div>
                </div>

                <Separator />

                <div className="bg-background rounded-lg p-4 space-y-3 text-center">
                    <h4 className='text-md font-medium'>Try Asking Your Agent</h4>
                    <ul className="space-y-1.5 text-sm text-muted-foreground text-left max-w-lg mx-auto list-disc list-inside">
                        <li>What pods are running in the default namespace?</li>
                        <li>List all available Deployments.</li>
                        <li>Get the details of the &apos;kagent&apos; service in the &apos;kagent&apos; namespace.</li>
                    </ul>
                </div>
            </CardContent>
            <CardFooter className="flex justify-center pb-8 pt-2">
                <Button
                    onClick={onFinish}
                    className="px-8 py-6 text-lg font-medium"
                    size="lg"
                >
                    Finish & Go to Agent
                </Button>
            </CardFooter>
        </>
    );
} 