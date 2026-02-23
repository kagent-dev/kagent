"use client";

import React, { useState } from "react";
import ReactMarkdown from "react-markdown";
import gfm from "remark-gfm";
import rehypeExternalLinks from "rehype-external-links";
import CodeBlock from "./CodeBlock";
import { Braces, Brackets, Type, Hash, ToggleLeft, Ban, Check, Copy, Code, Eye } from "lucide-react";
import { Button } from "@/components/ui/button";

// ── Markdown plumbing (shared with TruncatableText) ────────────────────────

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const markdownComponents: Record<string, React.ComponentType<any>> = {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  code: (props: any) => {
    const { children, className } = props;
    if (className) return <CodeBlock className={className}>{[children]}</CodeBlock>;
    return <code className={className}>{children}</code>;
  },
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  table: (props: any) => (
    <table className="min-w-full divide-y divide-gray-300 table-fixed">{props.children}</table>
  ),
};

function MarkdownBlock({ content, className }: { content: string; className?: string }) {
  return (
    <div className={`prose-md prose max-w-none dark:prose-invert text-sm ${className ?? ""}`}>
      <ReactMarkdown
        components={markdownComponents}
        remarkPlugins={[gfm]}
        rehypePlugins={[[rehypeExternalLinks, { target: "_blank" }]]}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}

// ── Helpers ─────────────────────────────────────────────────────────────────

function tryParseJson(s: string): unknown | null {
  const trimmed = s.trim();
  if (!trimmed.startsWith("{") && !trimmed.startsWith("[")) return null;
  try {
    return JSON.parse(trimmed);
  } catch {
    return null;
  }
}

const MARKDOWN_RE = /^#{1,6}\s|^\s*[-*+]\s|\*\*|__|\[.*\]\(.*\)|```|^\s*\d+\.\s|^\s*>/m;

function looksLikeMarkdown(s: string): boolean {
  return MARKDOWN_RE.test(s);
}

function isInlineValue(value: unknown): boolean {
  if (value === null || value === undefined) return true;
  if (typeof value === "boolean" || typeof value === "number") return true;
  if (typeof value === "string") {
    if (value.length > 80 || value.includes("\n")) return false;
    if (tryParseJson(value) !== null) return false;
    return true;
  }
  return false;
}

function rawSource(data: unknown): string {
  if (typeof data === "string") return data;
  return JSON.stringify(data, null, 2);
}

// ── Type icons ──────────────────────────────────────────────────────────────

function TypeIcon({ value }: { value: unknown }) {
  const cls = "w-3 h-3 shrink-0";
  if (value === null || value === undefined) return <Ban className={cls} />;
  if (typeof value === "boolean") return <ToggleLeft className={cls} />;
  if (typeof value === "number") return <Hash className={cls} />;
  if (typeof value === "string") return <Type className={cls} />;
  if (Array.isArray(value)) return <Brackets className={cls} />;
  if (typeof value === "object") return <Braces className={cls} />;
  return null;
}

// ── Recursive value renderer ────────────────────────────────────────────────

function ValueRenderer({ value, className }: { value: unknown; className?: string }) {
  if (value === null || value === undefined) {
    return <span className="text-xs text-muted-foreground italic">null</span>;
  }

  if (typeof value === "boolean") {
    return <span className={`text-sm ${className ?? ""}`}>{value ? "true" : "false"}</span>;
  }

  if (typeof value === "number") {
    return <span className={`text-sm ${className ?? ""}`}>{String(value)}</span>;
  }

  if (typeof value === "string") {
    return <StringRenderer content={value} className={className} />;
  }

  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="text-xs text-muted-foreground italic">{"[]"}</span>;
    return (
      <div className={`space-y-1 ${className ?? ""}`}>
        {value.map((item, i) => (
          <div key={i} className="ml-1 pl-2 border-l border-border">
            <ValueRenderer value={item} />
          </div>
        ))}
      </div>
    );
  }

  if (typeof value === "object") {
    return <ObjectRenderer obj={value as Record<string, unknown>} className={className} />;
  }

  return <span className="text-sm">{String(value)}</span>;
}

function StringRenderer({ content, className }: { content: string; className?: string }) {
  const parsed = tryParseJson(content);
  if (parsed !== null && typeof parsed === "object") {
    return <ValueRenderer value={parsed} className={className} />;
  }

  if (content.includes("\n") || looksLikeMarkdown(content)) {
    return <MarkdownBlock content={content} className={className} />;
  }

  return <span className={`text-sm break-words ${className ?? ""}`}>{content}</span>;
}

function ObjectRenderer({ obj, className }: { obj: Record<string, unknown>; className?: string }) {
  const entries = Object.entries(obj);
  if (entries.length === 0) {
    return <span className="text-xs text-muted-foreground italic">{"{}"}</span>;
  }

  return (
    <div className={`space-y-1 ${className ?? ""}`}>
      {entries.map(([key, val]) => {
        const inline = isInlineValue(val);
        if (inline) {
          return (
            <div key={key} className="flex items-baseline gap-1.5 min-w-0">
              <div className="flex items-center gap-1 text-muted-foreground shrink-0">
                <TypeIcon value={val} />
                <span className="text-xs font-medium">{key}</span>
              </div>
              <div className="min-w-0 break-words">
                <ValueRenderer value={val} />
              </div>
            </div>
          );
        }
        return (
          <div key={key}>
            <div className="flex items-center gap-1 text-muted-foreground mb-0.5">
              <TypeIcon value={val} />
              <span className="text-xs font-medium">{key}</span>
            </div>
            <div className="ml-1 pl-2 border-l border-border">
              <ValueRenderer value={val} />
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ── Public API ──────────────────────────────────────────────────────────────

export function SmartContent({ data, className }: { data: unknown; className?: string }) {
  const [viewSource, setViewSource] = useState(false);
  const [copied, setCopied] = useState(false);

  const source = rawSource(data);

  const handleCopy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(source);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch { /* clipboard unavailable */ }
  };

  const handleToggleSource = (e: React.MouseEvent) => {
    e.stopPropagation();
    setViewSource(v => !v);
  };

  return (
    <div className="relative overflow-hidden">
      <div className="absolute top-0 right-0 flex items-center gap-0.5 z-10">
        <Button variant="ghost" size="icon" className="h-5 w-5" onClick={handleToggleSource} title={viewSource ? "Formatted view" : "View source"}>
          {viewSource ? <Eye className="w-3 h-3" /> : <Code className="w-3 h-3" />}
        </Button>
        <Button variant="ghost" size="icon" className="h-5 w-5" onClick={handleCopy} title={copied ? "Copied!" : "Copy to clipboard"}>
          {copied ? <Check className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
        </Button>
      </div>
      {viewSource ? (
        <pre className={`text-sm whitespace-pre-wrap break-words ${className ?? ""}`}>{source}</pre>
      ) : (
        <ValueRenderer value={data} className={className} />
      )}
    </div>
  );
}

export function parseContentString(content: string): unknown {
  const trimmed = content.trim();
  if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
    try { return JSON.parse(trimmed); } catch { /* fall through */ }
  }
  return trimmed;
}
