"use client";

import { useState, useEffect, useCallback } from "react";
import { useRouter, useParams } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Pencil, Trash2, Play, Pause, PlayCircle, Loader2, Clock } from "lucide-react";
import { ScheduledRunExecution, ScheduledRun } from "@/types";
import {
  getScheduledRun,
  getScheduledRunExecutions,
  deleteScheduledRun,
  triggerScheduledRun,
  updateScheduledRun,
} from "@/app/actions/scheduledRuns";
import { ExecutionHistoryTable } from "@/components/schedules/ExecutionHistoryTable";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { formatDateTime } from "@/lib/formatDateTime";
import {
  formatScheduledRunTargetRef,
  getScheduledRunDisplayStatus,
  isFailedExecutionStatus,
  mergeScheduledRunExecutions,
  scheduledRunEditPath,
  scheduledRunTargetNamespace,
} from "@/lib/scheduledRuns";
import { toast } from "sonner";

const EXECUTIONS_PAGE_SIZE = 50;

export default function ScheduledRunDetailPage() {
  const router = useRouter();
  const params = useParams();
  const namespace = params.namespace as string;
  const name = params.name as string;

  const [sr, setSr] = useState<ScheduledRun | null>(null);
  const [executions, setExecutions] = useState<ScheduledRunExecution[]>([]);
  const [hasMoreExecutions, setHasMoreExecutions] = useState(false);
  const [isLoadingMoreExecutions, setIsLoadingMoreExecutions] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [isTriggering, setIsTriggering] = useState(false);
  const [isTogglingPause, setIsTogglingPause] = useState(false);

  const fetchData = useCallback(async (showLoading = true) => {
    try {
      if (showLoading) setLoading(true);
      const [response, executionsResponse] = await Promise.all([
        getScheduledRun(name, namespace),
        getScheduledRunExecutions(name, namespace, undefined, undefined, EXECUTIONS_PAGE_SIZE),
      ]);
      if (response.error || !response.data) {
        throw new Error(response.error || "Scheduled Run not found");
      }
      if (executionsResponse.error) {
        throw new Error(executionsResponse.error);
      }
      setSr(response.data);
      const latestExecutions = executionsResponse.data || response.data.status?.recentExecutions || [];
      const newestPageFull = executionsResponse.data?.length === EXECUTIONS_PAGE_SIZE;
      if (showLoading) {
        setHasMoreExecutions(newestPageFull);
      } else if (newestPageFull) {
        // Polling only refetches the newest page; a full page means older
        // executions may exist beyond what's loaded, so keep "load older"
        // reachable. handleLoadMoreExecutions stays authoritative for clearing it.
        setHasMoreExecutions(true);
      }
      setExecutions((existing) => {
        if (showLoading || existing.length === 0) {
          return latestExecutions;
        }
        return mergeScheduledRunExecutions(latestExecutions, existing);
      });
      setError(null);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to fetch Scheduled Run";
      if (showLoading) {
        setError(errorMessage);
      }
    } finally {
      if (showLoading) setLoading(false);
    }
  }, [name, namespace]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  useEffect(() => {
    if (!sr || !executions.some((execution) => execution.status === "InProgress")) return;
    const interval = window.setInterval(() => {
      fetchData(false);
    }, 5000);
    return () => window.clearInterval(interval);
  }, [executions, fetchData, sr]);

  const handleLoadMoreExecutions = async () => {
    const oldest = executions.at(-1);
    const before = oldest?.startTime;
    const beforeID = oldest?.id;
    if (!before || isLoadingMoreExecutions) return;
    setIsLoadingMoreExecutions(true);
    try {
      const response = await getScheduledRunExecutions(name, namespace, before, beforeID, EXECUTIONS_PAGE_SIZE);
      if (response.error) throw new Error(response.error);
      const older = response.data || [];
      setExecutions((existing) => mergeScheduledRunExecutions(older, existing));
      setHasMoreExecutions(older.length === EXECUTIONS_PAGE_SIZE);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load execution history");
    } finally {
      setIsLoadingMoreExecutions(false);
    }
  };

  const handleEdit = () => {
    router.push(scheduledRunEditPath(namespace, name));
  };

  const handleDelete = async () => {
    if (isDeleting) return;
    setIsDeleting(true);
    try {
      const response = await deleteScheduledRun(name, namespace);
      if (response.error) {
        throw new Error(response.error);
      }
      toast.success(`Scheduled Run "${name}" deleted successfully`);
      router.push("/schedules");
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to delete Scheduled Run";
      toast.error(errorMessage);
    } finally {
      setIsDeleting(false);
    }
  };

  const handleTrigger = async () => {
    setIsTriggering(true);
    try {
      const response = await triggerScheduledRun(name, namespace);
      if (response.error) {
        throw new Error(response.error);
      }
      // Immediate Message and terminal Task results are final; an asynchronous
      // Task stays InProgress until the outcome poller resolves it.
      if (isFailedExecutionStatus(response.data?.status)) {
        toast.error(response.data?.statusMessage || `Execution for "${name}" failed`);
      } else if (response.data?.status === "Succeeded") {
        toast.success(`Execution for "${name}" succeeded`);
      } else {
        toast.success(`Execution for "${name}" dispatched`);
      }
      await fetchData(false);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to trigger execution";
      toast.error(errorMessage);
    } finally {
      setIsTriggering(false);
    }
  };

  const handleToggleSuspend = async () => {
    if (!sr) return;
    setIsTogglingPause(true);
    try {
      const updated: ScheduledRun = {
        ...sr,
        spec: {
          ...sr.spec,
          suspended: !sr.spec.suspended,
        },
      };
      const response = await updateScheduledRun(updated);
      if (response.error) {
        throw new Error(response.error);
      }
      toast.success(sr.spec.suspended ? "Scheduled Run resumed" : "Scheduled Run suspended");
      await fetchData(false);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to update Scheduled Run";
      toast.error(errorMessage);
    } finally {
      setIsTogglingPause(false);
    }
  };

  if (loading) return <LoadingState />;
  if (error) return <ErrorState message={error} />;
  if (!sr) return <ErrorState message="Scheduled Run not found" />;

  const targetRef = sr.spec.targetRef;
  const targetDisplay = formatScheduledRunTargetRef(sr);
  const status = getScheduledRunDisplayStatus(sr);

  return (
    <div className="min-h-screen p-4 md:p-8">
      <div className="max-w-6xl mx-auto">
        <div className="flex flex-col gap-4 lg:flex-row lg:justify-between lg:items-center mb-8">
          <div>
            <h1 className="text-2xl font-bold break-all">{sr.metadata.name}</h1>
            <p className="text-sm text-muted-foreground">
              {sr.metadata.namespace}
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              onClick={handleToggleSuspend}
              disabled={isTogglingPause}
            >
              {isTogglingPause ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : sr.spec.suspended ? (
                <PlayCircle className="h-4 w-4 mr-2" />
              ) : (
                <Pause className="h-4 w-4 mr-2" />
              )}
              {sr.spec.suspended ? "Resume" : "Suspend"}
            </Button>
            <Button
              variant="outline"
              onClick={handleTrigger}
              disabled={isTriggering}
              title={
                isTriggering
                  ? "Dispatching to agent — outcome appears in execution history"
                  : undefined
              }
            >
              {isTriggering ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <Play className="h-4 w-4 mr-2" />
              )}
              {isTriggering ? "Dispatching..." : "Trigger Now"}
            </Button>
            <Button variant="outline" onClick={handleEdit}>
              <Pencil className="h-4 w-4 mr-2" />
              Edit
            </Button>
            <Button
              variant="destructive"
              onClick={() => setShowDeleteDialog(true)}
              disabled={isDeleting}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Delete
            </Button>
          </div>
        </div>

        <Card className="mb-6">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Clock className="h-5 w-5" />
              Schedule Details
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 md:grid-cols-3 gap-6">
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Schedule
                </p>
                <p className="font-mono">{sr.spec.schedule}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Time Zone
                </p>
                <p className="font-mono">{sr.spec.timeZone || "UTC"}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Target
                </p>
                <p className="break-all">{targetDisplay}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Status
                </p>
                <Badge variant={status.variant} className={status.className} title={status.title}>
                  {status.label}
                </Badge>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Execution Timeout
                </p>
                <p>{sr.spec.executionTimeout ?? "15m"}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Recent Completed Executions
                </p>
                <p>{sr.spec.recentExecutionsLimit ?? 10}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Session Interaction
                </p>
                <p>{sr.spec.allowSessionInteraction ? "Allowed" : "Read-only"}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Last Execution
                </p>
                <p className="text-sm">{formatDateTime(sr.status?.lastExecutionTime)}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Next Scheduled Execution
                </p>
                <p className="text-sm">{formatDateTime(sr.status?.nextExecutionTime)}</p>
              </div>
            </div>
            <div className="mt-4">
              <p className="text-sm font-medium text-muted-foreground mb-1">
                Prompt
              </p>
              <div className="bg-muted p-3 rounded-md text-sm whitespace-pre-wrap break-words">
                {sr.spec.prompt}
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Execution History</CardTitle>
          </CardHeader>
          <CardContent className="overflow-x-auto">
            <ExecutionHistoryTable
              executions={executions}
              targetNamespace={scheduledRunTargetNamespace(sr)}
              targetName={targetRef.name}
            />
            {hasMoreExecutions && (
              <div className="flex justify-center pt-4">
                <Button
                  variant="outline"
                  onClick={handleLoadMoreExecutions}
                  disabled={isLoadingMoreExecutions}
                >
                  {isLoadingMoreExecutions && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  Load older executions
                </Button>
              </div>
            )}
          </CardContent>
        </Card>

        <Dialog
          open={showDeleteDialog}
          onOpenChange={(open) => {
            if (!isDeleting) setShowDeleteDialog(open);
          }}
        >
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Delete Scheduled Run</DialogTitle>
              <DialogDescription>
                Are you sure you want to delete the Scheduled Run &apos;{sr.metadata.name}&apos;?
                This action cannot be undone.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter className="flex space-x-2 justify-end">
              <Button
                variant="outline"
                onClick={() => setShowDeleteDialog(false)}
                disabled={isDeleting}
              >
                Cancel
              </Button>
              <Button variant="destructive" onClick={handleDelete} disabled={isDeleting}>
                {isDeleting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                {isDeleting ? "Deleting..." : "Delete"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </div>
  );
}
