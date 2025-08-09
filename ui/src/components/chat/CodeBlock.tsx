"use client";
import React, { useState } from "react";
import { Check, Copy } from "lucide-react";
import { Button } from "../ui/button";

const hasChildren = (props: unknown): props is { children: React.ReactNode } => {
  return typeof props === 'object' && props !== null && 'children' in props;
};

const extractTextFromReactNode = (node: React.ReactNode): string => {
  if (typeof node === "string") {
    return node;
  }
  if (Array.isArray(node)) {
    return node.map(extractTextFromReactNode).join("");
  }
  if (React.isValidElement(node) && node.props && hasChildren(node.props)) {
    return extractTextFromReactNode(node.props.children);
  }
  return String(node || "");
};

const CodeBlock = ({ children, className }: { children: React.ReactNode[]; className: string }) => {
  const [copied, setCopied] = useState(false);

  const getCodeContent = (): string => {
    if (!children || children.length === 0) return "";
    return extractTextFromReactNode(children[0]);
  };

  const handleCopy = async () => {
    const codeContent = getCodeContent();
    if (codeContent) {
      await navigator.clipboard.writeText(codeContent);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className="relative group">
      <pre className={className}>
        <code className={className}>{children}</code>
      </pre>
      <Button 
        variant="link" 
        onClick={handleCopy} 
        className="absolute top-2 right-2 p-2.5 rounded-md opacity-0 group-hover:opacity-100 transition-opacity bg-background/80 hover:bg-background/90" 
        aria-label="Copy to clipboard"
        title={copied ? "Copied!" : "Copy to clipboard"}
      >
        {copied ? <Check size={16} /> : <Copy size={16} />}
      </Button>
    </div>
  );
};

export default CodeBlock;
