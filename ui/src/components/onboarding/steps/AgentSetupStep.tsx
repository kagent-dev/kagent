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
import { Switch } from '@/components/ui/switch';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';

const agentSetupSchema = z.object({
    agentName: z.string().min(1, "Agent name is required."),
    agentDescription: z.string().optional(),
    agentInstructions: z.string().min(10, "Instructions should be at least 10 characters long."),
    a2aAuth: z.object({
        enabled: z.boolean().default(false),
        type: z.enum(["jwt", "apiKey", "none"]).default("none"),
        audience: z.string().optional(),
        issuer: z.string().optional(),
    }).default({ enabled: false, type: "none" })
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
                        Configure the name, description, instructions, and authentication for your Kubernetes assistant.
                    </CardDescription>
                </CardHeader>
                <CardContent className="px-8 pt-6 pb-6 space-y-4">
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
                    <FormField
                        control={form.control}
                        name="a2aAuth.enabled"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Enable A2A Auth</FormLabel>
                                <FormControl>
                                    <Switch checked={field.value} onCheckedChange={field.onChange} />
                                </FormControl>
                                <FormDescription>Enable authentication for this agent&apos;s A2A server.</FormDescription>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                    <FormField
                        control={form.control}
                        name="a2aAuth.type"
                        render={({ field }) => (
                            <FormItem>
                                <FormLabel>Auth Type</FormLabel>
                                <Select value={field.value} onValueChange={field.onChange}>
                                    <FormControl>
                                        <SelectTrigger>
                                            <SelectValue placeholder="Select auth type" />
                                        </SelectTrigger>
                                    </FormControl>
                                    <SelectContent>
                                        <SelectItem value="jwt">JWT</SelectItem>
                                        <SelectItem value="apiKey">API Key</SelectItem>
                                        <SelectItem value="none">None</SelectItem>
                                    </SelectContent>
                                </Select>
                                <FormDescription>Select the authentication type for this agent.</FormDescription>
                                <FormMessage />
                            </FormItem>
                        )}
                    />
                    {form.watch('a2aAuth.type') === 'jwt' && (
                        <>
                            <FormField
                                control={form.control}
                                name="a2aAuth.audience"
                                render={({ field }) => (
                                    <FormItem>
                                        <FormLabel>JWT Audience</FormLabel>
                                        <FormControl>
                                            <Input {...field} />
                                        </FormControl>
                                        <FormDescription>Expected audience for JWT tokens.</FormDescription>
                                        <FormMessage />
                                    </FormItem>
                                )}
                            />
                            <FormField
                                control={form.control}
                                name="a2aAuth.issuer"
                                render={({ field }) => (
                                    <FormItem>
                                        <FormLabel>JWT Issuer</FormLabel>
                                        <FormControl>
                                            <Input {...field} />
                                        </FormControl>
                                        <FormDescription>Expected issuer for JWT tokens.</FormDescription>
                                        <FormMessage />
                                    </FormItem>
                                )}
                            />
                        </>
                    )}
                </CardContent>
                <CardFooter className="flex justify-between items-center pb-8 pt-2">
                    <Button variant="outline" type="button" onClick={onBack}>Back</Button>
                    <Button type="submit">Next: Select Tools</Button>
                </CardFooter>
            </form>
        </Form>
    );
} 