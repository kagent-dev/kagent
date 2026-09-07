"use client";

import { useState, useEffect, useCallback } from "react";
import Link from "next/link";
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
import { Clock, Plus, Pencil, Trash2, Play, Loader2 } from "lucide-react";
import { ScheduledRun } from "@/types";
import { getScheduledRuns, deleteScheduledRun, triggerScheduledRun } from "@/app/actions/scheduledRuns";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { formatDateTime } from "@/lib/formatDateTime";
import {
  formatScheduledRunTargetRef,
  getScheduledRunDisplayStatus,
  isFailedExecutionStatus,
  scheduledRunDetailPath,
  scheduledRunEditPath,
} from "@/lib/scheduledRuns";
import { toast } from "sonner";

export function ScheduledRunList() {
  const router = useRouter();
  const [scheduledRuns, setScheduledRuns] = useState<ScheduledRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [itemToDelete, setItemToDelete] = useState<ScheduledRun | null>(null);
  const [dispatchingScheduledRuns, setDispatchingScheduledRuns] = useState<Set<string>>(new Set());

  const fetchScheduledRuns = useCallback(async () => {
    try {
      setLoading(true);
      const response = await getScheduledRuns();
      if (response.error) {
        throw new Error(response.error);
      }
      setScheduledRuns(response.data ?? []);
      setError(null);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to fetch Scheduled Runs";
      setError(errorMessage);
      toast.error(errorMessage);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchScheduledRuns();
  }, [fetchScheduledRuns]);

  const handleEdit = (sr: ScheduledRun) => {
    const ns = sr.metadata.namespace || "";
    const name = sr.metadata.name;
    router.push(scheduledRunEditPath(ns, name));
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
        throw new Error(response.error);
      }
      toast.success(`Scheduled Run "${name}" deleted successfully`);
      setItemToDelete(null);
      await fetchScheduledRuns();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to delete Scheduled Run";
      toast.error(errorMessage);
      setItemToDelete(null);
    }
  };

  const handleTrigger = async (sr: ScheduledRun) => {
    const ns = sr.metadata.namespace || "";
    const name = sr.metadata.name;
    const key = `${ns}/${name}`;

    setDispatchingScheduledRuns((previous) => new Set(previous).add(key));

    try {
      const response = await triggerScheduledRun(name, ns);
      if (response.error) {
        throw new Error(response.error);
      }
      if (isFailedExecutionStatus(response.data?.status)) {
        toast.error(response.data?.statusMessage || `Execution for "${name}" failed`);
      } else if (response.data?.status === "Succeeded") {
        toast.success(`Execution for "${name}" succeeded`);
      } else {
        toast.success(`Execution for "${name}" dispatched`);
      }
      await fetchScheduledRuns();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to trigger execution";
      toast.error(errorMessage);
    } finally {
      setDispatchingScheduledRuns((previous) => {
        const next = new Set(previous);
        next.delete(key);
        return next;
      });
    }
  };

  const handleRowClick = (sr: ScheduledRun) => {
    const ns = sr.metadata.namespace || "";
    const name = sr.metadata.name;
    router.push(scheduledRunDetailPath(ns, name));
  };

  if (error) {
    return <ErrorState message={error} />;
  }

  return (
    <div className="min-h-screen p-4 md:p-8">
      <div className="max-w-6xl mx-auto">
        <div className="flex flex-col gap-4 sm:flex-row sm:justify-between sm:items-center mb-8">
          <h1 className="text-2xl font-bold">Scheduled Runs</h1>
          <Button variant="default" asChild className="w-full sm:w-auto">
            <Link href="/schedules/new">
              <Plus className="h-4 w-4 mr-2" />
              New Scheduled Run
            </Link>
          </Button>
        </div>

        {loading ? (
          <LoadingState />
        ) : scheduledRuns.length === 0 ? (
          <div className="flex min-h-72 flex-col items-center justify-center text-center">
            <Clock className="mb-4 h-12 w-12 text-muted-foreground/40" aria-hidden />
            <h2 className="text-lg font-medium">No Scheduled Runs yet</h2>
            <p className="mt-1 max-w-sm text-sm text-muted-foreground">
              Create a Scheduled Run to dispatch executions automatically.
            </p>
            <Button className="mt-5" asChild>
              <Link href="/schedules/new">
                <Plus className="mr-2 h-4 w-4" aria-hidden />
                New Scheduled Run
              </Link>
            </Button>
          </div>
        ) : (
          <div className="border rounded-lg overflow-x-auto">
            <Table className="min-w-[900px]">
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Namespace</TableHead>
                  <TableHead>Schedule</TableHead>
                  <TableHead>Target</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Last Execution</TableHead>
                  <TableHead>Next Scheduled Execution</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {scheduledRuns.map((sr) => {
                  const ns = sr.metadata.namespace || "";
                  const name = sr.metadata.name;
                  const key = `${ns}/${name}`;
                  const targetDisplay = formatScheduledRunTargetRef(sr);
                  const status = getScheduledRunDisplayStatus(sr);
                  const isDispatching = dispatchingScheduledRuns.has(key);

                  return (
                    <TableRow
                      key={key}
                      className="cursor-pointer"
                      onClick={() => handleRowClick(sr)}
                    >
                      <TableCell className="font-medium">{name}</TableCell>
                      <TableCell>{ns}</TableCell>
                      <TableCell className="font-mono text-xs">{sr.spec.schedule}</TableCell>
                      <TableCell>{targetDisplay}</TableCell>
                      <TableCell>
                        <Badge variant={status.variant} className={status.className} title={status.title}>
                          {status.label}
                        </Badge>
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-xs">
                        {formatDateTime(sr.status?.lastExecutionTime)}
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-xs">
                        {formatDateTime(sr.status?.nextExecutionTime)}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end space-x-1" onClick={(e) => e.stopPropagation()}>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleTrigger(sr)}
                            disabled={isDispatching}
                            aria-label={`Trigger Scheduled Run ${ns}/${name}`}
                            title={
                              isDispatching
                                ? "Dispatching..."
                                : "Trigger now"
                            }
                          >
                            {isDispatching ? (
                              <Loader2 className="h-4 w-4 animate-spin" />
                            ) : (
                              <Play className="h-4 w-4" />
                            )}
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleEdit(sr)}
                            aria-label={`Edit Scheduled Run ${ns}/${name}`}
                            title="Edit"
                          >
                            <Pencil className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="destructive"
                            size="sm"
                            onClick={() => handleDelete(sr)}
                            aria-label={`Delete Scheduled Run ${ns}/${name}`}
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
                Are you sure you want to delete the Scheduled Run &apos;{itemToDelete?.metadata.name}&apos;?
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
