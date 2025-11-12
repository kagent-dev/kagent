"use client";

import * as React from "react";
import { PropertyEditor } from "./PropertyEditor";
import { getValidationSummary } from "@/lib/agent-builder/validation";
import type { NodePropertiesProps } from "@/types";
import { AlertCircle, CheckCircle, Loader2, ChevronDown, ChevronUp } from "lucide-react";
import { Button } from "@/components/ui/button";

export function NodeProperties({ selectedNode, nodes, onNodeUpdate, validationResult, onCreateAgent, isSubmitting }: NodePropertiesProps) {
  const node = nodes.find(n => n.id === selectedNode);
  const [showWarnings, setShowWarnings] = React.useState(false);
  const [showErrors, setShowErrors] = React.useState(false);
  
  const hasWarnings = validationResult && validationResult.warnings.length > 0;
  const hasErrors = validationResult && !validationResult.isValid && Object.keys(validationResult.errors).length > 0;
  
  return (
    <div className="w-80 border-l bg-muted/50 flex-shrink-0 flex flex-col h-full overflow-hidden">
      <div className="p-4 flex-1 overflow-y-auto min-h-0">
        <h3 className="font-semibold mb-4 text-sm">Properties</h3>
        
        {!node ? (
          <p className="text-sm text-muted-foreground">
            Select a node to configure its properties
          </p>
        ) : (
          <>
            <div className="mb-4 p-2 bg-background rounded border">
              <div className="text-xs font-medium text-muted-foreground">Node Type</div>
              <div className="text-sm capitalize">{node.type}</div>
            </div>
            <PropertyEditor 
              nodeType={node.type || 'unknown'}
              data={node.data}
              onUpdate={(data) => onNodeUpdate(node.id, data)}
              nodes={nodes}
            />
          </>
        )}
      </div>

      {/* Validation errors and warnings - Compact Side by Side */}
      {(hasWarnings || hasErrors) && (
        <div className="border-t bg-background">
          {/* Compact header row when both collapsed */}
          {!showWarnings && !showErrors && (
            <div className="px-4 py-2 flex items-center gap-3 flex-wrap">
              {hasErrors && (
                <button
                  onClick={() => setShowErrors(true)}
                  className="flex items-center gap-1.5 hover:opacity-80 transition-opacity"
                >
                  <AlertCircle className="h-3.5 w-3.5 text-red-600 dark:text-red-400" />
                  <span className="text-xs font-medium text-red-700 dark:text-red-300">
                    Errors ({Object.keys(validationResult!.errors).length})
                  </span>
                  <ChevronDown className="h-3 w-3 text-red-600 dark:text-red-400" />
                </button>
              )}
              {hasWarnings && (
                <button
                  onClick={() => setShowWarnings(true)}
                  className="flex items-center gap-1.5 hover:opacity-80 transition-opacity"
                >
                  <AlertCircle className="h-3.5 w-3.5 text-yellow-600 dark:text-yellow-400" />
                  <span className="text-xs font-medium text-yellow-700 dark:text-yellow-300">
                    Warnings ({validationResult!.warnings.length})
                  </span>
                  <ChevronDown className="h-3 w-3 text-yellow-600 dark:text-yellow-400" />
                </button>
              )}
            </div>
          )}

          {/* Expanded errors section */}
          {showErrors && hasErrors && (
            <div className="bg-red-50 dark:bg-red-950/50">
              <button
                onClick={() => setShowErrors(false)}
                className="w-full px-4 py-2 flex items-center justify-between hover:bg-red-100 dark:hover:bg-red-900/50 transition-colors border-t border-red-200 dark:border-red-800"
              >
                <div className="flex items-center gap-2">
                  <AlertCircle className="h-4 w-4 text-red-600 dark:text-red-400" />
                  <span className="text-xs font-medium text-red-800 dark:text-red-200">
                    Errors ({Object.keys(validationResult!.errors).length})
                  </span>
                </div>
                <ChevronUp className="h-4 w-4 text-red-600 dark:text-red-400" />
              </button>
              <div className="px-4 pb-3 space-y-1">
                {Object.entries(validationResult!.errors).map(([key, error]) => (
                  <div key={key} className="text-xs text-red-700 dark:text-red-300">
                    • {error}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Expanded warnings section */}
          {showWarnings && hasWarnings && (
            <div className="bg-yellow-50 dark:bg-yellow-950/50">
              <button
                onClick={() => setShowWarnings(false)}
                className="w-full px-4 py-2 flex items-center justify-between hover:bg-yellow-100 dark:hover:bg-yellow-900/50 transition-colors border-t border-yellow-200 dark:border-yellow-800"
              >
                <div className="flex items-center gap-2">
                  <AlertCircle className="h-4 w-4 text-yellow-600 dark:text-yellow-400" />
                  <span className="text-xs font-medium text-yellow-800 dark:text-yellow-200">
                    Warnings ({validationResult!.warnings.length})
                  </span>
                </div>
                <ChevronUp className="h-4 w-4 text-yellow-600 dark:text-yellow-400" />
              </button>
              <div className="px-4 pb-3 space-y-1">
                {validationResult!.warnings.map((warning, index) => (
                  <div key={index} className="text-xs text-yellow-700 dark:text-yellow-300">
                    • {warning}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Validation status - Only show when valid (no errors/warnings) */}
      {validationResult && validationResult.isValid && (
        <div className="px-4 py-2 border-t text-center bg-green-50 border-green-200 dark:bg-green-950 dark:border-green-800">
          <div className="flex items-center justify-center gap-2">
            <CheckCircle className="h-3 w-3 text-green-600 dark:text-green-400" />
            <div className="text-xs font-medium text-green-800 dark:text-green-200">
              {getValidationSummary(validationResult)}
            </div>
          </div>
        </div>
      )}

      {/* Create Agent Button */}
      {onCreateAgent && (
        <div className="p-4 border-t bg-background">
          <Button 
            onClick={onCreateAgent}
            disabled={isSubmitting || (validationResult ? !validationResult.isValid : false)}
            className="w-full bg-violet-500 hover:bg-violet-600"
          >
            {isSubmitting ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Creating...
              </>
            ) : (
              "Create Agent"
            )}
          </Button>
        </div>
      )}
    </div>
  );
}
