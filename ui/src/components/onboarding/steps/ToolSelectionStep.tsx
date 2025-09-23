import React, { useState, useEffect, useMemo } from 'react';
import { Button } from '@/components/ui/button';
import { CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
import { ScrollArea } from "@/components/ui/scroll-area";
import { Checkbox } from "@/components/ui/checkbox";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Info, ChevronDown, ChevronRight, FunctionSquare, Search } from 'lucide-react';
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { getToolResponseDisplayName, getToolResponseDescription, getToolResponseIdentifier, getToolResponseCategory, toolResponseToAgentTool } from "@/lib/toolUtils";
import type { Tool, ToolsResponse } from "@/types";
import { Input } from "@/components/ui/input";

// Grouping category logic should mirror SelectToolsDialog: use getToolResponseCategory

interface ToolSelectionStepProps {
    availableTools: ToolsResponse[] | null;
    loadingTools: boolean;
    errorTools: string | null;
    initialSelectedTools: Tool[];
    onNext: (selectedTools: Tool[]) => void;
    onBack: () => void;
}

export function ToolSelectionStep({
    availableTools,
    loadingTools,
    errorTools,
    initialSelectedTools,
    onNext,
    onBack
}: ToolSelectionStepProps) {
    const [selectedTools, setSelectedTools] = useState<Tool[]>(initialSelectedTools);
    const [expandedCategories, setExpandedCategories] = useState<{ [key: string]: boolean }>({});
    
    const [searchQuery, setSearchQuery] = useState<string>("");

    const toolResponseMatchesTool = (toolResponse: ToolsResponse, tool: Tool): boolean => {
        if (tool.type === "Agent" && tool.agent) {
            return false; // Agents don't match ToolResponse objects
        } else if (tool.type === "McpServer" && tool.mcpServer) {
            return tool.mcpServer.name === toolResponse.server_name && 
                   tool.mcpServer.toolNames.includes(toolResponse.id);
        }
        return false;
    };

    const toolsByCategory = useMemo(() => {
        if (!availableTools) return {} as Record<string, ToolsResponse[]>;

        // Only include Kubernetes tools in onboarding step
        const k8sTools = availableTools.filter((tool) => tool.server_name?.includes("kagent-tool-server"));

        const groups: Record<string, ToolsResponse[]> = {};
        const sortedTools = [...k8sTools].sort((a, b) =>
            getToolResponseDisplayName(a).localeCompare(getToolResponseDisplayName(b))
        );
        sortedTools.forEach((tool) => {
            const category = getToolResponseCategory(tool);
            if (!groups[category]) groups[category] = [];
            groups[category].push(tool);
        });
        return Object.entries(groups)
            .sort(([catA], [catB]) => catA.localeCompare(catB))
            .reduce((acc, [key, value]) => {
                acc[key] = value;
                return acc;
            }, {} as typeof groups);
    }, [availableTools]);

    const filteredToolsByCategory = useMemo(() => {
        // Already filtered to K8s tools above; keep identity here for clarity
        return toolsByCategory;
    }, [toolsByCategory]);

    const searchedToolsByCategory = useMemo(() => {
        const query = searchQuery.trim().toLowerCase();
        if (!query) return filteredToolsByCategory;

        const matchesQuery = (tool: ToolsResponse): boolean => {
            const name = getToolResponseDisplayName(tool).toLowerCase();
            const description = getToolResponseDescription(tool).toLowerCase();
            const server = tool.server_name?.toLowerCase() ?? "";
            const id = tool.id?.toLowerCase() ?? "";
            return (
                name.includes(query) ||
                description.includes(query) ||
                server.includes(query) ||
                id.includes(query)
            );
        };

        return Object.entries(filteredToolsByCategory).reduce((acc, [category, tools]) => {
            const matched = tools.filter(matchesQuery);
            if (matched.length > 0) acc[category] = matched;
            return acc;
        }, {} as Record<string, ToolsResponse[]>);
    }, [filteredToolsByCategory, searchQuery]);

    useEffect(() => {
        if (availableTools && Object.keys(expandedCategories).length === 0) {
            const initialExpandedState: { [key: string]: boolean } = {};
            Object.keys(toolsByCategory).forEach(category => {
                initialExpandedState[category] = true;
            });
            setExpandedCategories(initialExpandedState);
        }
    }, [availableTools, toolsByCategory, expandedCategories]);

    // Pre-select specific K8s tools if none are initially selected
    useEffect(() => {
        // Only run when tools are loaded and no tools are initially selected from props
        if (availableTools && initialSelectedTools.length === 0 && selectedTools.length === 0) {
            const desiredIds: string[] = [
                "k8s_get_available_api_resources",
                "k8s_get_resources",
            ];

            const initialSelection: Tool[] = [];
            availableTools.forEach((tool) => {
                const toolId = getToolResponseDisplayName(tool);
                if (desiredIds.includes(toolId)) {
                    initialSelection.push(toolResponseToAgentTool(tool, tool.server_name));
                }
            });

            if (initialSelection.length > 0) {
                setSelectedTools(initialSelection);
            }
        }
    }, [availableTools, initialSelectedTools, selectedTools.length]);

    const handleToolToggle = (toolResponse: ToolsResponse) => {
        const agentTool = toolResponseToAgentTool(toolResponse, toolResponse.server_name);
        setSelectedTools(prev => {
            const isSelected = prev.some(t => toolResponseMatchesTool(toolResponse, t));
            return isSelected ? prev.filter(t => !toolResponseMatchesTool(toolResponse, t)) : [...prev, agentTool];
        });
    };

    const toggleCategory = (category: string) => {
        setExpandedCategories((prev) => ({ ...prev, [category]: !prev[category] }));
    };

    const isToolSelected = (toolResponse: ToolsResponse): boolean => {
        return selectedTools.some(t => toolResponseMatchesTool(toolResponse, t));
    };

    const handleSubmit = () => {
        onNext(selectedTools);
    };

    // Tooltips removed; no description lookups needed

    if (loadingTools) return <LoadingState />;
    if (errorTools) return <ErrorState message={`Failed to load tools: ${errorTools}`} />;

    const hasAnyTools = availableTools && availableTools.length > 0;
    const hasCategoryToolsBeforeSearch = Object.keys(filteredToolsByCategory).length > 0;
    const hasSearchedTools = Object.keys(searchedToolsByCategory).length > 0;

    return (
        <>
            <CardHeader className="pt-8 pb-4 border-b">
                <CardTitle className="text-2xl">Step 3: Select Tools</CardTitle>
                <CardDescription className="text-md">
                    Tools give your agent actions. We&apos;ve selected two for you (
                    <span className="italic">k8s_get_available_api_resources</span>{' '}
                    <span className="italic">k8s_get_resources</span>
                    ), but you can add more later.
                </CardDescription>
            </CardHeader>
            <CardContent className="px-8 pt-6 pb-6 space-y-4">
                <div className="mb-3 w-full">
                    <div className="relative w-full">
                        <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                        <Input
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                            placeholder="Search tools by name or description..."
                            aria-label="Search tools"
                            className="pl-8"
                        />
                    </div>
                </div>

                {!hasAnyTools ? (
                    <Alert variant="default"><Info className="h-4 w-4" /><AlertTitle>No Tools Available</AlertTitle><AlertDescription>No tools found. Connect MCP Servers or define tools later.</AlertDescription></Alert>
                ) : !hasCategoryToolsBeforeSearch ? (
                    <Alert variant="default"><Info className="h-4 w-4" /><AlertTitle>No Kubernetes Tools Found</AlertTitle><AlertDescription>Couldn&apos;t find specific K8s tools. Connect MCP Servers or define tools later.</AlertDescription></Alert>
                ) : !hasSearchedTools ? (
                    <Alert variant="default"><Info className="h-4 w-4" /><AlertTitle>No Matching Tools</AlertTitle><AlertDescription>No tools match your search. Try a different query or clear the search.</AlertDescription></Alert>
                ) : (
                    <ScrollArea className="h-[300px] border rounded-md p-2">
                        <div className="space-y-3 pr-2">
                            {Object.entries(searchedToolsByCategory).map(([category, categoryTools]) => (
                                <div key={category}>
                                    <div
                                        className="flex items-center justify-between cursor-pointer py-1 px-1 rounded hover:bg-muted/50"
                                        onClick={() => toggleCategory(category)}
                                    >
                                        <div className="flex items-center gap-2">
                                            {expandedCategories[category] ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
                                            <h4 className="font-semibold capitalize text-sm">{category}</h4>
                                        </div>
                                        <span className="text-xs text-muted-foreground">{categoryTools.length} tool(s)</span>
                                    </div>
                                    {expandedCategories[category] && (
                                        <div className="pl-6 pt-2 space-y-2">
                                            {categoryTools.map((tool: ToolsResponse) => (
                                                <div key={getToolResponseIdentifier(tool)} className="flex items-start space-x-3">
                                                    <Checkbox
                                                        id={getToolResponseIdentifier(tool)}
                                                        checked={isToolSelected(tool)}
                                                        onCheckedChange={() => handleToolToggle(tool)}
                                                        className="mt-1"
                                                    />
                                                    <div className="grid gap-1.5 leading-none">
                                                        <label htmlFor={getToolResponseIdentifier(tool)} className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 flex items-center gap-2">
                                                            <FunctionSquare className="h-4 w-4 text-blue-500 flex-shrink-0" />
                                                            {getToolResponseDisplayName(tool)}
                                                        </label>
                                                        <p className="text-xs text-muted-foreground">{getToolResponseDescription(tool)}</p>
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </div>
                            ))}
                        </div>
                    </ScrollArea>
                )}
            </CardContent>
            <CardFooter className="flex justify-between items-center pb-8 pt-2">
                <Button variant="outline" type="button" onClick={onBack}>Back</Button>
                <Button onClick={handleSubmit} disabled={!hasAnyTools && !loadingTools}>Next: Review</Button>
            </CardFooter>
        </>
    );
} 