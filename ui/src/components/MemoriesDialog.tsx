"use client";

import { useCallback, useEffect, useState } from "react";
import { Brain, Loader2, Trash2 } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { clearAgentMemory, listAgentMemories } from "@/app/actions/memories";
import { AgentMemory } from "@/types";

interface MemoriesDialogProps {
  agentName: string;
  namespace: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

function formatDateOnly(iso: string): string {
  try {
    const d = new Date(iso);
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    return `${y}-${m}-${day}`;
  } catch {
    return iso;
  }
}

export function MemoriesDialog({ agentName, namespace, open, onOpenChange }: MemoriesDialogProps) {
  const [memories, setMemories] = useState<AgentMemory[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const fetchMemories = useCallback(async () => {
    setLoading(true);
    setError(null);
    const { data, error: fetchError } = await listAgentMemories(agentName, namespace);
    if (fetchError) {
      setError(fetchError instanceof Error ? fetchError.message : "Failed to load memories");
    } else {
      setMemories(data ?? []);
    }
    setLoading(false);
  }, [agentName, namespace]);

  useEffect(() => {
    if (!open) return;
    fetchMemories();
  }, [open, fetchMemories]);

  const handleClearMemories = async () => {
    setDeleting(true);
    try {
      const { error: deleteError } = await clearAgentMemory(agentName, namespace);
      if (deleteError) {
        setError(deleteError instanceof Error ? deleteError.message : "Failed to delete memories");
      } else {
        setMemories([]);
        setDeleteDialogOpen(false);
      }
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl max-h-[80vh] flex flex-col" onClick={(e) => e.stopPropagation()}>
        <DialogHeader>
          <div className="flex items-start justify-between gap-2 w-full">
            <div>
              <DialogTitle className="flex items-center gap-2">
                <Brain className="h-5 w-5" />
                Memories for {namespace}/{agentName}
              </DialogTitle>
              <DialogDescription>
                All memories associated with this agent, ranked by access frequency.
              </DialogDescription>
            </div>
            {memories.length > 0 && (
              <Button
                variant="ghost"
                size="sm"
                className="self-start text-red-400 hover:text-red-300 hover:bg-red-400/10 h-8 w-8 p-0 mt-0.5"
                onClick={(e) => {
                  e.stopPropagation();
                  e.preventDefault();
                  setDeleteDialogOpen(true);
                }}
                disabled={deleting}
                aria-label="Delete all memories"
              >
                {deleting ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Trash2 className="h-4 w-4" />
                )}
              </Button>
            )}
          </div>
        </DialogHeader>

        <div className="flex-1 overflow-auto mt-2">
          {loading && (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          )}

          {!loading && error && (
            <div className="text-sm text-destructive text-center py-8">{error}</div>
          )}

          {!loading && !error && memories.length === 0 && (
            <div className="text-sm text-muted-foreground text-center py-8">
              No memories found for this agent.
            </div>
          )}

          {!loading && !error && memories.length > 0 && (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[55%]">Content</TableHead>
                  <TableHead className="text-center">Access Count</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {memories.map((memory) => (
                  <TableRow key={memory.id}>
                    <TableCell className="text-sm whitespace-pre-wrap break-words">{memory.content}</TableCell>
                    <TableCell className="text-center">
                      <Badge variant={memory.access_count >= 10 ? "default" : "secondary"}>
                        {memory.access_count}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
                      {formatDateOnly(memory.created_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </DialogContent>

      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent onClick={(e) => e.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete all memories</AlertDialogTitle>
            <AlertDialogDescription>
              This will remove all memories for this agent. This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleting}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-red-500 hover:bg-red-600"
              onClick={(e) => {
                e.preventDefault();
                handleClearMemories();
              }}
              disabled={deleting}
            >
              {deleting ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Deleting...
                </>
              ) : (
                "Delete all"
              )}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Dialog>
  );
}
