"use client";

import * as React from "react";
import { FileText, Workflow } from "lucide-react";

interface BuilderModeToggleProps {
  mode: 'form' | 'visual';
  onModeChange: (mode: 'form' | 'visual') => void;
  disabled?: boolean;
}

export function BuilderModeToggle({ mode, onModeChange, disabled }: BuilderModeToggleProps) {
  return (
    <div className="flex border border-border rounded-md overflow-hidden bg-muted/50 shadow-sm">
      <button
        className={`px-3 py-1.5 text-xs font-medium transition-all duration-200 flex items-center gap-1.5 ${
          mode === 'form'
            ? 'bg-primary text-primary-foreground shadow-sm'
            : 'bg-background text-foreground hover:bg-muted hover:text-foreground'
        } ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}
        onClick={() => !disabled && onModeChange('form')}
        disabled={disabled}
      >
        <FileText className="w-3 h-3" />
        Form Builder
      </button>
      <button
        className={`px-3 py-1.5 text-xs font-medium transition-all duration-200 flex items-center gap-1.5 ${
          mode === 'visual'
            ? 'bg-primary text-primary-foreground shadow-sm'
            : 'bg-background text-foreground hover:bg-muted hover:text-foreground'
        } ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}
        onClick={() => !disabled && onModeChange('visual')}
        disabled={disabled}
      >
        <Workflow className="w-3 h-3" />
        Visual Builder
      </button>
    </div>
  );
}
