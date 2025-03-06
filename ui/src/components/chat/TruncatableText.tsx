import React, { memo, useState, useCallback } from "react";
import ReactMarkdown from "react-markdown";
import { ChevronUp, ChevronDown, Maximize2 } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { ScrollArea } from "@radix-ui/react-scroll-area";
import CodeBlock from "./CodeBlock";

interface TruncatableTextProps {
  content: string;
  isJson?: boolean;
  className?: string;
  jsonThreshold?: number;
  textThreshold?: number;
  showFullscreen?: boolean;
  isStreaming?: boolean;
}

const components = {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  code: (props: any) => {
    const { children, className } = props;
    // If it has a language class, it's a code block, not inline code
    if (className) {
      return <CodeBlock className={className}>{[children]}</CodeBlock>;
    }
    // For inline code, just return the default
    return <code className={className}>{children}</code>;
  },
};

export const TruncatableText = memo(({ content, isJson = false, className = "", jsonThreshold = 1000, textThreshold = 1500, showFullscreen = true, isStreaming = false }: TruncatableTextProps) => {
  const [isExpanded, setIsExpanded] = useState(false);
  const threshold = isJson ? jsonThreshold : textThreshold;

  // Only enable truncation for non-streaming, completed messages
  const shouldTruncate = !isStreaming && content.length > threshold;

  const toggleExpand = useCallback(() => {
    setIsExpanded(!isExpanded);
  }, [isExpanded]);

  const displayContent = shouldTruncate && !isExpanded ? content.slice(0, threshold) + "..." : content;

  const renderContent = () => {
    if (isJson) {
      return <pre className="whitespace-pre-wrap">{displayContent.trim()}</pre>;
    }

    return (
      <div className="relative">
        <ReactMarkdown className={`prose-md prose prose-invert max-w-none ${isStreaming ? "streaming-content" : ""}`} components={components}>
          {displayContent.trim()}
        </ReactMarkdown>

        {isStreaming && (
          <div className="inline-flex items-center ml-2">
            <div className="text-sm text-white/60 mt-1 animate-pulse">...</div>
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
          ${shouldTruncate && !isExpanded ? "max-h-[300px]" : "max-h-[10000px]"}
          ${className}
          ${isStreaming ? "streaming" : ""}
        `}
      >
        {renderContent()}
      </div>

      {shouldTruncate && !isStreaming && (
        <div className="mt-2 flex items-center justify-end gap-2">
          <button type="button" onClick={toggleExpand} className="inline-flex items-center justify-center p-2 hover:text-white/90 rounded z-10" aria-label={isExpanded ? "Show less" : "Show more"}>
            {isExpanded ? <ChevronUp size={18} /> : <ChevronDown size={18} />}
          </button>

          {showFullscreen && (
            <Dialog>
              <DialogTrigger asChild>
                <button type="button" className="inline-flex items-center justify-center p-2 rounded hover:text-white/90 z-10" aria-label="Toggle fullscreen">
                  <Maximize2 size={18} />
                </button>
              </DialogTrigger>
              <DialogContent className="bg-neutral-900 text-white border border-neutral-700 max-w-3xl max-h-[80vh]">
                <DialogHeader>
                  <DialogTitle className="text-lg font-semibold">Details</DialogTitle>
                </DialogHeader>
                <ScrollArea className="mt-4 w-full max-h-[60vh] overflow-y-auto">
                  <div className="px-6 pb-6">
                    <ReactMarkdown className="prose-md prose prose-invert max-w-none">{content}</ReactMarkdown>
                  </div>
                </ScrollArea>
              </DialogContent>
            </Dialog>
          )}
        </div>
      )}
    </div>
  );
});

TruncatableText.displayName = "TruncatableText";
