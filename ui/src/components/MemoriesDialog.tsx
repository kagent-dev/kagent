"use client";

import { useEffect, useState } from "react";
import { Brain, Loader2 } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { listAgentMemories } from "@/app/actions/memories";
import { AgentMemory } from "@/types";

interface MemoriesDialogProps {
  agentName: string;
  namespace: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function MemoriesDialog({ agentName, namespace, open, onOpenChange }: MemoriesDialogProps) {
  const [memories, setMemories] = useState<AgentMemory[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;

    const fetchMemories = async () => {
      setLoading(true);
      setError(null);
      const { data, error: fetchError } = await listAgentMemories(agentName, namespace);
      if (fetchError) {
        setError(fetchError instanceof Error ? fetchError.message : "Failed to load memories");
      } else {
        setMemories(data ?? []);
      }
      setLoading(false);
    };

    fetchMemories();
  }, [open, agentName, namespace]);

  const formatDate = (iso: string) => {
    try {
      return new Date(iso).toLocaleString();
    } catch {
      return iso;
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl max-h-[80vh] flex flex-col" onClick={(e) => e.stopPropagation()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Brain className="h-5 w-5" />
            Memories for {namespace}/{agentName}
          </DialogTitle>
          <DialogDescription>
            All memories associated with this agent, ranked by access frequency.
          </DialogDescription>
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
                  <TableHead className="w-[50%]">Content</TableHead>
                  <TableHead className="text-center">Access Count</TableHead>
                  <TableHead>Created At</TableHead>
                  <TableHead>Expires At</TableHead>
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
                      {formatDate(memory.created_at)}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
                      {memory.expires_at ? formatDate(memory.expires_at) : "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
