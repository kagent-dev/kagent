"use client";

import React, { memo } from "react";
import dynamic from "next/dynamic";

const MarkdownContent = dynamic(() => import("./MarkdownContent"), {
  ssr: false,
  loading: () => <div className="h-5" aria-hidden />,
});

interface TruncatableTextProps {
  content: string;
  isJson?: boolean;
  className?: string;
  isStreaming?: boolean;
}

export const TruncatableText = memo(({ content, isJson = false, className = "", isStreaming = false }: TruncatableTextProps) => {
  const renderContent = () => {
    if (isJson) {
      return <pre className="whitespace-pre-wrap">{content.trim()}</pre>;
    }

    if (isStreaming) {
      return <div className="whitespace-pre-wrap">{content}</div>;
    }

    return (
      <div className="relative">
        <div className={`prose-md prose max-w-none dark:prose-invert dark:text-primary-foreground ${isStreaming ? "streaming-content" : ""}`}>
          <MarkdownContent content={content} />
        </div>

        {isStreaming && (
          <div className="inline-flex items-center ml-2">
            <div className="text-sm mt-1 animate-pulse">...</div>
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="relative">
      <div
        className={`
          overflow-auto scroll w-full
          ${className}
          ${isStreaming ? "streaming" : ""}
        `}
      >
        {renderContent()}
      </div>
    </div>
  );
});

TruncatableText.displayName = "TruncatableText";
