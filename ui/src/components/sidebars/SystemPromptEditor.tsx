"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";

interface SystemPromptEditorProps {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  systemPrompt: string;
  onSave: (prompt: string) => Promise<void>;
  title?: string;
  description?: string;
}

export function SystemPromptEditor({
  isOpen,
  onOpenChange,
  systemPrompt,
  onSave,
  title = "Agent Instructions",
  description = "You can update the instructions for the agent to follow while interacting with the user"
}: SystemPromptEditorProps) {
  const [currentSystemPrompt, setCurrentSystemPrompt] = useState(systemPrompt);
  const [editedSystemPrompt, setEditedSystemPrompt] = useState(systemPrompt);
  const [isPromptEdited, setIsPromptEdited] = useState(false);
  const [isConfirmationDialogOpen, setIsConfirmationDialogOpen] = useState(false);
  const [isUpdating, setIsUpdating] = useState(false);

  // Reset state when the component receives new props
  if (currentSystemPrompt !== systemPrompt) {
    setCurrentSystemPrompt(systemPrompt);
    setEditedSystemPrompt(systemPrompt);
    setIsPromptEdited(false);
  }

  const handleSystemPromptChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setEditedSystemPrompt(e.target.value);
    setIsPromptEdited(e.target.value !== currentSystemPrompt);
  };

  const handleCloseSystemPrompt = () => {
    if (isPromptEdited) {
      setIsConfirmationDialogOpen(true);
    } else {
      onOpenChange(false);
    }
  };

  const handleDiscardChanges = () => {
    setIsConfirmationDialogOpen(false);
    setEditedSystemPrompt(currentSystemPrompt);
    setIsPromptEdited(false);
    onOpenChange(false);
  };

  const handleContinueEditing = () => {
    setIsConfirmationDialogOpen(false);
  };

  const handleUpdateSystemPrompt = async () => {
    setIsUpdating(true);
    try {
      await onSave(editedSystemPrompt);
      setCurrentSystemPrompt(editedSystemPrompt);
      setIsPromptEdited(false);
      onOpenChange(false);
    } catch (error) {
      console.error("Failed to update instructions:", error);
    } finally {
      setIsUpdating(false);
    }
  };

  return (
    <>
      <Dialog open={isOpen} onOpenChange={handleCloseSystemPrompt}>
        <DialogContent className="max-w-4xl max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="text-secondary-foreground">{title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>

          <Textarea 
            value={editedSystemPrompt} 
            onChange={handleSystemPromptChange} 
            className="min-h-[60vh] font-mono text-sm text-secondary-foreground" 
            placeholder="Enter agent instructions..." 
          />

          <DialogFooter>
            <Button 
              variant="default" 
              onClick={handleUpdateSystemPrompt} 
              disabled={!isPromptEdited || isUpdating}
            >
              {isUpdating ? "Updating..." : "Update"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Confirmation Dialog for Unsaved Changes */}
      <AlertDialog open={isConfirmationDialogOpen} onOpenChange={setIsConfirmationDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle className="text-secondary-foreground">Unsaved Changes</AlertDialogTitle>
            <AlertDialogDescription>
              You have unsaved changes to the instructions. Do you want to discard these changes?
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={handleContinueEditing} className="text-secondary-foreground">
              Continue Editing
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDiscardChanges} className="bg-red-600 hover:bg-red-700">
              Discard Changes
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}