import React from 'react';
import { Button } from '@/components/ui/button';
import { CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
// Branding updated: using text-based brand instead of logo component

interface WelcomeStepProps {
    onNext: () => void;
}

export function WelcomeStep({ onNext }: WelcomeStepProps) {
    return (
        <>
            <CardHeader className="items-center text-center pt-10 pb-6 border-b">
                <span className="text-3xl md:text-4xl font-semibold tracking-tight mb-6 block">
                    adolphe.ai
                </span>
                <CardTitle className="text-2xl mb-2">Bringing <span className="font-semibold text-primary">Agentic AI</span> to Cloud Native</CardTitle>
            </CardHeader>

            <CardContent className="px-8 pt-8 pb-6">
                <div className="max-w-md mx-auto space-y-6">
                    <div className=" space-y-4">
                        <p className="text-lg">
                            Let&apos;s get you started by creating your first agent: <br />a handy{" "}
                            <span className="font-semibold">Kubernetes Assistant</span>!
                        </p>
                    </div>

                    <div className="bg-muted/50 rounded-lg p-5 mt-8">
                        <h3 className="font-medium mb-3 text-center">This wizard will guide you through:</h3>
                        <ul className="space-y-2.5">
                            <li className="flex items-start">
                                <div className="flex items-center justify-center w-6 h-6 bg-primary/10 rounded-full mr-3 flex-shrink-0 mt-0.5">
                                    <span className="text-primary text-sm font-medium">1</span>
                                </div>
                                <span>Creating a preferred AI model configuration</span>
                            </li>
                            <li className="flex items-start">
                                <div className="flex items-center justify-center w-6 h-6 bg-primary/10 rounded-full mr-3 flex-shrink-0 mt-0.5">
                                    <span className="text-primary text-sm font-medium">2</span>
                                </div>
                                <span>Coming up with agent instructions</span>
                            </li>
                            <li className="flex items-start">
                                <div className="flex items-center justify-center w-6 h-6 bg-primary/10 rounded-full mr-3 flex-shrink-0 mt-0.5">
                                    <span className="text-primary text-sm font-medium">3</span>
                                </div>
                                <span>Selecting the right tools for your agent</span>
                            </li>
                        </ul>
                    </div>
                </div>
            </CardContent>

            <CardFooter className="flex justify-center pb-8 pt-2">
                <Button
                    onClick={onNext}
                    className="px-8 py-6 text-lg font-medium"
                    size="lg"
                >
                    Let&apos;s Get Started
                </Button>
            </CardFooter>
        </>
    );
} 