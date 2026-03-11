"use client";
import React, { useState, useEffect, useMemo } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
    Plus,
    ChevronDown,
    ChevronRight,
    Pencil,
    Trash2,
    CheckCircle2,
    XCircle,
    Clock,
    Bot,
    RefreshCw,
    AlertTriangle,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { AgentCronJob } from "@/types";
import type { AgentResponse } from "@/types";
import { getCronJobs, deleteCronJob } from "@/app/actions/cronjobs";
import { getAgents } from "@/app/actions/agents";
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
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";

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

function relativeTime(isoTime?: string): string {
    if (!isoTime) return "";
    try {
        const now = Date.now();
        const t = new Date(isoTime).getTime();
        const diff = t - now;
        const absDiff = Math.abs(diff);
        if (absDiff < 60_000) return diff > 0 ? "in <1m" : "<1m ago";
        if (absDiff < 3_600_000) {
            const m = Math.round(absDiff / 60_000);
            return diff > 0 ? `in ${m}m` : `${m}m ago`;
        }
        if (absDiff < 86_400_000) {
            const h = Math.round(absDiff / 3_600_000);
            return diff > 0 ? `in ${h}h` : `${h}h ago`;
        }
        const d = Math.round(absDiff / 86_400_000);
        return diff > 0 ? `in ${d}d` : `${d}d ago`;
    } catch {
        return "";
    }
}

function getConditionStatus(job: AgentCronJob, conditionType: string): string | undefined {
    return job.status?.conditions?.find((c) => c.type === conditionType)?.status;
}

export default function CronJobsPage() {
    const router = useRouter();
    const [cronJobs, setCronJobs] = useState<AgentCronJob[]>([]);
    const [agents, setAgents] = useState<AgentResponse[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());
    const [jobToDelete, setJobToDelete] = useState<AgentCronJob | null>(null);
    const [agentFilter, setAgentFilter] = useState<string>("all");

    useEffect(() => {
        fetchData();
    }, []);

    const fetchData = async () => {
        try {
            setLoading(true);
            const [cronResponse, agentsResponse] = await Promise.all([
                getCronJobs(),
                getAgents(),
            ]);
            if (cronResponse.error || !cronResponse.data) {
                throw new Error(cronResponse.error || "Failed to fetch cron jobs");
            }
            setCronJobs(cronResponse.data);
            if (!agentsResponse.error && agentsResponse.data) {
                setAgents(agentsResponse.data);
            }
        } catch (err) {
            const errorMessage = err instanceof Error ? err.message : "Failed to fetch cron jobs";
            setError(errorMessage);
            toast.error(errorMessage);
        } finally {
            setLoading(false);
        }
    };

    const filteredJobs = useMemo(() => {
        if (agentFilter === "all") return cronJobs;
        return cronJobs.filter((job) => job.spec.agentRef === agentFilter);
    }, [cronJobs, agentFilter]);

    const uniqueAgentRefs = useMemo(() => {
        const refs = new Set(cronJobs.map((j) => j.spec.agentRef));
        return Array.from(refs).sort();
    }, [cronJobs]);

    const toggleRow = (ref: string) => {
        const next = new Set(expandedRows);
        if (next.has(ref)) next.delete(ref);
        else next.add(ref);
        setExpandedRows(next);
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
            await fetchData();
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
                    <div className="flex items-center gap-2">
                        <Button
                            variant="outline"
                            size="sm"
                            onClick={() => fetchData()}
                            disabled={loading}
                        >
                            <RefreshCw className={`h-4 w-4 mr-2 ${loading ? "animate-spin" : ""}`} />
                            Refresh
                        </Button>
                        <Button
                            variant="default"
                            onClick={() => router.push("/cronjobs/new")}
                        >
                            <Plus className="h-4 w-4 mr-2" />
                            New Cron Job
                        </Button>
                    </div>
                </div>

                {cronJobs.length > 0 && (
                    <div className="flex items-center gap-3 mb-4">
                        <div className="flex items-center gap-2">
                            <Bot className="h-4 w-4 text-muted-foreground" />
                            <Select value={agentFilter} onValueChange={setAgentFilter}>
                                <SelectTrigger className="w-[220px] h-8 text-sm">
                                    <SelectValue placeholder="Filter by agent" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="all">All agents</SelectItem>
                                    {uniqueAgentRefs.map((ref) => (
                                        <SelectItem key={ref} value={ref}>
                                            {ref}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                        <span className="text-sm text-muted-foreground">
                            {filteredJobs.length} of {cronJobs.length} jobs
                        </span>
                    </div>
                )}

                {loading ? (
                    <LoadingState />
                ) : cronJobs.length === 0 ? (
                    <div className="flex flex-col items-center justify-center min-h-[300px] gap-4 text-muted-foreground">
                        <Clock className="h-12 w-12 opacity-30" />
                        <p className="text-sm">No cron jobs found. Create one to get started.</p>
                    </div>
                ) : (
                    <div className="space-y-3">
                        {filteredJobs.map((job) => {
                            const ref = cronJobRef(job);
                            const isAccepted = getConditionStatus(job, "Accepted") === "True";
                            const lastResult = job.status?.lastRunResult;
                            const isExpanded = expandedRows.has(ref);

                            return (
                                <div key={ref} className="border rounded-lg overflow-hidden">
                                    <div
                                        className="flex items-center justify-between p-4 cursor-pointer hover:bg-secondary/5"
                                        onClick={() => toggleRow(ref)}
                                    >
                                        <div className="flex items-center gap-3 min-w-0 flex-1">
                                            {isExpanded ? (
                                                <ChevronDown className="h-4 w-4 shrink-0" />
                                            ) : (
                                                <ChevronRight className="h-4 w-4 shrink-0" />
                                            )}
                                            <span className="font-medium truncate">{job.metadata.name}</span>
                                            <code className="text-xs bg-muted px-2 py-0.5 rounded shrink-0">
                                                {job.spec.schedule}
                                            </code>
                                            <Badge variant="outline" className="gap-1 shrink-0">
                                                <Bot className="h-3 w-3" />
                                                {job.spec.agentRef}
                                            </Badge>
                                            {lastResult === "Success" && (
                                                <Badge variant="default" className="bg-green-600 hover:bg-green-700 gap-1 shrink-0">
                                                    <CheckCircle2 className="h-3 w-3" />
                                                    Success
                                                </Badge>
                                            )}
                                            {lastResult === "Failed" && (
                                                <Badge variant="destructive" className="gap-1 shrink-0">
                                                    <XCircle className="h-3 w-3" />
                                                    Failed
                                                </Badge>
                                            )}
                                            {!isAccepted && (
                                                <Badge variant="secondary" className="gap-1 text-yellow-600 dark:text-yellow-400 shrink-0">
                                                    <AlertTriangle className="h-3 w-3" />
                                                    Invalid
                                                </Badge>
                                            )}
                                        </div>
                                        <div className="flex items-center gap-3 shrink-0 ml-4">
                                            {job.status?.nextRunTime && (
                                                <span className="text-xs text-muted-foreground" title={formatTime(job.status.nextRunTime)}>
                                                    Next: {relativeTime(job.status.nextRunTime)}
                                                </span>
                                            )}
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
                                                variant="ghost"
                                                size="sm"
                                                className="text-destructive hover:text-destructive"
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    handleDelete(job);
                                                }}
                                            >
                                                <Trash2 className="h-4 w-4" />
                                            </Button>
                                        </div>
                                    </div>
                                    {isExpanded && (
                                        <div className="p-4 border-t bg-secondary/5">
                                            <div className="grid grid-cols-2 gap-4">
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Namespace</p>
                                                    <p className="text-sm">{job.metadata.namespace || "default"}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Schedule</p>
                                                    <p className="text-sm font-mono">{job.spec.schedule}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Agent</p>
                                                    <p className="text-sm">{job.spec.agentRef}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Last Result</p>
                                                    <p className={`text-sm ${
                                                        lastResult === "Success"
                                                            ? "text-green-600 dark:text-green-400"
                                                            : lastResult === "Failed"
                                                            ? "text-red-600 dark:text-red-400"
                                                            : ""
                                                    }`}>
                                                        {lastResult || "N/A"}
                                                    </p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Next Run</p>
                                                    <p className="text-sm">{formatTime(job.status?.nextRunTime)}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Last Run</p>
                                                    <p className="text-sm">{formatTime(job.status?.lastRunTime)}</p>
                                                </div>
                                                {job.status?.lastSessionID && (
                                                    <div>
                                                        <p className="text-sm font-medium text-muted-foreground">Last Session ID</p>
                                                        <p className="text-sm font-mono">{job.status.lastSessionID}</p>
                                                    </div>
                                                )}
                                                <div className="col-span-2">
                                                    <p className="text-sm font-medium text-muted-foreground">Prompt</p>
                                                    <pre className="mt-1 text-sm bg-muted p-3 rounded whitespace-pre-wrap">{job.spec.prompt}</pre>
                                                </div>
                                                {job.status?.lastRunMessage && (
                                                    <div className="col-span-2">
                                                        <p className="text-sm font-medium text-muted-foreground">Last Run Message</p>
                                                        <pre className="mt-1 text-sm bg-muted p-3 rounded whitespace-pre-wrap text-red-600 dark:text-red-400">{job.status.lastRunMessage}</pre>
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
