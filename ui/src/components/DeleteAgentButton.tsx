"use client";

import { useState } from "react";
import { Loader2, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { deleteAgent } from "@/app/actions/agents";
import { useAgents } from "./AgentsProvider";

interface DeleteButtonProps {
  agentName: string;
}

export function DeleteButton({ agentName }: DeleteButtonProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const { refreshAgents } = useAgents();

  const handleDelete = async (e: React.MouseEvent) => {
    // Prevent the event from bubbling up to the Card component
    e.stopPropagation();
    e.preventDefault();

    try {
      setIsDeleting(true);
      await deleteAgent(agentName);

      await refreshAgents();
    } catch (error) {
      console.error("Error deleting agent:", error);
    } finally {
      setIsDeleting(false);
      setIsOpen(false);
    }
  };

  const handleOpenChange = (open: boolean) => {
    if (!isDeleting) {
      setIsOpen(open);
    }
  };

  return (
    <>
      <Button
        variant="ghost"
        size="sm"
        className="text-red-400 hover:text-red-300 hover:bg-red-400/10 h-8 w-8 p-0"
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
          setIsOpen(true);
        }}
        disabled={isDeleting}
      >
        {isDeleting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
      </Button>

      <AlertDialog open={isOpen} onOpenChange={handleOpenChange}>
        <AlertDialogContent onClick={(e) => e.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Agent</AlertDialogTitle>
            <AlertDialogDescription>Are you sure you want to delete this agent? This action cannot be undone.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isDeleting} onClick={(e) => e.stopPropagation()}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction className="bg-red-500 hover:bg-red-600" onClick={(e) => handleDelete(e)} disabled={isDeleting}>
              {isDeleting ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Deleting...
                </>
              ) : (
                "Delete"
              )}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
