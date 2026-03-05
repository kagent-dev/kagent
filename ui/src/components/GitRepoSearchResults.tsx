"use client";
import React, { useState } from "react";
import { GitRepoSearchResult } from "@/types";
import { Button } from "@/components/ui/button";
import { Copy, Check, FileCode, ChevronDown, ChevronRight } from "lucide-react";

function ScoreBadge({ score }: { score: number }) {
    const pct = Math.round(score * 100);
    let color = "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400";
    if (pct >= 70) {
        color = "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400";
    } else if (pct >= 40) {
        color = "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400";
    }
    return (
        <span className={`text-xs px-2 py-0.5 rounded font-mono ${color}`}>
            {pct}%
        </span>
    );
}

function ChunkTypeBadge({ chunkType }: { chunkType: string }) {
    return (
        <span className="text-xs px-2 py-0.5 rounded bg-secondary text-secondary-foreground">
            {chunkType}
        </span>
    );
}

function CodeContent({ content, contextBefore, contextAfter }: {
    content: string;
    contextBefore?: string[];
    contextAfter?: string[];
}) {
    const [copied, setCopied] = useState(false);

    const handleCopy = async () => {
        await navigator.clipboard.writeText(content);
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
    };

    return (
        <div className="relative group">
            <pre className="text-xs overflow-x-auto p-3 bg-secondary/30 rounded font-mono leading-relaxed">
                {contextBefore && contextBefore.length > 0 && (
                    <span className="text-muted-foreground/50">{contextBefore.join("\n")}{"\n"}</span>
                )}
                <code>{content}</code>
                {contextAfter && contextAfter.length > 0 && (
                    <span className="text-muted-foreground/50">{"\n"}{contextAfter.join("\n")}</span>
                )}
            </pre>
            <Button
                variant="ghost"
                size="sm"
                className="absolute top-1 right-1 opacity-0 group-hover:opacity-100 transition-opacity h-7 w-7 p-0"
                onClick={handleCopy}
            >
                {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
            </Button>
        </div>
    );
}

function SearchResultCard({ result }: { result: GitRepoSearchResult }) {
    const [expanded, setExpanded] = useState(true);
    const lineRange = result.lineStart === result.lineEnd
        ? `L${result.lineStart}`
        : `L${result.lineStart}-${result.lineEnd}`;

    return (
        <div className="border rounded-lg overflow-hidden">
            <div
                className="flex items-center justify-between p-3 cursor-pointer hover:bg-secondary/5"
                onClick={() => setExpanded(!expanded)}
            >
                <div className="flex items-center gap-2 min-w-0">
                    {expanded ? (
                        <ChevronDown className="h-4 w-4 shrink-0" />
                    ) : (
                        <ChevronRight className="h-4 w-4 shrink-0" />
                    )}
                    <FileCode className="h-4 w-4 shrink-0 text-muted-foreground" />
                    <span className="font-mono text-sm truncate">{result.filePath}</span>
                    <span className="text-xs text-muted-foreground shrink-0">{lineRange}</span>
                    {result.chunkName && (
                        <span className="text-xs text-muted-foreground truncate">{result.chunkName}</span>
                    )}
                </div>
                <div className="flex items-center gap-2 shrink-0 ml-2">
                    <ChunkTypeBadge chunkType={result.chunkType} />
                    <ScoreBadge score={result.score} />
                    {result.repo && (
                        <span className="text-xs text-muted-foreground">{result.repo}</span>
                    )}
                </div>
            </div>
            {expanded && (
                <div className="px-3 pb-3">
                    <CodeContent
                        content={result.content}
                        contextBefore={result.context?.before}
                        contextAfter={result.context?.after}
                    />
                </div>
            )}
        </div>
    );
}

export function GitRepoSearchResults({ results }: { results: GitRepoSearchResult[] }) {
    if (results.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
                <p className="text-sm">No results found. Try a different query.</p>
            </div>
        );
    }

    return (
        <div className="space-y-2">
            <p className="text-sm text-muted-foreground">{results.length} result{results.length !== 1 ? "s" : ""}</p>
            {results.map((result, idx) => (
                <SearchResultCard key={`${result.repo}-${result.filePath}-${result.lineStart}-${idx}`} result={result} />
            ))}
        </div>
    );
}
