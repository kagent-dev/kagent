"use client";
import React, { useState } from "react";
import { Check, Copy } from "lucide-react";
import { Button } from "../ui/button";

const CodeBlock = ({ children, className }: { children: React.ReactNode[]; className: string }) => {
  const [copied, setCopied] = useState(false);

  const getCodeContent = (): string => {
    if (!children || children.length === 0) return "";
    const child = children[0];
    if (typeof child === "string") {
      return child;
    }
    if (React.isValidElement(child) && child.props && (child.props as any).children) {
      return String((child.props as any).children);
    }
    return String(child);
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
        className="absolute top-2 right-2 p-1.5 rounded-md opacity-0 group-hover:opacity-100 transition-opacity bg-background/80 hover:bg-background/90" 
        aria-label="Copy to clipboard"
        title={copied ? "Copied!" : "Copy to clipboard"}
      >
        {copied ? <Check size={16} /> : <Copy size={16} />}
      </Button>
    </div>
  );
};

export default CodeBlock;
