"use client";

import { useState } from "react";
import ReactMarkdown from "react-markdown";
import gfm from "remark-gfm";
import rehypeExternalLinks from "rehype-external-links";
import CodeBlock from "./CodeBlock";
import HTMLPreviewDialog from "./HTMLPreviewDialog";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function PreWithPreview({ children }: any) {
  const [showPreview, setShowPreview] = useState(false);

  if (children.props?.className?.includes("language-html")) {
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
}

const components = {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  code: ({ children, className }: any) =>
    className ? <CodeBlock className={className}>{[children]}</CodeBlock> : <code>{children}</code>,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  table: ({ children }: any) => (
    <table className="min-w-full divide-y divide-gray-300 table-fixed">{children}</table>
  ),
  pre: PreWithPreview,
};

export default function MarkdownContent({ content }: { content: string }) {
  return (
    <ReactMarkdown
      components={components}
      remarkPlugins={[gfm]}
      rehypePlugins={[[rehypeExternalLinks, { target: "_blank" }]]}
    >
      {content.trim()}
    </ReactMarkdown>
  );
}
