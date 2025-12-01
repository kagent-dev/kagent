import React from 'react';
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Provider } from "@/types";
import { ChevronDown, ChevronRight } from "lucide-react";

interface ValidationErrors {
  name?: string;
  selectedCombinedModel?: string;
  apiKey?: string;
  requiredParams?: Record<string, string>;
  optionalParams?: string;
}

interface ModelParam {
  id: string;
  key: string;
  value: string;
}

interface ParamsSectionProps {
  selectedProvider: Provider | null;
  requiredParams: ModelParam[];
  optionalParams: ModelParam[];
  errors: ValidationErrors;
  isSubmitting: boolean;
  isLoading: boolean;
  onRequiredParamChange: (index: number, value: string) => void;
  onOptionalParamChange: (index: number, value: string) => void;
  isExpanded?: boolean;
  onToggleExpand?: () => void;
  title?: string;
}

export const ParamsSection: React.FC<ParamsSectionProps> = ({
  selectedProvider, requiredParams, optionalParams, errors, isSubmitting, isLoading,
  onRequiredParamChange, onOptionalParamChange, isExpanded = true, onToggleExpand, title = "Parameters"
}) => {
  return (
    <Card>
      <CardHeader 
        className="flex flex-row items-center justify-between cursor-pointer"
        onClick={onToggleExpand}
      >
        <CardTitle>{title}</CardTitle>
        {onToggleExpand && (
          isExpanded 
            ? <ChevronDown className="h-5 w-5" /> 
            : <ChevronRight className="h-5 w-5" />
        )}
      </CardHeader>
      {isExpanded && (
        <CardContent className="space-y-4">
          {selectedProvider && requiredParams.length > 0 && (
            <div>
              <label className="text-sm font-medium mb-2 block text-gray-800">Required</label>
              <div className="space-y-3 pl-4 border-l-2 border-border">
                {requiredParams.map((param, index) => (
                  <div key={param.key} className="space-y-1">
                    <label htmlFor={`required-param-${param.key}`} className="text-xs font-medium text-gray-700">{param.key}</label>
                    <Input
                      id={`required-param-${param.key}`}
                      placeholder={`Enter value for ${param.key}`}
                      value={param.value}
                      onChange={(e) => onRequiredParamChange(index, e.target.value)}
                      className={errors.requiredParams?.[param.key] ? "border-destructive" : ""}
                      disabled={isSubmitting || isLoading}
                    />
                    {errors.requiredParams?.[param.key] && <p className="text-destructive text-sm mt-1">{errors.requiredParams[param.key]}</p>}
                  </div>
                ))}
              </div>
            </div>
          )}

          {selectedProvider && optionalParams.length > 0 && (
            <div>
              <label className="text-sm font-medium mb-2 block text-gray-800">Optional</label>
              <div className="space-y-3 pl-4 border-l-2 border-border">
                {optionalParams.map((param, index) => (
                  <div key={param.key || `opt-idx-${index}`} className="space-y-1"> {/* Use index for key if key is empty initially */}
                    <label htmlFor={`optional-param-${param.key}`} className="text-xs font-medium text-gray-700">{param.key || 'New Parameter'}</label>
                    <Input
                      id={`optional-param-${param.key}`}
                      placeholder={`(Optional) Enter value for ${param.key}`}
                      value={param.value}
                      onChange={(e) => onOptionalParamChange(index, e.target.value)}
                      disabled={isSubmitting || isLoading}
                    />
                  </div>
                ))}
                {errors.optionalParams && <p className="text-destructive text-sm mt-1">{errors.optionalParams}</p>}
              </div>
            </div>
          )}

          {!selectedProvider && (
            <div className="text-sm text-muted-foreground">
                Select a provider to view and configure its parameters.
            </div>
          )}
        </CardContent>
      )}
    </Card>
  );
}; 