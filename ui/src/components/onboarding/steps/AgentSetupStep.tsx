import React from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import * as z from 'zod';
import { Button } from '@/components/ui/button';
import { CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { K8S_AGENT_DEFAULTS } from '../OnboardingWizard';

const agentSetupSchema = z.object({
    agentName: z.string().min(1, "Agent name is required."),
    agentDescription: z.string().optional(),
    agentInstructions: z.string().min(10, "Instructions should be at least 10 characters long."),
});
export type AgentSetupFormData = z.infer<typeof agentSetupSchema>;

interface AgentSetupStepProps {
    initialData: {
        agentName?: string;
        agentDescription?: string;
        agentInstructions?: string;
    };
    onNext: (data: AgentSetupFormData) => void;
    onBack: () => void;
}

export function AgentSetupStep({ initialData, onNext, onBack }: AgentSetupStepProps) {
    const form = useForm<AgentSetupFormData>({
        resolver: zodResolver(agentSetupSchema),
        defaultValues: {
            agentName: initialData.agentName || K8S_AGENT_DEFAULTS.name,
            agentDescription: initialData.agentDescription || K8S_AGENT_DEFAULTS.description,
            agentInstructions: initialData.agentInstructions || K8S_AGENT_DEFAULTS.instructions,
        },
        // Ensure form reflects current state if user goes back and forth
        values: {
            agentName: initialData.agentName || K8S_AGENT_DEFAULTS.name,
            agentDescription: initialData.agentDescription || K8S_AGENT_DEFAULTS.description,
            agentInstructions: initialData.agentInstructions || K8S_AGENT_DEFAULTS.instructions,
        }
    });

    function onSubmit(values: AgentSetupFormData) {
        onNext(values);
    }

    return (
        <Form {...form}>
            <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-0">
                <CardHeader className="pt-8 pb-4 border-b">
                    <CardTitle className="text-2xl">Step 2: Set Up The AI Agent</CardTitle>
                    <CardDescription className="text-md">
                        Configure the name, description, and instructions for our Kubernetes assistant.
                    </CardDescription>
                </CardHeader>
                <CardContent className="px-8 pt-6 pb-6 space-y-4"> {/* Add spacing here */}
                    <FormField
                        control={form.control}
                        name="agentName"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Agent Name</FormLabel>
                                <FormControl>
                                    <Input {...field} />
                                </FormControl>
                                <FormDescription>A unique name for your agent.</FormDescription>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                    <FormField
                        control={form.control}
                        name="agentDescription"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Description</FormLabel>
                                <FormControl>
                                    <Input {...field} />
                                </FormControl>
                                <FormDescription>A brief summary of what this agent does (optional).</FormDescription>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                    <FormField
                        control={form.control}
                        name="agentInstructions"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Instructions (System Prompt)</FormLabel>
                                <FormControl>
                                    <Textarea
                                        className="resize-y min-h-[200px] font-mono text-xs"
                                        {...field}
                                    />
                                </FormControl>
                                <FormDescription>
                                    These instructions guide the agent. We&apos;re starting with basic defaults, but you can modify them. Read more <a href="https://kagent.dev/docs/getting-started/system-prompts" target="_blank" rel="noopener noreferrer" className="text-primary underline">here</a>.
                                </FormDescription>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                </CardContent>
                <CardFooter className="flex justify-between items-center pb-8 pt-2">
                    <Button variant="outline" type="button" onClick={onBack}>Back</Button>
                    <Button type="submit">Next: Select Tools</Button>
                </CardFooter>
            </form>
        </Form>
    );
} 