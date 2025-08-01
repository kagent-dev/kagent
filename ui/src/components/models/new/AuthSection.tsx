import React from 'react';
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Eye, EyeOff, ExternalLinkIcon } from "lucide-react";
import Link from "next/link";
import { Provider } from "@/types"; 
import { PROVIDERS_INFO, getProviderFormKey, BackendModelProviderType } from "@/lib/providers"; 
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";

interface ValidationErrors {
  name?: string;
  selectedCombinedModel?: string;
  apiKey?: string;
  requiredParams?: Record<string, string>;
  optionalParams?: string;
}

interface AuthSectionProps {
  isOllamaSelected: boolean;
  isEditMode: boolean;
  apiKey: string;
  showApiKey: boolean;
  errors: ValidationErrors;
  isSubmitting: boolean;
  isLoading: boolean;
  onApiKeyChange: (value: string) => void;
  onToggleShowApiKey: () => void;
  selectedProvider: Provider | null;
  isApiKeyNeeded: boolean;
  onApiKeyNeededChange: (isApiKeyNeeded: boolean) => void;
}

export const AuthSection: React.FC<AuthSectionProps> = ({
  isOllamaSelected, isEditMode, apiKey, showApiKey, errors, isSubmitting,
  isLoading, onApiKeyChange, onToggleShowApiKey, selectedProvider,
  isApiKeyNeeded, onApiKeyNeededChange
}) => {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Authentication</CardTitle>
      </CardHeader>
      <CardContent>
        {!isOllamaSelected ? (
          <div>
            <label className="text-sm mb-2 block">
              API Key {isEditMode && "(Leave blank to keep existing)"}
            </label>
            <div className="flex items-center space-x-2">
              <div className="relative flex-grow">
                 <Input
                   type={showApiKey ? "text" : "password"}
                   value={apiKey}
                   onChange={(e) => onApiKeyChange(e.target.value)}
                   className={`${errors.apiKey ? "border-destructive" : ""} pr-10 w-full`}
                   placeholder={isEditMode ? "Enter new API key to update" : "Enter API key..."}
                   disabled={isSubmitting || isLoading || !isApiKeyNeeded || isOllamaSelected}
                   autoComplete="new-password"
                 />
                 <Button
                   type="button"
                   variant="ghost"
                   size="sm"
                   className="absolute right-0 top-0 h-full px-3"
                   onClick={onToggleShowApiKey}
                   disabled={isSubmitting || isLoading || !isApiKeyNeeded || isOllamaSelected}
                   title={showApiKey ? "Hide API Key" : "Show API Key"}
                 >
                   {showApiKey ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                 </Button>
               </div>
               {selectedProvider && (
                  (() => {
                     const providerKey = getProviderFormKey(selectedProvider.type as BackendModelProviderType);
                     const providerInfo = providerKey ? PROVIDERS_INFO[providerKey] : undefined;
                     return providerInfo?.apiKeyLink ? (
                       <Button variant="outline" size="icon" asChild>
                         <Link href={providerInfo.apiKeyLink} target="_blank" rel="noopener noreferrer" title={`Find your ${selectedProvider.name} API key`}>
                           <ExternalLinkIcon className="h-4 w-4" />
                         </Link>
                       </Button>
                     ) : null;
                  })()
               )}
             </div>
             {errors.apiKey && <p className="text-destructive text-sm mt-1">{errors.apiKey}</p>}
            <div className="flex items-center space-x-2 pt-3">
              <Checkbox
                id="api-gateway-checkbox"
                checked={!isApiKeyNeeded}
                onCheckedChange={(checkboxIsChecked) => {
                  const newIsApiKeyNeeded = !checkboxIsChecked;
                  onApiKeyNeededChange(newIsApiKeyNeeded);
                  if (newIsApiKeyNeeded) {
                    onApiKeyChange("");
                  }
                }}
                disabled={isSubmitting || isLoading || isOllamaSelected}
              />
              <Label
                htmlFor="api-gateway-checkbox"
                className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
              >
                I don&apos;t need to provide an API key
              </Label>
            </div>
           </div>
        ) : (
          <div className="border bg-accent border-border p-3 rounded text-sm text-accent-foreground">
            Ollama models run locally and do not require an API key.
          </div>
        )}
      </CardContent>
    </Card>
  );
}; 