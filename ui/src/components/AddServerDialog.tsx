import { useState, useEffect } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Terminal, Globe, Loader2, PlusCircle, Trash2, Code, InfoIcon, AlertCircle } from "lucide-react";
import type { RemoteMCPServer, MCPServer, ToolServerCreateRequest } from "@/types";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { createRFC1123ValidName, isResourceNameValid } from "@/lib/utils";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { Checkbox } from "./ui/checkbox";

interface AddServerDialogProps {
  open: boolean;
  supportedToolServerTypes: string[];
  onOpenChange: (open: boolean) => void;
  onAddServer: (serverRequest: ToolServerCreateRequest) => void;
  onError?: (error: string) => void;
}

interface ArgPair {
  value: string;
}

interface EnvPair {
  key: string;
  value: string;
}

export function AddServerDialog({ open, supportedToolServerTypes, onOpenChange, onAddServer, onError }: AddServerDialogProps) {
  const [activeTab, setActiveTab] = useState<"command" | "url">("url");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [serverName, setServerName] = useState("");
  const [userEditedName, setUserEditedName] = useState(false);
  const [serverNamespace, setServerNamespace] = useState("");
  const [useStreamableHttp, setUseStreamableHttp] = useState(false);

  const [commandType, setCommandType] = useState("npx");
  const [commandPrefix, setCommandPrefix] = useState("");
  const [packageName, setPackageName] = useState("");
  const [argPairs, setArgPairs] = useState<ArgPair[]>([{ value: "" }]);
  const [envPairs, setEnvPairs] = useState<EnvPair[]>([{ key: "", value: "" }]);
  const [commandPreview, setCommandPreview] = useState("");
  

  // SseServer parameters
  const [url, setUrl] = useState("");
  const [headers, setHeaders] = useState("");
  const [timeout, setTimeout] = useState("5s");
  const [sseReadTimeout, setSseReadTimeout] = useState("300s");
  const [terminateOnClose, setTerminateOnClose] = useState(true);

  // Handle server name input changes
  const handleServerNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setServerName(e.target.value);
    setUserEditedName(true);
  };

  const isToolServerTypeSupported = (type: string): boolean => {
    return supportedToolServerTypes.includes(type);
  };

  // Auto-generate server name when package name or URL changes, but only if user hasn't manually edited the name
  useEffect(() => {
    // Skip auto-generation if user has manually edited the name
    if (userEditedName) {
      return;
    }

    let generatedName = "";

    if (activeTab === "command" && packageName.trim()) {
      generatedName = createRFC1123ValidName([packageName.trim()]);
    } else if (activeTab === "url" && url.trim()) {
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
  }, [activeTab, packageName, url, userEditedName]);

  useEffect(() => {
    if (activeTab === "command") {
      let preview = commandType;

      if (commandPrefix.trim()) {
        preview += " " + commandPrefix.trim();
      }

      if (packageName.trim()) {
        preview += " " + packageName.trim();
      }

      argPairs.forEach((arg) => {
        if (arg.value.trim()) {
          preview += " " + arg.value.trim();
        }
      });

      setCommandPreview(preview);
    }
  }, [activeTab, commandType, commandPrefix, packageName, argPairs]);

  const addArgPair = () => {
    setArgPairs([...argPairs, { value: "" }]);
  };

  const removeArgPair = (index: number) => {
    setArgPairs(argPairs.filter((_, i) => i !== index));
  };

  const updateArgPair = (index: number, newValue: string) => {
    const updatedPairs = [...argPairs];
    updatedPairs[index].value = newValue;
    setArgPairs(updatedPairs);
  };

  const addEnvPair = () => {
    setEnvPairs([...envPairs, { key: "", value: "" }]);
  };

  const removeEnvPair = (index: number) => {
    setEnvPairs(envPairs.filter((_, i) => i !== index));
  };

  const updateEnvPair = (index: number, field: "key" | "value", newValue: string) => {
    const updatedPairs = [...envPairs];
    updatedPairs[index][field] = newValue;
    setEnvPairs(updatedPairs);
  };

  const formatEnvVars = (): Record<string, string> => {
    const envVars: Record<string, string> = {};
    envPairs.forEach((pair) => {
      if (pair.key.trim() && pair.value.trim()) {
        envVars[pair.key.trim()] = pair.value.trim();
      }
    });
    return envVars;
  };

  const handleSubmit = () => {
    if (activeTab === "command" && !packageName.trim()) {
      setError("Package name is required");
      return;
    }

    if (activeTab === "url" && !url.trim()) {
      setError("URL is required");
      return;
    }

    // Validate URL has a protocol
    if (activeTab === "url" && !url.trim().match(/^[a-z]+:\/\//i)) {
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

    try {
      let serverRequest: ToolServerCreateRequest;

      if (activeTab === "command") {
        // Create MCPServer for stdio-based server
        let image: string;
        let cmd: string;
        let args: string[];

        if (commandType === "uvx") {
          // Use uvx with the official uv image
          image = "ghcr.io/astral-sh/uv:debian";
          cmd = "uvx";
          
          // Build args array: [args..., packageName]
          args = [];
          if (commandPrefix.trim()) {
            // Split command prefix and add to args
            args.push(...commandPrefix.trim().split(/\s+/));
          }
          // Add additional arguments first
          argPairs.filter((arg) => arg.value.trim() !== "").forEach((arg) => {
            args.push(arg.value.trim());
          });
          // Add package name at the end
          args.push(packageName.trim());
        } else {
          // Use npx with Node.js image
          image = "node:24-alpine3.21";
          cmd = "npx";
          
          // Build args array: [args..., packageName]
          args = [];
          if (commandPrefix.trim()) {
            // Split command prefix and add to args
            args.push(...commandPrefix.trim().split(/\s+/));
          }
          // Add additional arguments first
          argPairs.filter((arg) => arg.value.trim() !== "").forEach((arg) => {
            args.push(arg.value.trim());
          });
          // Add package name at the end
          args.push(packageName.trim());
        }

        const mcpServer: MCPServer = {
          metadata: {
            name: finalServerName,
            namespace: serverNamespace.trim() || 'default'
          },
          spec: {
            deployment: {
              image: image,
              port: 3000, // Default port
              cmd: cmd,
              args: args,
              ...(Object.keys(formatEnvVars()).length > 0 && { env: formatEnvVars() }),
            },
            transportType: "stdio",
            stdioTransport: {}
          }
        };

        serverRequest = {
          type: "MCPServer",
          mcpServer
        };
      } else {
        // Create RemoteMCPServer for URL-based server
        // Parse headers if provided
        let parsedHeaders: Record<string, string> | undefined;
        if (headers.trim()) {
          try {
            parsedHeaders = JSON.parse(headers);
          } catch (e) {
            setError("Headers must be valid JSON");
            console.error(`❌ Error parsing headers: ${e}`);
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

        const remoteMCPServer: RemoteMCPServer = {
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
              value: value as string,
            })) : [],
            timeout: timeoutValue,
            sseReadTimeout: sseReadTimeoutValue,
            terminateOnClose: terminateOnClose,
          },
        };

        serverRequest = {
          type: "RemoteMCPServer",
          remoteMCPServer
        };
      }

      onAddServer(serverRequest);
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
    setCommandType("npx");
    setCommandPrefix("");
    setPackageName("");
    setArgPairs([{ value: "" }]);
    setEnvPairs([{ key: "", value: "" }]);
    setServerName("");
    setUserEditedName(false);
    
    setUrl("");
    setHeaders("");
    setTimeout("5s");
    setSseReadTimeout("300s");
    setTerminateOnClose(true);
    setError(null);
    
    setCommandPreview("");
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

  const renderTabTrigger = (
    value: string,
    icon: React.ReactNode,
    label: string,
    disabled: boolean,
    tooltip?: string
  ) => {
    const trigger = (
      <TabsTrigger
        value={value}
        className="flex items-center gap-2 justify-center w-full"
        disabled={disabled}
      >
        {icon}
        {label}
      </TabsTrigger>
    );
    return disabled && tooltip ? (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="block w-full">{trigger}</span>
          </TooltipTrigger>
          <TooltipContent>
            <span>{tooltip}</span>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    ) : (
      trigger
    );
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-2xl flex flex-col max-h-[90vh]">
        <DialogHeader className="px-6 pt-6 pb-2 border-b flex-shrink-0">
          <DialogTitle>Add MCP Server</DialogTitle>
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

            <Tabs defaultValue="url" value={activeTab} onValueChange={(v) => setActiveTab(v as "command" | "url")}>
              <TabsList className="grid w-full grid-cols-2">
                <TabsTrigger value="url" className="flex items-center gap-2">
                  <Globe className="h-4 w-4" />
                  URL
                </TabsTrigger>
                {renderTabTrigger(
                  "command",
                  <Terminal className="h-4 w-4" />,
                  "Command",
                  !isToolServerTypeSupported("MCPServer"),
                  "KMCP integration disabled: MCPServer CRD not found in cluster."
                )}
              </TabsList>

              <TabsContent value="command" className="pt-4 space-y-4">
                {/* Command Preview Box */}
                <div className="p-3 bg-gray-50 border rounded-md font-mono text-sm text-gray-500">
                  <div className="flex items-center gap-2 mb-1">
                    <Code className="h-4 w-4" />
                    <span>Command Preview:</span>
                  </div>
                  <div className="overflow-x-auto">
                    <div className="whitespace-pre-wrap break-all">{commandPreview || "<command will appear here>"}</div>
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-4">
                  <div className="space-y-2">
                    <Label>Command Executor</Label>
                    <Select value={commandType} onValueChange={setCommandType}>
                      <SelectTrigger>
                        <SelectValue placeholder="Select command" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="npx">npx</SelectItem>
                        <SelectItem value="uvx">uvx</SelectItem>
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-muted-foreground">Select the command executor (e.g., npx or uvx)</p>
                  </div>
                </div>

                <div className="space-y-2">
                  <div className="flex items-center justify-between mb-1">
                    <Label htmlFor="package-name">Package Name</Label>
                  </div>
                  <Input id="package-name" placeholder="E.g. mcp-package" value={packageName} onChange={(e) => setPackageName(e.target.value)} />
                  <p className="text-xs text-muted-foreground">The name of the package to execute</p>
                </div>

                

                <div className="space-y-2">
                  <div className="flex justify-between items-center">
                    <Label>Arguments</Label>
                  </div>

                  <div className="space-y-2">
                    {argPairs.map((pair, index) => (
                      <div key={index} className="flex gap-2 items-center">
                        <Input placeholder="Argument (e.g., --verbose, --help, ...)" value={pair.value} onChange={(e) => updateArgPair(index, e.target.value)} className="flex-1" />
                        <Button variant="ghost" size="sm" onClick={() => removeArgPair(index)} disabled={argPairs.length === 1} className="p-1">
                          <Trash2 className="h-4 w-4 text-red-500" />
                        </Button>
                      </div>
                    ))}
                    <Button variant="outline" size="sm" onClick={addArgPair} className="mt-2 w-full">
                      <PlusCircle className="h-4 w-4 mr-2" />
                      Add Argument
                    </Button>
                  </div>
                </div>

                <div className="space-y-2">
                  <div className="flex justify-between items-center">
                    <Label>Environment Variables</Label>
                  </div>

                  <div className="space-y-2">
                    {envPairs.map((pair, index) => (
                      <div key={index} className="flex gap-2 items-center">
                        <Input placeholder="Key (e.g., NODE_ENV)" value={pair.key} onChange={(e) => updateEnvPair(index, "key", e.target.value)} className="flex-1" />
                        <Input placeholder="Value (e.g., production)" value={pair.value} onChange={(e) => updateEnvPair(index, "value", e.target.value)} className="flex-1" />
                        <Button variant="ghost" size="sm" onClick={() => removeEnvPair(index)} disabled={envPairs.length === 1} className="p-1">
                          <Trash2 className="h-4 w-4 text-red-500" />
                        </Button>
                      </div>
                    ))}
                    <Button variant="outline" size="sm" onClick={addEnvPair} className="mt-2 w-full">
                      <PlusCircle className="h-4 w-4 mr-2" />
                      Add Environment Variable
                    </Button>
                  </div>
                </div>

                
              </TabsContent>

              <TabsContent value="url" className="pt-4 space-y-4">
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

                <div className="space-y-2">
                  <Label htmlFor="headers">Headers (JSON)</Label>
                  <Input id="headers" placeholder='e.g., {"Authorization": "Bearer token"}' value={headers} onChange={(e) => setHeaders(e.target.value)} />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="timeout">Connection Timeout (e.g., 30s)</Label>
                  <Input id="timeout" type="string" value={timeout} onChange={(e) => setTimeout(e.target.value)} />
                </div>

                {!useStreamableHttp && (
                  <div className="space-y-2">
                    <Label htmlFor="sse-read-timeout">SSE Read Timeout (e.g., 60s)</Label>
                    <Input id="sse-read-timeout" type="string" value={sseReadTimeout} onChange={(e) => setSseReadTimeout(e.target.value)} />
                  </div>
                )}

                <div className="space-y-2">
                  <div className="flex items-center space-x-2">
                    <Checkbox id="terminate-on-close" checked={terminateOnClose} onCheckedChange={(checked) => setTerminateOnClose(Boolean(checked))} />
                    <Label htmlFor="terminate-on-close">Terminate Connection On Close</Label>
                  </div>
                  <p className="text-xs text-muted-foreground">When enabled, the server will terminate connection when the client disconnects</p>
                </div>
              </TabsContent>
            </Tabs>
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