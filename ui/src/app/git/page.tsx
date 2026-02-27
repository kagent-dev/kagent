"use client";
import React, { useState, useEffect, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Plus, ChevronDown, ChevronRight, Trash2, RefreshCw, Database, GitFork, Search, X, Loader2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { GitRepo, GitRepoSearchResult } from "@/types";
import { getGitRepos, deleteGitRepo, syncGitRepo, indexGitRepo, searchGitRepos } from "@/app/actions/gitrepos";
import { LoadingState } from "@/components/LoadingState";
import { ErrorState } from "@/components/ErrorState";
import { GitRepoSearchResults } from "@/components/GitRepoSearchResults";
import { toast } from "sonner";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";

function formatTime(isoTime?: string): string {
    if (!isoTime) return "N/A";
    try {
        return new Date(isoTime).toLocaleString();
    } catch {
        return isoTime;
    }
}

function StatusBadge({ status, error }: { status: string; error?: string }) {
    const config: Record<string, { bg: string; text: string; label: string }> = {
        indexed: { bg: "bg-green-100 dark:bg-green-900/30", text: "text-green-700 dark:text-green-400", label: "Indexed" },
        cloned: { bg: "bg-blue-100 dark:bg-blue-900/30", text: "text-blue-700 dark:text-blue-400", label: "Cloned" },
        cloning: { bg: "bg-yellow-100 dark:bg-yellow-900/30", text: "text-yellow-700 dark:text-yellow-400", label: "Cloning..." },
        indexing: { bg: "bg-yellow-100 dark:bg-yellow-900/30", text: "text-yellow-700 dark:text-yellow-400", label: "Indexing..." },
        error: { bg: "bg-red-100 dark:bg-red-900/30", text: "text-red-700 dark:text-red-400", label: "Error" },
    };
    const c = config[status] || config.error;
    return (
        <span className={`text-xs px-2 py-0.5 rounded ${c.bg} ${c.text}`} title={error}>
            {c.label}
        </span>
    );
}

export default function GitReposPage() {
    const router = useRouter();
    const [repos, setRepos] = useState<GitRepo[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set());
    const [repoToDelete, setRepoToDelete] = useState<GitRepo | null>(null);
    const [busyRepos, setBusyRepos] = useState<Set<string>>(new Set());
    const [searchQuery, setSearchQuery] = useState("");
    const [searchResults, setSearchResults] = useState<GitRepoSearchResult[] | null>(null);
    const [searching, setSearching] = useState(false);

    const handleSearch = async () => {
        const query = searchQuery.trim();
        if (!query) return;
        setSearching(true);
        try {
            const response = await searchGitRepos({ query, limit: 20, contextLines: 3 });
            if (response.error || !response.data) {
                throw new Error(response.error || "Search failed");
            }
            setSearchResults(response.data);
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Search failed");
        } finally {
            setSearching(false);
        }
    };

    const clearSearch = () => {
        setSearchQuery("");
        setSearchResults(null);
    };

    const fetchRepos = useCallback(async () => {
        try {
            setLoading(true);
            const response = await getGitRepos();
            if (response.error || !response.data) {
                throw new Error(response.error || "Failed to fetch git repos");
            }
            setRepos(response.data);
        } catch (err) {
            const errorMessage = err instanceof Error ? err.message : "Failed to fetch git repos";
            setError(errorMessage);
            toast.error(errorMessage);
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        fetchRepos();
    }, [fetchRepos]);

    const toggleRow = (name: string) => {
        const next = new Set(expandedRows);
        if (next.has(name)) {
            next.delete(name);
        } else {
            next.add(name);
        }
        setExpandedRows(next);
    };

    const handleSync = async (repo: GitRepo) => {
        setBusyRepos((prev) => new Set(prev).add(repo.name));
        try {
            const response = await syncGitRepo(repo.name);
            if (response.error) {
                throw new Error(response.error);
            }
            toast.success(`Repo "${repo.name}" synced successfully`);
            await fetchRepos();
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Failed to sync repo");
        } finally {
            setBusyRepos((prev) => {
                const next = new Set(prev);
                next.delete(repo.name);
                return next;
            });
        }
    };

    const handleIndex = async (repo: GitRepo) => {
        setBusyRepos((prev) => new Set(prev).add(repo.name));
        try {
            const response = await indexGitRepo(repo.name);
            if (response.error) {
                throw new Error(response.error);
            }
            toast.success(`Indexing started for "${repo.name}"`);
            await fetchRepos();
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Failed to index repo");
        } finally {
            setBusyRepos((prev) => {
                const next = new Set(prev);
                next.delete(repo.name);
                return next;
            });
        }
    };

    const handleDelete = (repo: GitRepo) => {
        setRepoToDelete(repo);
    };

    const confirmDelete = async () => {
        if (!repoToDelete) return;

        try {
            const response = await deleteGitRepo(repoToDelete.name);
            if (response.error) {
                throw new Error(response.error);
            }
            toast.success(`Repo "${repoToDelete.name}" deleted successfully`);
            setRepoToDelete(null);
            await fetchRepos();
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Failed to delete repo");
            setRepoToDelete(null);
        }
    };

    if (error) {
        return <ErrorState message={error} />;
    }

    return (
        <div className="min-h-screen p-8">
            <div className="max-w-6xl mx-auto">
                <div className="flex justify-between items-center mb-8">
                    <h1 className="text-2xl font-bold">GIT Repos</h1>
                    <Button
                        variant="default"
                        onClick={() => router.push("/git/new")}
                    >
                        <Plus className="h-4 w-4 mr-2" />
                        Add Repo
                    </Button>
                </div>

                <div className="flex gap-2 mb-6">
                    <div className="relative flex-1">
                        <Input
                            placeholder="Search across all indexed repos..."
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                            onKeyDown={(e) => { if (e.key === "Enter") handleSearch(); }}
                            className="pr-8"
                        />
                        {searchQuery && (
                            <button
                                onClick={clearSearch}
                                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                            >
                                <X className="h-4 w-4" />
                            </button>
                        )}
                    </div>
                    <Button onClick={handleSearch} disabled={searching || !searchQuery.trim()}>
                        {searching ? <Loader2 className="h-4 w-4 animate-spin" /> : <Search className="h-4 w-4" />}
                    </Button>
                </div>

                {searchResults !== null && (
                    <div className="mb-6">
                        {searching ? <LoadingState /> : <GitRepoSearchResults results={searchResults} />}
                    </div>
                )}

                {loading ? (
                    <LoadingState />
                ) : repos.length === 0 ? (
                    <div className="flex flex-col items-center justify-center min-h-[300px] gap-4 text-muted-foreground">
                        <GitFork className="h-12 w-12 opacity-30" />
                        <p className="text-sm">No git repos found. Add one to get started.</p>
                    </div>
                ) : (
                    <div className="space-y-4">
                        {repos.map((repo) => {
                            const isBusy = busyRepos.has(repo.name) || repo.status === "cloning" || repo.status === "indexing";

                            return (
                                <div key={repo.name} className="border rounded-lg overflow-hidden">
                                    <div
                                        className="flex items-center justify-between p-4 cursor-pointer hover:bg-secondary/5"
                                        onClick={() => toggleRow(repo.name)}
                                    >
                                        <div className="flex items-center space-x-3">
                                            {expandedRows.has(repo.name) ? (
                                                <ChevronDown className="h-4 w-4" />
                                            ) : (
                                                <ChevronRight className="h-4 w-4" />
                                            )}
                                            <span className="font-medium">{repo.name}</span>
                                            <StatusBadge status={repo.status} error={repo.error} />
                                            {repo.fileCount > 0 && (
                                                <span className="text-xs text-muted-foreground">
                                                    {repo.fileCount} files / {repo.chunkCount} chunks
                                                </span>
                                            )}
                                        </div>
                                        <div className="flex space-x-2">
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                title="Sync (git pull)"
                                                disabled={isBusy}
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    handleSync(repo);
                                                }}
                                            >
                                                <RefreshCw className={`h-4 w-4 ${isBusy ? "animate-spin" : ""}`} />
                                            </Button>
                                            <Button
                                                variant="ghost"
                                                size="sm"
                                                title="Re-index"
                                                disabled={isBusy}
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    handleIndex(repo);
                                                }}
                                            >
                                                <Database className="h-4 w-4" />
                                            </Button>
                                            <Button
                                                variant="destructive"
                                                size="sm"
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    handleDelete(repo);
                                                }}
                                            >
                                                <Trash2 className="h-4 w-4" />
                                            </Button>
                                        </div>
                                    </div>
                                    {expandedRows.has(repo.name) && (
                                        <div className="p-4 border-t bg-secondary/10">
                                            <div className="grid grid-cols-2 gap-4">
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">URL</p>
                                                    <p className="font-mono text-sm break-all">{repo.url}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Branch</p>
                                                    <p>{repo.branch}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Status</p>
                                                    <p>{repo.status}{repo.error ? ` - ${repo.error}` : ""}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Files / Chunks</p>
                                                    <p>{repo.fileCount} / {repo.chunkCount}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Last Synced</p>
                                                    <p>{formatTime(repo.lastSynced)}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Last Indexed</p>
                                                    <p>{formatTime(repo.lastIndexed)}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Created</p>
                                                    <p>{formatTime(repo.createdAt)}</p>
                                                </div>
                                                <div>
                                                    <p className="text-sm font-medium text-muted-foreground">Updated</p>
                                                    <p>{formatTime(repo.updatedAt)}</p>
                                                </div>
                                            </div>
                                        </div>
                                    )}
                                </div>
                            );
                        })}
                    </div>
                )}

                <Dialog open={repoToDelete !== null} onOpenChange={(open) => !open && setRepoToDelete(null)}>
                    <DialogContent>
                        <DialogHeader>
                            <DialogTitle>Delete Git Repo</DialogTitle>
                            <DialogDescription>
                                Are you sure you want to delete the repo &apos;{repoToDelete?.name}&apos;? This will remove the cloned files and all indexed data. This action cannot be undone.
                            </DialogDescription>
                        </DialogHeader>
                        <DialogFooter className="flex space-x-2 justify-end">
                            <Button
                                variant="outline"
                                onClick={() => setRepoToDelete(null)}
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
