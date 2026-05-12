"use client";

import React, { useState, useEffect, useCallback } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Plus, Pencil, Trash2, Play, Loader2 } from "lucide-react";
import { ScheduledRun } from "@/types";
import { getScheduledRuns, deleteScheduledRun, triggerScheduledRun } from "@/app/actions/scheduledRuns";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { toast } from "sonner";

function formatDateTime(dateStr?: string): string {
  if (!dateStr) return "-";
  try {
    return new Date(dateStr).toLocaleString();
  } catch {
    return dateStr;
  }
}

export function ScheduledRunList() {
  const router = useRouter();
  const [scheduledRuns, setScheduledRuns] = useState<ScheduledRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [itemToDelete, setItemToDelete] = useState<ScheduledRun | null>(null);
  const [triggeringItems, setTriggeringItems] = useState<Set<string>>(new Set());

  const fetchScheduledRuns = useCallback(async () => {
    try {
      setLoading(true);
      const response = await getScheduledRuns();
      if (response.error || !response.data) {
        throw new Error(response.error || "Failed to fetch scheduled runs");
      }
      setScheduledRuns(response.data);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to fetch scheduled runs";
      setError(errorMessage);
      toast.error(errorMessage);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- legitimate data fetch on mount
    fetchScheduledRuns();
  }, [fetchScheduledRuns]);

  const handleEdit = (sr: ScheduledRun) => {
    const ns = sr.metadata.namespace || "";
    const name = sr.metadata.name;
    router.push(`/schedules/new?edit=true&name=${name}&namespace=${ns}`);
  };

  const handleDelete = (sr: ScheduledRun) => {
    setItemToDelete(sr);
  };

  const confirmDelete = async () => {
    if (!itemToDelete) return;

    const ns = itemToDelete.metadata.namespace || "";
    const name = itemToDelete.metadata.name;

    try {
      const response = await deleteScheduledRun(name, ns);
      if (response.error) {
        throw new Error(response.error || "Failed to delete scheduled run");
      }
      toast.success(`Scheduled run "${name}" deleted successfully`);
      setItemToDelete(null);
      await fetchScheduledRuns();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to delete scheduled run";
      toast.error(errorMessage);
      setItemToDelete(null);
    }
  };

  const handleTrigger = async (sr: ScheduledRun) => {
    const ns = sr.metadata.namespace || "";
    const name = sr.metadata.name;
    const key = `${ns}/${name}`;

    setTriggeringItems((prev) => new Set(prev).add(key));

    try {
      const response = await triggerScheduledRun(name, ns);
      if (response.error) {
        throw new Error(response.error || "Failed to trigger scheduled run");
      }
      toast.success(`Triggered run for "${name}"`);
      await fetchScheduledRuns();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to trigger scheduled run";
      toast.error(errorMessage);
    } finally {
      setTriggeringItems((prev) => {
        const next = new Set(prev);
        next.delete(key);
        return next;
      });
    }
  };

  const handleRowClick = (sr: ScheduledRun) => {
    const ns = sr.metadata.namespace || "";
    const name = sr.metadata.name;
    router.push(`/schedules/${ns}/${name}`);
  };

  if (error) {
    return <ErrorState message={error} />;
  }

  return (
    <div className="min-h-screen p-8">
      <div className="max-w-6xl mx-auto">
        <div className="flex justify-between items-center mb-8">
          <h1 className="text-2xl font-bold">Scheduled Runs</h1>
          <Button variant="default" onClick={() => router.push("/schedules/new")}>
            <Plus className="h-4 w-4 mr-2" />
            New Schedule
          </Button>
        </div>

        {loading ? (
          <LoadingState />
        ) : scheduledRuns.length === 0 ? (
          <div className="text-center py-12 text-muted-foreground">
            No scheduled runs found. Create one to get started.
          </div>
        ) : (
          <div className="border rounded-lg overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Namespace</TableHead>
                  <TableHead>Schedule</TableHead>
                  <TableHead>Agent</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Active</TableHead>
                  <TableHead>Last Run</TableHead>
                  <TableHead>Next Run</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {scheduledRuns.map((sr) => {
                  const ns = sr.metadata.namespace || "";
                  const name = sr.metadata.name;
                  const key = `${ns}/${name}`;
                  const agentRef = sr.spec.agentRef;
                  const agentDisplay = agentRef.namespace
                    ? `${agentRef.namespace}/${agentRef.name}`
                    : agentRef.name;

                  return (
                    <TableRow
                      key={key}
                      className="cursor-pointer"
                      onClick={() => handleRowClick(sr)}
                    >
                      <TableCell className="font-medium">{name}</TableCell>
                      <TableCell>{ns}</TableCell>
                      <TableCell className="font-mono text-xs">{sr.spec.schedule}</TableCell>
                      <TableCell>{agentDisplay}</TableCell>
                      <TableCell>
                        {sr.spec.suspend ? (
                          <Badge variant="secondary">Suspended</Badge>
                        ) : (
                          <Badge variant="outline" className="text-green-600 border-green-600">Active</Badge>
                        )}
                      </TableCell>
                      <TableCell>{sr.status?.active ?? 0}</TableCell>
                      <TableCell className="whitespace-nowrap text-xs">
                        {formatDateTime(sr.status?.lastRunTime)}
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-xs">
                        {formatDateTime(sr.status?.nextRunTime)}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end space-x-1" onClick={(e) => e.stopPropagation()}>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleTrigger(sr)}
                            disabled={triggeringItems.has(key)}
                            title="Trigger now"
                          >
                            {triggeringItems.has(key) ? (
                              <Loader2 className="h-4 w-4 animate-spin" />
                            ) : (
                              <Play className="h-4 w-4" />
                            )}
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleEdit(sr)}
                            title="Edit"
                          >
                            <Pencil className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="destructive"
                            size="sm"
                            onClick={() => handleDelete(sr)}
                            title="Delete"
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}

        <Dialog open={itemToDelete !== null} onOpenChange={(open) => !open && setItemToDelete(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Delete Scheduled Run</DialogTitle>
              <DialogDescription>
                Are you sure you want to delete the scheduled run &apos;{itemToDelete?.metadata.name}&apos;?
                This action cannot be undone.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter className="flex space-x-2 justify-end">
              <Button variant="outline" onClick={() => setItemToDelete(null)}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={confirmDelete}>
                Delete
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </div>
  );
}
