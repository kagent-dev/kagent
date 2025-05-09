import React, { memo } from "react";
import ReactMarkdown from "react-markdown";
import CodeBlock from "./CodeBlock";

interface TruncatableTextProps {
  content: string;
  isJson?: boolean;
  className?: string;
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
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  a: (props: any) => {
    const { children, className } = props;
    return <a href={children} target="_blank" rel="noopener noreferrer" className={className}>{children}</a>;
  },
};

export const TruncatableText = memo(({ content, isJson = false, className = "", isStreaming = false }: TruncatableTextProps) => {
  const renderContent = () => {
    if (isJson) {
      return <pre className="whitespace-pre-wrap">{content.trim()}</pre>;
    }

    return (
      <div className="relative">
        <ReactMarkdown className={`prose-md prose max-w-none dark:prose-invert dark:text-primary-foreground ${isStreaming ? "streaming-content" : ""}`} components={components}>
          {content.trim()}
        </ReactMarkdown>

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
