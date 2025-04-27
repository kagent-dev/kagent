import React, { useState, useEffect, useMemo } from 'react';
import { Button } from '@/components/ui/button';
import { CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
import { ScrollArea } from "@/components/ui/scroll-area";
import { Checkbox } from "@/components/ui/checkbox";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Info, ChevronDown, ChevronRight, FunctionSquare, Filter, FilterX, HelpCircle } from 'lucide-react';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { getToolDisplayName, getToolDescription, getToolIdentifier, componentToAgentTool } from "@/lib/toolUtils";
import { AgentTool, Component, ToolConfig } from "@/types/datamodel";

const getToolCategory = (tool: Component<ToolConfig>): string => {
    if (tool.provider === "autogen_ext.tools.mcp.SseMcpToolAdapter") {
      return tool.label || "MCP Server";
    }
    const toolId = getToolIdentifier(tool);
    const parts = toolId.split("-");
    if (parts.length >= 2 && parts[0] === 'component') {
        const providerParts = parts[1].split(".");
        if (providerParts.length >= 3 && providerParts[1] === "tools") return providerParts[2];
        if (providerParts.length >= 2) return providerParts[1];
    }
    return "other";
};

interface ToolSelectionStepProps {
    availableTools: Component<ToolConfig>[] | null;
    loadingTools: boolean;
    errorTools: string | null;
    initialSelectedTools: AgentTool[];
    onNext: (selectedTools: AgentTool[]) => void;
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
    const [selectedTools, setSelectedTools] = useState<AgentTool[]>(initialSelectedTools);
    const [expandedCategories, setExpandedCategories] = useState<{ [key: string]: boolean }>({});
    const [showAllTools, setShowAllTools] = useState(false);

    const toolsByCategory = useMemo(() => {
        if (!availableTools) return {};
        const groups: Record<string, Component<ToolConfig>[]> = {};
        const sortedTools = [...availableTools].sort((a, b) => (getToolDisplayName(a) || "").localeCompare(getToolDisplayName(b) || ""));
        sortedTools.forEach(tool => {
            const category = getToolCategory(tool);
            if (!groups[category]) groups[category] = [];
            groups[category].push(tool);
        });
        return Object.entries(groups).sort(([catA], [catB]) => catA.localeCompare(catB))
               .reduce((acc, [key, value]) => { acc[key] = value; return acc; }, {} as typeof groups);
    }, [availableTools]);

    const filteredToolsByCategory = useMemo(() => {
        if (showAllTools) return toolsByCategory;
        const k8sCategory = toolsByCategory['k8s'];
        return k8sCategory ? { k8s: k8sCategory } : {};
    }, [toolsByCategory, showAllTools]);

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
            const toolsToSelect: string[] = ["GetResources", "GetAvailableAPIResources"];
            const k8sCategoryKey = Object.keys(toolsByCategory).find(key => key.toLowerCase() === 'k8s');

            if (k8sCategoryKey) {
                const k8sToolsComponents = toolsByCategory[k8sCategoryKey];
                const initialSelection: AgentTool[] = [];
                k8sToolsComponents.forEach(component => {
                    const toolName = getToolDisplayName(component);
                    if (toolsToSelect.includes(toolName)) {
                        initialSelection.push(componentToAgentTool(component));
                    }
                });
                if (initialSelection.length > 0) {
                    setSelectedTools(initialSelection);
                }
            }
        }
    }, [availableTools, toolsByCategory, initialSelectedTools, selectedTools.length]);

    const handleToolToggle = (component: Component<ToolConfig>) => {
        const agentTool = componentToAgentTool(component);
        const toolId = getToolIdentifier(agentTool);
        setSelectedTools(prev => {
            const isSelected = prev.some(t => getToolIdentifier(t) === toolId);
            return isSelected ? prev.filter(t => getToolIdentifier(t) !== toolId) : [...prev, agentTool];
        });
    };

    const toggleCategory = (category: string) => {
        setExpandedCategories((prev) => ({ ...prev, [category]: !prev[category] }));
    };

    const isToolSelected = (component: Component<ToolConfig>): boolean => {
        const toolId = getToolIdentifier(componentToAgentTool(component));
        return selectedTools.some(t => getToolIdentifier(t) === toolId);
    };

    const handleSubmit = () => {
        onNext(selectedTools);
    };

    const getResourcesTool = availableTools?.find(t => getToolDisplayName(t) === 'GetResources');
    const getApiResourcesTool = availableTools?.find(t => getToolDisplayName(t) === 'GetAvailableAPIResources');
    const getResourcesDesc = getResourcesTool ? getToolDescription(getResourcesTool) : "No description available.";
    const getApiResourcesDesc = getApiResourcesTool ? getToolDescription(getApiResourcesTool) : "No description available.";

    if (loadingTools) return <LoadingState />;
    if (errorTools) return <ErrorState message={`Failed to load tools: ${errorTools}`} />;

    const hasAnyTools = availableTools && availableTools.length > 0;
    const hasFilteredTools = Object.keys(filteredToolsByCategory).length > 0;

    return (
        <>
            <CardHeader className="pt-8 pb-4 border-b">
                <CardTitle className="text-2xl">Step 3: Select Tools</CardTitle>
                <CardDescription className="text-md">
                    <TooltipProvider delayDuration={100}>
                        Tools give your agent actions. We&apos;ve selected two for you (
                        <Tooltip><TooltipTrigger asChild><span className="italic inline-flex items-center cursor-help">GetAvailableAPIResources<HelpCircle className="h-3 w-3 ml-0.5 text-primary" /></span></TooltipTrigger><TooltipContent side="top" className="max-w-xs p-2"><p className="text-xs">{getApiResourcesDesc}</p></TooltipContent></Tooltip>
                        {' '}
                        <Tooltip><TooltipTrigger asChild><span className="italic inline-flex items-center cursor-help">GetResources<HelpCircle className="h-3 w-3 ml-0.5 text-primary" /></span></TooltipTrigger><TooltipContent side="top" className="max-w-xs p-2"><p className="text-xs">{getResourcesDesc}</p></TooltipContent></Tooltip>
                        ), but you can add more later.
                    </TooltipProvider>
                </CardDescription>
            </CardHeader>
            <CardContent className="px-8 pt-6 pb-6 space-y-4">
                <div className="flex justify-end mb-3">
                    <Button
                        variant="outline" size="sm"
                        onClick={() => setShowAllTools(!showAllTools)}
                        disabled={!hasAnyTools}
                    >
                        {showAllTools ? <FilterX className="mr-2 h-4 w-4" /> : <Filter className="mr-2 h-4 w-4" />}
                        {showAllTools ? "Show K8s Tools Only" : "Show All Available Tools"}
                    </Button>
                </div>

                {!hasAnyTools ? (
                    <Alert variant="default"><Info className="h-4 w-4" /><AlertTitle>No Tools Available</AlertTitle><AlertDescription>No tools found. Connect Tool Servers or define tools later.</AlertDescription></Alert>
                ) : !hasFilteredTools && !showAllTools ? (
                    <Alert variant="default"><Info className="h-4 w-4" /><AlertTitle>No Kubernetes Tools Found</AlertTitle><AlertDescription>Couldn&apos;t find specific K8s tools. Try showing all available tools.</AlertDescription></Alert>
                ) : (
                    <ScrollArea className="h-[300px] border rounded-md p-2">
                        <div className="space-y-3 pr-2">
                            {Object.entries(filteredToolsByCategory).map(([category, categoryTools]) => (
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
                                            {categoryTools.map((tool: Component<ToolConfig>) => (
                                                <div key={getToolIdentifier(tool)} className="flex items-start space-x-3">
                                                    <Checkbox
                                                        id={getToolIdentifier(tool)}
                                                        checked={isToolSelected(tool)}
                                                        onCheckedChange={() => handleToolToggle(tool)}
                                                        className="mt-1"
                                                    />
                                                    <div className="grid gap-1.5 leading-none">
                                                        <label htmlFor={getToolIdentifier(tool)} className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 flex items-center gap-2">
                                                            <FunctionSquare className="h-4 w-4 text-blue-500 flex-shrink-0" />
                                                            {getToolDisplayName(tool)}
                                                        </label>
                                                        <p className="text-xs text-muted-foreground">{getToolDescription(tool)}</p>
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