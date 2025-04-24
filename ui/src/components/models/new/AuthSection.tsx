import React from 'react';
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Eye, EyeOff, ExternalLinkIcon } from "lucide-react";
import Link from "next/link";
import { Provider } from "@/lib/types"; 
import { PROVIDERS_INFO, getProviderFormKey, BackendModelProviderType } from "@/lib/providers"; 

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
}

export const AuthSection: React.FC<AuthSectionProps> = ({
  isOllamaSelected, isEditMode, apiKey, showApiKey, errors, isSubmitting,
  isLoading, onApiKeyChange, onToggleShowApiKey, selectedProvider
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
                   disabled={isSubmitting || isLoading}
                   autoComplete="new-password"
                 />
                 <Button
                   type="button"
                   variant="ghost"
                   size="sm"
                   className="absolute right-0 top-0 h-full px-3"
                   onClick={onToggleShowApiKey}
                   disabled={isSubmitting || isLoading}
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