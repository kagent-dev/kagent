import { useState, useEffect } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2, ChevronDown, ChevronUp, InfoIcon, AlertCircle } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import type { RemoteMCPServer } from "@/types";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { isResourceNameValid } from "@/lib/utils";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { Checkbox } from "./ui/checkbox";

interface AddServerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onAddServer: (serverConfig: RemoteMCPServer) => void;
  onError?: (error: string) => void;
}

export function AddServerDialog({ open, onOpenChange, onAddServer, onError }: AddServerDialogProps) {
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [serverName, setServerName] = useState("");
  const [userEditedName, setUserEditedName] = useState(false);
  const [serverNamespace, setServerNamespace] = useState("");
  const [useStreamableHttp, setUseStreamableHttp] = useState(false);

  // SseServer parameters
  const [url, setUrl] = useState("");
  const [headers, setHeaders] = useState("");
  const [timeout, setTimeout] = useState("5s");
  const [sseReadTimeout, setSseReadTimeout] = useState("300s");

  // Handle server name input changes
  const handleServerNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setServerName(e.target.value);
    setUserEditedName(true);
  };

  // Auto-generate server name when URL changes, but only if user hasn't manually edited the name
  useEffect(() => {
    // Skip auto-generation if user has manually edited the name
    if (userEditedName) {
      return;
    }

    let generatedName = "";

    if (url.trim()) {
      try {
        const urlObj = new URL(url.trim());
        // Convert hostname to RFC 1123 compliant format
        let hostname = urlObj.hostname.toLowerCase();
        
        // Replace invalid characters with hyphens
        hostname = hostname.replace(/[^a-z0-9.-]/g, "-");
        
        // Replace multiple consecutive hyphens with a single hyphen
        hostname = hostname.replace(/-+/g, "-");
        
        // Remove hyphens at the beginning and end
        hostname = hostname.replace(/^-+|-+$/g, "");
        
        // If the hostname starts with a dot, prepend an 'a'
        if (hostname.startsWith(".")) {
          hostname = "a" + hostname;
        }
        
        // If the hostname ends with a dot, append an 'a'
        if (hostname.endsWith(".")) {
          hostname = hostname + "a";
        }
        
        // If it doesn't start with alphanumeric, prepend 'server-'
        if (!/^[a-z0-9]/.test(hostname)) {
          hostname = "server-" + hostname;
        }
        
        // If it doesn't end with alphanumeric, append '-server'
        if (!/[a-z0-9]$/.test(hostname)) {
          hostname = hostname + "-server";
        }
        
        generatedName = hostname;
      } catch {
        // If URL is invalid, use a default name
        generatedName = "remote-server";
      }
    }

    if (!generatedName) {
      generatedName = "tool-server";
    }

    // Directly set the server name without an intermediate variable
    setServerName(generatedName);
  }, [url, userEditedName]);

  const handleSubmit = () => {
    if (!url.trim()) {
      setError("URL is required");
      return;
    }

    // Validate URL has a protocol
    if (!url.trim().match(/^[a-z]+:\/\//i)) {
      setError("Please enter a valid URL with protocol (e.g., http:// or https://)");
      return;
    }
    
    // Get the final server name
    const finalServerName = serverName.trim();
    
    // Check if the name is empty
    if (!finalServerName) {
      setError("Server name is required");
      return;
    }
    
    // Ensure the server name conforms to RFC 1123
    if (!isResourceNameValid(finalServerName)) {
      setError("Server name must conform to RFC 1123 subdomain standard (lowercase alphanumeric characters, '-' or '.', must start and end with alphanumeric character)");
      return;
    }

    setIsSubmitting(true);
    setError(null); // Clear any previous errors

    // Parse headers if provided
    let parsedHeaders: Record<string, string> | undefined;
    if (headers.trim()) {
      try {
        parsedHeaders = JSON.parse(headers);
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
      } catch (e) {
        setError("Headers must be valid JSON");
        setIsSubmitting(false);
        return;
      }
    }

    // Parse timeout values
    let timeoutValue: string | undefined;
    if (timeout.trim()) {
      timeoutValue = timeout.trim();
    }

    let sseReadTimeoutValue: string | undefined;
    if (sseReadTimeout.trim()) {
      sseReadTimeoutValue = sseReadTimeout.trim();
    }

    const newServer: RemoteMCPServer = {
      metadata: {
        name: finalServerName,
        namespace: serverNamespace.trim() || ''
      },

      spec: {
        description: "",
        protocol: useStreamableHttp ? "STREAMABLE_HTTP" : "SSE",
        url: url.trim(),
        headersFrom: parsedHeaders ? Object.entries(parsedHeaders).map(([key, value]) => ({
          name: key,
          value: value
        })) : [],
        timeout: timeoutValue,
        sseReadTimeout: sseReadTimeoutValue,
      },
    };

    try {
      onAddServer(newServer);
      resetForm();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Unknown error occurred";
      setError(errorMessage);
      if (onError) {
        onError(errorMessage);
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  const resetForm = () => {
    setServerName("");
    setUserEditedName(false);
    setUrl("");
    setHeaders("");
    setTimeout("5s");
    setSseReadTimeout("300s");
    setError(null);
    setShowAdvanced(false);
  };

  const handleClose = () => {
    resetForm();
    onOpenChange(false);
  };

  // Format error message to be more user-friendly
  const formatErrorMessage = (errorMsg: string): string => {
    // Handle common backend errors
    if (errorMsg.includes("already exists")) {
      return "A server with this name already exists. Please choose a different name.";
    }
    
    if (errorMsg.includes("Failed to create server")) {
      return "Failed to create server. Please check your configuration and try again.";
    }
    
    if (errorMsg.includes("Network error")) {
      return "Network error: Could not connect to the server. Please check your connection and try again.";
    }
    
    if (errorMsg.includes("Request timed out")) {
      return "Request timed out: The server took too long to respond. Please try again later.";
    }
    
    // Return the original error if no specific formatting is needed
    return errorMsg;
  };

  const handleUseStreamableHttpChange = (checked: boolean) => {
    setUseStreamableHttp(checked);
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-2xl flex flex-col max-h-[90vh]">
        <DialogHeader className="px-6 pt-6 pb-2 border-b flex-shrink-0">
          <DialogTitle>Add Tool Server</DialogTitle>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto px-6">
          {error && (
            <div className="mb-4 p-3 bg-red-50 border border-red-200 text-red-700 rounded-md text-sm flex items-start">
              <AlertCircle className="h-5 w-5 mr-2 mt-0.5 flex-shrink-0" />
              <div className="flex-1">
                <p className="font-medium">Error</p>
                <p>{formatErrorMessage(error)}</p>
              </div>
            </div>
          )}

          <div className="space-y-4">
            <div className="space-y-2">
              <div className="flex items-center space-x-2">
                <Label htmlFor="server-name">Server Name</Label>
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="inline-flex">
                        <InfoIcon className="h-4 w-4 text-gray-400" />
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p className="max-w-xs text-xs">Must be lowercase alphanumeric characters, &apos;-&apos; or &apos;.&apos;, and must start and end with an alphanumeric character</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </div>
              <Input 
                id="server-name" 
                placeholder="e.g., my-tool-server" 
                value={serverName} 
                onChange={handleServerNameChange}
                className={!isResourceNameValid(serverName) && serverName ? "border-red-300" : ""}
              />
              {!isResourceNameValid(serverName) && serverName && (
                <p className="text-xs text-red-500">Name must conform to RFC 1123 subdomain format</p>
              )}
            </div>

            <div className="space-y-2">
              <div className="flex items-center space-x-2">
                <Label htmlFor="server-namespace">Server Namespace</Label>
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="inline-flex">
                        <InfoIcon className="h-4 w-4 text-gray-400" />
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p className="max-w-xs text-xs">Must be lowercase alphanumeric characters, &apos;-&apos; or &apos;.&apos;, and must start and end with an alphanumeric character</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </div>
              <NamespaceCombobox
                value={serverNamespace}
                onValueChange={setServerNamespace}
              />
            </div>

            <div className="pt-4 space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="url">Server URL</Label>
                  <Input id="url" placeholder="e.g., https://example.com/mcp-endpoint" value={url} onChange={(e) => setUrl(e.target.value)} />
                  <p className="text-xs text-muted-foreground">Enter the URL of the MCP server endpoint</p>
                </div>

                <div className="space-y-2">
                  <div className="flex items-center space-x-2">
                    <Checkbox id="use-streamable-http" checked={useStreamableHttp} onCheckedChange={handleUseStreamableHttpChange} />
                    <Label htmlFor="use-streamable-http">Use Streamable HTTP</Label>
                  </div>
                  <p className="text-xs text-muted-foreground">Use Streamable HTTP to connect to the MCP server, instead of SSE</p>
                </div>

                <Collapsible open={showAdvanced} onOpenChange={setShowAdvanced} className="border rounded-md p-2">
                  <CollapsibleTrigger className="flex w-full items-center justify-between p-2">
                    <span className="font-medium">Advanced Settings</span>
                    {showAdvanced ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                  </CollapsibleTrigger>
                  <CollapsibleContent className="space-y-4 pt-2">
                    <div className="space-y-2">
                      <Label htmlFor="headers">Headers (JSON)</Label>
                      <Input id="headers" placeholder='e.g., {"Authorization": "Bearer token"}' value={headers} onChange={(e) => setHeaders(e.target.value)} />
                    </div>

                    <div className="space-y-2">
                      <Label htmlFor="timeout">Connection Timeout (seconds)</Label>
                      <Input id="timeout" type="string" value={timeout} onChange={(e) => setTimeout(e.target.value)} />
                    </div>

                    <div className="space-y-2">
                      <Label htmlFor="sse-read-timeout">SSE Read Timeout (seconds)</Label>
                      <Input id="sse-read-timeout" type="string" value={sseReadTimeout} onChange={(e) => setSseReadTimeout(e.target.value)} />
                    </div>
                  </CollapsibleContent>
                </Collapsible>
              </div>
          </div>
        </div>

        <DialogFooter className="px-6 py-4 border-t flex-shrink-0">
          <Button variant="outline" onClick={handleClose} disabled={isSubmitting}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={isSubmitting} className="bg-blue-500 hover:bg-blue-600 text-white">
            {isSubmitting ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Adding...
              </>
            ) : (
              "Add Server"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}