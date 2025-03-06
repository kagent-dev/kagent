import { ScrollArea } from "@/components/ui/scroll-area";
import { ChevronRight, ChevronLeft, FunctionSquare, Clipboard } from "lucide-react";
import type { Team, AssistantAgentConfig, ToolConfig, Component } from "@/types/datamodel";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useState } from "react";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Textarea } from "@/components/ui/textarea";
import { getToolDescription, getToolDisplayName, getToolIdentifier, isMcpTool } from "@/lib/data";
import { useResponsiveSidebar } from "@/components/sidebars/useResponsiveSidebar";
import { createTeam } from "@/app/actions/teams";
import KagentLogo from "../kagent-logo";
import { findAllAssistantAgents, updateUsersAgent } from "@/lib/agents";

interface AgentDetailsSidebarProps {
  selectedTeam: Team | null;
}

export function AgentDetailsSidebar({ selectedTeam }: AgentDetailsSidebarProps) {
  const [isSystemPromptOpen, setIsSystemPromptOpen] = useState(false);
  const [currentSystemPrompt, setCurrentSystemPrompt] = useState("");
  const [editedSystemPrompt, setEditedSystemPrompt] = useState("");
  const [isPromptEdited, setIsPromptEdited] = useState(false);
  const [isConfirmationDialogOpen, setIsConfirmationDialogOpen] = useState(false);
  const [isUpdating, setIsUpdating] = useState(false);
  const { isOpen, toggle } = useResponsiveSidebar({ breakpoint: 1024, side: "right" });

  // Keep track of the current agent for updating
  const [currentAgentIndex, setCurrentAgentIndex] = useState<number | null>(null);

  const renderAgentTools = (tools: Component<ToolConfig>[] = []) => {
    if (tools.length === 0) {
      return <div className="text-sm text-white/40 italic">No tools available</div>;
    }

    return (
      <ul className="mt-4 flex flex-col gap-2">
        {tools.map((tool) => {
          const toolIdentifier = getToolIdentifier(tool);
          const displayName = getToolDisplayName(tool);
          const displayDescription = getToolDescription(tool);

          return (
            <li key={toolIdentifier}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className="hover:text-white/60 transition-colors">{displayName}</span>
                </TooltipTrigger>
                <TooltipContent>
                  <p>{displayDescription}</p>
                </TooltipContent>
              </Tooltip>
              {isMcpTool(tool) && <span className="ml-2 text-xs bg-blue-400/20 text-blue-400 px-2 py-0.5 rounded">MCP</span>}
            </li>
          );
        })}
      </ul>
    );
  };

  const handleOpenSystemPrompt = (systemMessage: string, agentIndex: number) => {
    setCurrentSystemPrompt(systemMessage);
    setEditedSystemPrompt(systemMessage);
    setIsPromptEdited(false);
    setCurrentAgentIndex(agentIndex);
    setIsSystemPromptOpen(true);
  };

  const handleSystemPromptChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setEditedSystemPrompt(e.target.value);
    setIsPromptEdited(e.target.value !== currentSystemPrompt);
  };

  const handleCloseSystemPrompt = () => {
    if (isPromptEdited) {
      setIsConfirmationDialogOpen(true);
    } else {
      setIsSystemPromptOpen(false);
    }
  };

  const handleDiscardChanges = () => {
    setIsConfirmationDialogOpen(false);
    setIsSystemPromptOpen(false);
    setIsPromptEdited(false);
  };

  const handleContinueEditing = () => {
    setIsConfirmationDialogOpen(false);
  };

  const handleUpdateSystemPrompt = async () => {
    if (!selectedTeam || currentAgentIndex === null) return;

    setIsUpdating(true);
    try {
      const updatedTeam = updateUsersAgent(selectedTeam, (agent) => {
        agent.config.system_message = editedSystemPrompt;
      });

      // Remove the created_at and updated_at variables from the selectedTeam - backend fails as the date/time is not in the correct format (or something)
      // in any case, the backend updates the created_at and updated_at fields automatically anyway
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { created_at, updated_at, ...editedTeam } = updatedTeam;
      await createTeam(editedTeam);

      setCurrentSystemPrompt(editedSystemPrompt);
      setIsPromptEdited(true);
      setIsUpdating(false);
      // Close the dialog
      setIsSystemPromptOpen(false);
    } catch (error) {
      console.error("Failed to update instructions:", error);
    } finally {
      setIsUpdating(false);
    }
  };

  const assistantAgents = findAllAssistantAgents(selectedTeam?.component);

  return (
    <>
      <div
        className={`fixed top-0 right-0 h-screen transition-all duration-300 ease-in-out 
            bg-[#2A2A2A] border-l border-t border-b border-[#3A3A3A] 
            ${isOpen ? "w-96" : "w-12"}`}
      >
        <div className="h-full flex flex-col text-white">
          <div className="p-4 flex items-center gap-2 border-b border-[#3A3A3A] shrink-0">
            <Button variant="ghost" size="icon" onClick={toggle} className="h-8 w-8 hover:bg-[#3A3A3A] text-white hover:text-white transition-colors">
              {isOpen ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
            </Button>
            {isOpen && <h1 className="text-sm font-semibold flex-1">Agent</h1>}
          </div>

          <div className={`h-full ${isOpen ? "opacity-100" : "opacity-0"} transition-opacity duration-200`}>
            <div className="h-full flex flex-col text-white">
              {!selectedTeam ? (
                <div className="flex items-center justify-center flex-1 text-white/50 p-6">No agent selected</div>
              ) : (
                <ScrollArea className="flex-1 px-6 py-6">
                  <div className="space-y-4">
                    {assistantAgents.map((participant, index) => {
                      const assistantAgent = participant.config as AssistantAgentConfig;
                      return (
                        <div key={index} className="text-sm text-white/50">
                          <div className="flex items-start  flex-col space-y-2">
                            <div className="flex items-center justify-between w-full">
                              <div className="inline-flex justify-center items-center gap-2">
                                <KagentLogo className="h-5 w-5" />
                                <h5 className="text-white font-semibold text-base">{participant.label}</h5>
                              </div>
                              <div>
                                <TooltipProvider>
                                  <Tooltip>
                                    <TooltipTrigger asChild>
                                      <Button
                                        variant="link"
                                        size="sm"
                                        onClick={async () => await navigator.clipboard.writeText(JSON.stringify(selectedTeam))}
                                        className="px-0 text-white/80 hover:text-white"
                                      >
                                        <Clipboard className="h-4 w-4 mr-2" />
                                      </Button>
                                    </TooltipTrigger>
                                    <TooltipContent>
                                      <p>Copy JSON representation</p>
                                    </TooltipContent>
                                  </Tooltip>
                                </TooltipProvider>
                              </div>
                            </div>

                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <Badge variant="outline" className="text-xs text-white/50">
                                    {assistantAgent.model_client.config.model}
                                  </Badge>
                                </TooltipTrigger>
                                <TooltipContent>
                                  <p>The model agent is using</p>
                                </TooltipContent>
                              </Tooltip>
                            </TooltipProvider>
                          </div>
                          <p className="mt-4 text-base">{assistantAgent.description}</p>
                          <div className="mt-4 text-base">
                            <Button
                              variant="link"
                              size="sm"
                              onClick={() => handleOpenSystemPrompt(assistantAgent.system_message ?? "No system prompt available", index)}
                              className="px-0 text-white/80 hover:text-white"
                            >
                              View instructions &rarr;
                            </Button>
                          </div>

                          <div className="mt-8">
                            <h6 className="text-base font-medium text-white/70 flex items-center gap-2 mb-4">
                              <FunctionSquare className="h-6 w-6" />
                              Available Tools
                            </h6>
                            {renderAgentTools(assistantAgent.tools)}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </ScrollArea>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Agent Instructions Dialog */}
      <AlertDialog open={isSystemPromptOpen} onOpenChange={handleCloseSystemPrompt}>
        <AlertDialogContent className="bg-[#2A2A2A] border border-[#3A3A3A] text-white max-w-4xl max-h-[80vh] overflow-y-auto">
          <AlertDialogHeader>
            <AlertDialogTitle>Agent Instructions</AlertDialogTitle>
          </AlertDialogHeader>
          <div className="my-4">
            <Textarea
              value={editedSystemPrompt}
              onChange={handleSystemPromptChange}
              className="min-h-[60vh] bg-[#1A1A1A] border-[#3A3A3A] text-white font-mono text-sm"
              placeholder="Enter agent instructions..."
            />
          </div>
          <AlertDialogFooter>
            <div className="flex justify-between w-full">
              <AlertDialogCancel className="bg-transparent text-white hover:text-white/80 hover:bg-transparent">Cancel</AlertDialogCancel>
              <Button variant="default" onClick={handleUpdateSystemPrompt} disabled={!isPromptEdited || isUpdating} className="bg-violet-600 hover:bg-violet-700 text-white">
                {isUpdating ? "Updating..." : "Update"}
              </Button>
            </div>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Confirmation Dialog for Unsaved Changes */}
      <AlertDialog open={isConfirmationDialogOpen} onOpenChange={setIsConfirmationDialogOpen}>
        <AlertDialogContent className="bg-[#2A2A2A] border border-[#3A3A3A] text-white">
          <AlertDialogHeader>
            <AlertDialogTitle>Unsaved Changes</AlertDialogTitle>
            <AlertDialogDescription className="text-white/70">You have unsaved changes to the instructions. Do you want to discard these changes?</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={handleContinueEditing} className="bg-transparent text-white hover:text-white/80 hover:bg-transparent">
              Continue Editing
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDiscardChanges} className="bg-red-600 hover:bg-red-700 text-white">
              Discard Changes
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
