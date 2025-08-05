import React, { memo, useState } from "react";
import ReactMarkdown from "react-markdown";
import CodeBlock from "./CodeBlock";
import gfm from 'remark-gfm'
import rehypeExternalLinks from 'rehype-external-links'
import HTMLPreviewDialog from "./HTMLPreviewDialog";

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
  table: (props: any) => {
    const { children } = props;
    return <table className="min-w-full divide-y divide-gray-300 table-fixed">{children}</table>;
  },
  pre: (props: any) => {
    const { children } = props;
    const [showPreview, setShowPreview] = useState(false);
    
    // Render the preview button and dialog for HTML preview
    if (children.props && children.props.className && children.props.className.includes("language-html")) {
      return (
        <div className="relative">
          <pre className="whitespace-pre-wrap">{children}</pre>
          <button
            onClick={() => setShowPreview(true)}
            className="absolute top-2 right-2 px-2 py-1 text-xs bg-violet-600 text-white rounded hover:bg-violet-700"
          >
            Preview
          </button>
          <HTMLPreviewDialog
            html={children.props.children}
            open={showPreview}
            onOpenChange={setShowPreview}
          />
        </div>
      );
    }
    return <pre className="whitespace-pre-wrap">{children}</pre>;
  },
};

export const TruncatableText = memo(({ content, isJson = false, className = "", isStreaming = false }: TruncatableTextProps) => {
  const renderContent = () => {
    if (isJson) {
      return <pre className="whitespace-pre-wrap">{content.trim()}</pre>;
    }

    return (
      <div className="relative">
        <ReactMarkdown
          className={`prose-md prose max-w-none dark:prose-invert dark:text-primary-foreground ${isStreaming ? "streaming-content" : ""}`}
          components={components}
          remarkPlugins={[gfm]}
          rehypePlugins={[[rehypeExternalLinks, {target: '_blank'}]]}>
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
