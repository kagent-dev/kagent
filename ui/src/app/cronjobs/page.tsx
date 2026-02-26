"use client";
import React, { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Plus, ChevronDown, ChevronRight, Pencil, Trash2, CheckCircle2, XCircle, Clock } from "lucide-react";
import { useRouter } from "next/navigation";
import { AgentCronJob } from "@/types";
import { getCronJobs, deleteCronJob } from "@/app/actions/cronjobs";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { toast } from "sonner";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";

function cronJobRef(job: AgentCronJob): string {
    return `${job.metadata.namespace || "default"}/${job.metadata.name}`;
}

function formatTime(isoTime?: string): string {
    if (!isoTime) return "N/A";
    try {
        return new Date(isoTime).toLocaleString();
    } catch {
        return isoTime;
    }
}

function getConditionStatus(job: AgentCronJob, conditionType: string): string | undefined {
    return job.status?.conditions?.find((c) => c.type === conditionType)?.status;
}

export default function CronJobsPage() {
    const router = useRouter();
    const [cronJobs, setCronJobs] = useState<AgentCronJob[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());
    const [jobToDelete, setJobToDelete] = useState<AgentCronJob | null>(null);

    useEffect(() => {
        fetchCronJobs();
    }, []);

    const fetchCronJobs = async () => {
        try {
            setLoading(true);
            const response = await getCronJobs();
            if (response.error || !response.data) {
                throw new Error(response.error || "Failed to fetch cron jobs");
            }
            setCronJobs(response.data);
        } catch (err) {
            const errorMessage = err instanceof Error ? err.message : "Failed to fetch cron jobs";
            setError(errorMessage);
            toast.error(errorMessage);
        } finally {
            setLoading(false);
        }
    };

    const toggleRow = (ref: string) => {
        const newExpandedRows = new Set(expandedRows);
        if (expandedRows.has(ref)) {
            newExpandedRows.delete(ref);
        } else {
            newExpandedRows.add(ref);
        }
        setExpandedRows(newExpandedRows);
    };

    const handleEdit = (job: AgentCronJob) => {
        router.push(`/cronjobs/new?edit=true&name=${job.metadata.name}&namespace=${job.metadata.namespace || "default"}`);
    };

    const handleDelete = (job: AgentCronJob) => {
        setJobToDelete(job);
    };

    const confirmDelete = async () => {
        if (!jobToDelete) return;

        const ref = cronJobRef(jobToDelete);
        try {
            const response = await deleteCronJob(
                jobToDelete.metadata.namespace || "default",
                jobToDelete.metadata.name
            );
            if (response.error) {
                throw new Error(response.error || "Failed to delete cron job");
            }
            toast.success(`Cron job "${ref}" deleted successfully`);
            setJobToDelete(null);
            await fetchCronJobs();
        } catch (err) {
            const errorMessage = err instanceof Error ? err.message : "Failed to delete cron job";
            toast.error(errorMessage);
            setJobToDelete(null);
        }
    };

    if (error) {
        return <ErrorState message={error} />;
    }

    return (
        <div className="min-h-screen p-8">
            <div className="max-w-6xl mx-auto">
                <div className="flex justify-between items-center mb-8">
                    <h1 className="text-2xl font-bold">Cron Jobs</h1>
                    <Button
                        variant="default"
                        onClick={() => router.push("/cronjobs/new")}
                    >
                        <Plus className="h-4 w-4 mr-2" />
                        New Cron Job
                    </Button>
                </div>

                {loading ? (
                    <LoadingState />
                ) : cronJobs.length === 0 ? (
                    <div className="flex flex-col items-center justify-center min-h-[300px] gap-4 text-muted-foreground">
                        <Clock className="h-12 w-12 opacity-30" />
                        <p className="text-sm">No cron jobs found. Create one to get started.</p>
                    </div>
                ) : (
                    <div className="space-y-4">
                        {cronJobs.map((job) => {
                            const ref = cronJobRef(job);
                            const isAccepted = getConditionStatus(job, "Accepted") === "True";
                            const lastResult = job.status?.lastRunResult;

                            return (
                                <div key={ref} className="border rounded-lg overflow-hidden">
                                    <div
                                        className="flex items-center justify-between p-4 cursor-pointer hover:bg-secondary/5"
                                        onClick={() => toggleRow(ref)}
                                    >
                                        <div className="flex items-center space-x-3">
                                            {expandedRows.has(ref) ? (
                                                <ChevronDown className="h-4 w-4" />
                                            ) : (
                                                <ChevronRight className="h-4 w-4" />
                                            )}
                                            <span className="font-medium">{ref}</span>
                                            <code className="text-xs bg-muted px-2 py-0.5 rounded">{job.spec.schedule}</code>
                                            {lastResult === "Success" && (
                                                <CheckCircle2 className="h-4 w-4 text-green-500" />
                                            )}
                                            {lastResult === "Failed" && (
                                                <XCircle className="h-4 w-4 text-red-500" />
                                            )}
                                            {!isAccepted && (
                                                <span className="text-xs text-yellow-600 bg-yellow-100 dark:bg-yellow-900/30 dark:text-yellow-400 px-2 py-0.5 rounded">
                                                    Invalid
                                                </span>
                                            )}
                                        </div>
                                        <div className="flex space-x-2">
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    handleEdit(job);
                                                }}
                                            >
                                                <Pencil className="h-4 w-4" />
                                            </Button>
                                            <Button
                                                variant="destructive"
                                                size="sm"
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    handleDelete(job);
                                                }}
                                            >
                                                <Trash2 className="h-4 w-4" />
                                            </Button>
                                        </div>
                                    </div>
                                    {expandedRows.has(ref) && (
                                        <div className="p-4 border-t bg-secondary/10">
                                            <div className="grid grid-cols-2 gap-4">
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Schedule</p>
                                                    <p><code>{job.spec.schedule}</code></p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Agent</p>
                                                    <p>{job.spec.agentRef}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Next Run</p>
                                                    <p>{formatTime(job.status?.nextRunTime)}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Last Run</p>
                                                    <p>{formatTime(job.status?.lastRunTime)}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Last Result</p>
                                                    <p className={
                                                        lastResult === "Success"
                                                            ? "text-green-600 dark:text-green-400"
                                                            : lastResult === "Failed"
                                                            ? "text-red-600 dark:text-red-400"
                                                            : ""
                                                    }>
                                                        {lastResult || "N/A"}
                                                    </p>
                                                </div>
                                                {job.status?.lastSessionID && (
                                                    <div>
                                                        <p className="text-sm font-medium text-muted-foreground">Last Session ID</p>
                                                        <p className="text-sm font-mono">{job.status.lastSessionID}</p>
                                                    </div>
                                                )}
                                                <div className="col-span-2">
                                                    <p className="text-sm font-medium text-muted-foreground">Prompt</p>
                                                    <pre className="mt-1 text-sm bg-muted p-2 rounded whitespace-pre-wrap">
                                                        {job.spec.prompt}
                                                    </pre>
                                                </div>
                                                {job.status?.lastRunMessage && (
                                                    <div className="col-span-2">
                                                        <p className="text-sm font-medium text-muted-foreground">Last Run Message</p>
                                                        <pre className="mt-1 text-sm bg-muted p-2 rounded whitespace-pre-wrap text-red-600 dark:text-red-400">
                                                            {job.status.lastRunMessage}
                                                        </pre>
                                                    </div>
                                                )}
                                            </div>
                                        </div>
                                    )}
                                </div>
                            );
                        })}
                    </div>
                )}

                <Dialog open={jobToDelete !== null} onOpenChange={(open) => !open && setJobToDelete(null)}>
                    <DialogContent>
                        <DialogHeader>
                            <DialogTitle>Delete Cron Job</DialogTitle>
                            <DialogDescription>
                                Are you sure you want to delete the cron job &apos;{jobToDelete ? cronJobRef(jobToDelete) : ""}&apos;? This action cannot be undone.
                            </DialogDescription>
                        </DialogHeader>
                        <DialogFooter className="flex space-x-2 justify-end">
                            <Button
                                variant="outline"
                                onClick={() => setJobToDelete(null)}
                            >
                                Cancel
                            </Button>
                            <Button
                                variant="destructive"
                                onClick={confirmDelete}
                            >
                                Delete
                            </Button>
                        </DialogFooter>
                    </DialogContent>
                </Dialog>
            </div>
        </div>
    );
}
