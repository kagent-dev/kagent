"use client";

import React, { useState, useEffect, useCallback } from "react";
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
import { ScheduledRun } from "@/types";
import {
  getScheduledRun,
  deleteScheduledRun,
  triggerScheduledRun,
  updateScheduledRun,
} from "@/app/actions/scheduledRuns";
import { RunHistoryTable } from "@/components/schedules/RunHistoryTable";
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

export default function ScheduledRunDetailPage() {
  const router = useRouter();
  const params = useParams();
  const namespace = params.namespace as string;
  const name = params.name as string;

  const [sr, setSr] = useState<ScheduledRun | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const [isTriggering, setIsTriggering] = useState(false);
  const [isTogglingPause, setIsTogglingPause] = useState(false);

  const fetchData = useCallback(async () => {
    try {
      setLoading(true);
      const response = await getScheduledRun(name, namespace);
      if (response.error || !response.data) {
        throw new Error(response.error || "Scheduled run not found");
      }
      setSr(response.data);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to fetch scheduled run";
      setError(errorMessage);
    } finally {
      setLoading(false);
    }
  }, [name, namespace]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- legitimate data fetch on mount/dependency change
    fetchData();
  }, [fetchData]);

  const handleEdit = () => {
    router.push(`/schedules/new?edit=true&name=${name}&namespace=${namespace}`);
  };

  const handleDelete = async () => {
    try {
      const response = await deleteScheduledRun(name, namespace);
      if (response.error) {
        throw new Error(response.error);
      }
      toast.success(`Scheduled run "${name}" deleted successfully`);
      router.push("/schedules");
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to delete scheduled run";
      toast.error(errorMessage);
      setShowDeleteDialog(false);
    }
  };

  const handleTrigger = async () => {
    setIsTriggering(true);
    try {
      const response = await triggerScheduledRun(name, namespace);
      if (response.error) {
        throw new Error(response.error);
      }
      toast.success(`Triggered run for "${name}"`);
      await fetchData();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to trigger scheduled run";
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
          suspend: !sr.spec.suspend,
        },
      };
      const response = await updateScheduledRun(updated);
      if (response.error) {
        throw new Error(response.error);
      }
      toast.success(sr.spec.suspend ? "Schedule resumed" : "Schedule suspended");
      await fetchData();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to update scheduled run";
      toast.error(errorMessage);
    } finally {
      setIsTogglingPause(false);
    }
  };

  if (loading) return <LoadingState />;
  if (error) return <ErrorState message={error} />;
  if (!sr) return <ErrorState message="Scheduled run not found" />;

  const agentRef = sr.spec.agentRef;
  const agentDisplay = agentRef.namespace
    ? `${agentRef.namespace}/${agentRef.name}`
    : agentRef.name;

  return (
    <div className="min-h-screen p-8">
      <div className="max-w-6xl mx-auto">
        {/* Header */}
        <div className="flex justify-between items-center mb-8">
          <div>
            <h1 className="text-2xl font-bold">{sr.metadata.name}</h1>
            <p className="text-sm text-muted-foreground">
              {sr.metadata.namespace}
            </p>
          </div>
          <div className="flex space-x-2">
            <Button
              variant="outline"
              onClick={handleToggleSuspend}
              disabled={isTogglingPause}
            >
              {isTogglingPause ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : sr.spec.suspend ? (
                <PlayCircle className="h-4 w-4 mr-2" />
              ) : (
                <Pause className="h-4 w-4 mr-2" />
              )}
              {sr.spec.suspend ? "Resume" : "Suspend"}
            </Button>
            <Button
              variant="outline"
              onClick={handleTrigger}
              disabled={isTriggering}
            >
              {isTriggering ? (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              ) : (
                <Play className="h-4 w-4 mr-2" />
              )}
              Trigger Now
            </Button>
            <Button variant="outline" onClick={handleEdit}>
              <Pencil className="h-4 w-4 mr-2" />
              Edit
            </Button>
            <Button
              variant="destructive"
              onClick={() => setShowDeleteDialog(true)}
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Delete
            </Button>
          </div>
        </div>

        {/* Details Card */}
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
                  Agent
                </p>
                <p>{agentDisplay}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Status
                </p>
                {sr.spec.suspend ? (
                  <Badge variant="secondary">Suspended</Badge>
                ) : (
                  <Badge variant="outline" className="text-green-600 border-green-600">
                    Active
                  </Badge>
                )}
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Concurrency Policy
                </p>
                <p>{sr.spec.concurrencyPolicy || "Forbid"}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Max Run History
                </p>
                <p>{sr.spec.maxRunHistory ?? 10}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Active Runs
                </p>
                <p>{sr.status?.active ?? 0}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Last Run
                </p>
                <p className="text-sm">{formatDateTime(sr.status?.lastRunTime)}</p>
              </div>
              <div>
                <p className="text-sm font-medium text-muted-foreground">
                  Next Run
                </p>
                <p className="text-sm">{formatDateTime(sr.status?.nextRunTime)}</p>
              </div>
            </div>
            <div className="mt-4">
              <p className="text-sm font-medium text-muted-foreground mb-1">
                Prompt
              </p>
              <div className="bg-muted p-3 rounded-md text-sm whitespace-pre-wrap">
                {sr.spec.prompt}
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Run History */}
        <Card>
          <CardHeader>
            <CardTitle>Run History</CardTitle>
          </CardHeader>
          <CardContent>
            <RunHistoryTable
              entries={sr.status?.runHistory || []}
              agentNamespace={agentRef.namespace}
              agentName={agentRef.name}
            />
          </CardContent>
        </Card>

        {/* Delete Dialog */}
        <Dialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Delete Scheduled Run</DialogTitle>
              <DialogDescription>
                Are you sure you want to delete the scheduled run &apos;{sr.metadata.name}&apos;?
                This action cannot be undone.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter className="flex space-x-2 justify-end">
              <Button
                variant="outline"
                onClick={() => setShowDeleteDialog(false)}
              >
                Cancel
              </Button>
              <Button variant="destructive" onClick={handleDelete}>
                Delete
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>
    </div>
  );
}
