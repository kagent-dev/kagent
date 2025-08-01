import { useState, useEffect } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Terminal, Globe, Loader2, ChevronDown, ChevronUp, PlusCircle, Trash2, Code, InfoIcon, AlertCircle } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import type { SseMcpServerConfig, StdioMcpServerConfig, StreamableHttpMcpServerConfig, ToolServer } from "@/types";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { isResourceNameValid } from "@/lib/utils";
import { NamespaceCombobox } from "@/components/NamespaceCombobox";
import { Checkbox } from "./ui/checkbox";

interface AddServerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onAddServer: (serverConfig: ToolServer) => void;
  onError?: (error: string) => void;
}

interface ArgPair {
  value: string;
}

interface EnvPair {
  key: string;
  value: string;
}

export function AddServerDialog({ open, onOpenChange, onAddServer, onError }: AddServerDialogProps) {
  const [activeTab, setActiveTab] = useState<"command" | "url">("command");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [showAdvancedCommand, setShowAdvancedCommand] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [serverName, setServerName] = useState("");
  const [userEditedName, setUserEditedName] = useState(false);
  const [serverNamespace, setServerNamespace] = useState("");
  const [useStreamableHttp, setUseStreamableHttp] = useState(false);

  // Command structure fields
  const [commandType, setCommandType] = useState("npx");
  const [commandPrefix, setCommandPrefix] = useState("");
  const [packageName, setPackageName] = useState("");

  // Arguments in simplified format (single value)
  const [argPairs, setArgPairs] = useState<ArgPair[]>([{ value: "" }]);

  // Environment variables as key-value pairs
  const [envPairs, setEnvPairs] = useState<EnvPair[]>([{ key: "", value: "" }]);

  // Command preview
  const [commandPreview, setCommandPreview] = useState("");

  // StdioServer parameters
  const [stderr, setStderr] = useState("");
  const [cwd, setCwd] = useState("");

  // SseServer parameters
  const [url, setUrl] = useState("");
  const [headers, setHeaders] = useState("");
  const [timeout, setTimeout] = useState("5s");
  const [sseReadTimeout, setSseReadTimeout] = useState("300s");

  // Clean up package name for server name
  const cleanPackageName = (pkgName: string): string => {
    // Remove organization prefix if exists (e.g., @org/package-name -> package-name)
    let cleaned = pkgName.trim().replace(/^@[\w-]+\//, "");

    // Remove common prefixes like "mcp-" if followed by a descriptive name
    if (cleaned.startsWith("mcp-") && cleaned.length > 4) {
      cleaned = cleaned.substring(4);
    }

    // Convert to lowercase
    cleaned = cleaned.toLowerCase();
    
    // Replace spaces and invalid characters with hyphens
    cleaned = cleaned.replace(/[^a-z0-9.-]/g, "-");
    
    // Replace multiple consecutive hyphens with a single hyphen
    cleaned = cleaned.replace(/-+/g, "-");
    
    // Remove hyphens at the beginning and end
    cleaned = cleaned.replace(/^-+|-+$/g, "");
    
    // If the string starts with a dot, prepend an 'a'
    if (cleaned.startsWith(".")) {
      cleaned = "a" + cleaned;
    }
    
    // If the string ends with a dot, append an 'a'
    if (cleaned.endsWith(".")) {
      cleaned = cleaned + "a";
    }
    
    // Ensure the name starts and ends with an alphanumeric character
    // If it doesn't start with alphanumeric, prepend 'server-'
    if (!/^[a-z0-9]/.test(cleaned)) {
      cleaned = "server-" + cleaned;
    }
    
    // If it doesn't end with alphanumeric, append '-server'
    if (!/[a-z0-9]$/.test(cleaned)) {
      cleaned = cleaned + "-server";
    }
    
    // If the string is empty (could happen after all the replacements), use a default name
    if (!cleaned) {
      cleaned = "tool-server";
    }
    
    return cleaned;
  };

  // Handle server name input changes
  const handleServerNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setServerName(e.target.value);
    setUserEditedName(true);
  };

  // Auto-generate server name when package name or URL changes, but only if user hasn't manually edited the name
  useEffect(() => {
    // Skip auto-generation if user has manually edited the name
    if (userEditedName) {
      return;
    }

    let generatedName = "";

    if (activeTab === "command" && packageName.trim()) {
      generatedName = cleanPackageName(packageName.trim());
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

  // Update command preview whenever inputs change
  useEffect(() => {
    if (activeTab === "command") {
      let preview = commandType;

      // Add command prefix if present
      if (commandPrefix.trim()) {
        preview += " " + commandPrefix.trim();
      }

      // Add package name if present
      if (packageName.trim()) {
        preview += " " + packageName.trim();
      }

      // Add all non-empty arguments
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

  const formatArgs = (): string[] => {
    const args: string[] = [];

    // Add command prefix to the args array if present
    if (commandPrefix.trim()) {
      // Split on whitespace to handle cases where command prefix has multiple parts
      args.push(...commandPrefix.trim().split(/\s+/));
    }

    // Add package name if present
    if (packageName.trim()) {
      args.push(packageName.trim());
    }

    // Add all additional arguments
    argPairs.filter((arg) => arg.value.trim() !== "").forEach((arg) => args.push(arg.value.trim()));

    return args;
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

    let params: StdioMcpServerConfig | SseMcpServerConfig | StreamableHttpMcpServerConfig;
    if (activeTab === "command") {
      // Create StdioServerParameters
      params = {
        command: commandType,
        args: formatArgs(),
      };

      // Add environment variables if any exist
      const formattedEnv = formatEnvVars();
      if (Object.keys(formattedEnv).length > 0) {
        params.env = formattedEnv;
      }
    } else {
      params = {
        url: url.trim(),
      };

      // Add optional parameters if they exist
      if (headers.trim()) {
        try {
          params.headers = JSON.parse(headers);
          // eslint-disable-next-line @typescript-eslint/no-unused-vars
        } catch (e) {
          setError("Headers must be valid JSON");
          setIsSubmitting(false);
          return;
        }
      }

      if (timeout.trim()) {
        // Default to seconds (s) if no unit is provided
        if (!timeout.endsWith("s") && !timeout.endsWith("m") && !timeout.endsWith("h")) {
          params.timeout = timeout + "s";
        } else {
          params.timeout = timeout;
        }
      }

      if (sseReadTimeout.trim()) {
        // Default to seconds (s) if no unit is provided
        if (!sseReadTimeout.endsWith("s") && !sseReadTimeout.endsWith("m") && !sseReadTimeout.endsWith("h")) {
          params.sseReadTimeout = sseReadTimeout + "s";
        } else {
          params.sseReadTimeout = sseReadTimeout;
        }
      }
    }

    const newServer: ToolServer = {
      metadata: {
        name: finalServerName,
        namespace: serverNamespace.trim() || ''
      },

      spec: {
        description: "",
        config: {
          stdio: typeof params === "object" && "command" in params ? params : undefined,
          sse: !useStreamableHttp && typeof params === "object" && "url" in params ? params : undefined,
          streamableHttp: useStreamableHttp && typeof params === "object" && "url" in params ? params : undefined,
        },
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
    setCommandType("npx");
    setCommandPrefix("");
    setPackageName("");
    setArgPairs([{ value: "" }]);
    setEnvPairs([{ key: "", value: "" }]);
    setServerName("");
    setUserEditedName(false);
    setStderr("");
    setCwd("");
    setUrl("");
    setHeaders("");
    setTimeout("5");
    setSseReadTimeout("300");
    setError(null);
    setShowAdvanced(false);
    setShowAdvancedCommand(false);
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

            <Tabs defaultValue="command" value={activeTab} onValueChange={(v) => setActiveTab(v as "command" | "url")}>
              <TabsList className="grid w-full grid-cols-2">
                <TabsTrigger value="command" className="flex items-center gap-2">
                  <Terminal className="h-4 w-4" />
                  Command
                </TabsTrigger>
                <TabsTrigger value="url" className="flex items-center gap-2">
                  <Globe className="h-4 w-4" />
                  URL
                </TabsTrigger>
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
                    <Button variant="ghost" size="sm" onClick={() => setShowAdvancedCommand(!showAdvancedCommand)} className="h-8 px-2 text-xs">
                      {showAdvancedCommand ? "Hide Advanced" : "Show Advanced"}
                    </Button>
                  </div>
                  <Input id="package-name" placeholder="E.g. mcp-package" value={packageName} onChange={(e) => setPackageName(e.target.value)} />
                  <p className="text-xs text-muted-foreground">The name of the package to execute</p>
                </div>

                {showAdvancedCommand && (
                  <div className="space-y-2 border-l-2 pl-4 ml-2 border-blue-100">
                    <Label htmlFor="command-prefix">Command Prefix (Optional)</Label>
                    <Input id="command-prefix" placeholder="E.g., --from git+https://github.com/user/repo" value={commandPrefix} onChange={(e) => setCommandPrefix(e.target.value)} />
                    <p className="text-xs text-muted-foreground">Additional parameters that go between the executor and package name</p>
                  </div>
                )}

                <div className="space-y-2">
                  <div className="flex justify-between items-center">
                    <Label>Arguments</Label>
                  </div>

                  <div className="space-y-2">
                    {argPairs.map((pair, index) => (
                      <div key={index} className="flex gap-2 items-center">
                        <Input placeholder="Argument (e.g., --directory /path or --verbose, ...)" value={pair.value} onChange={(e) => updateArgPair(index, e.target.value)} className="flex-1" />
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

                <Collapsible open={showAdvanced} onOpenChange={setShowAdvanced} className="border rounded-md p-2">
                  <CollapsibleTrigger className="flex w-full items-center justify-between p-2">
                    <span className="font-medium">Advanced Settings</span>
                    {showAdvanced ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                  </CollapsibleTrigger>
                  <CollapsibleContent className="space-y-4 pt-2">
                    <div className="space-y-2">
                      <Label htmlFor="stderr">Stderr Handling</Label>
                      <Input id="stderr" placeholder="e.g., pipe" value={stderr} onChange={(e) => setStderr(e.target.value)} />
                    </div>

                    <div className="space-y-2">
                      <Label htmlFor="cwd">Working Directory</Label>
                      <Input id="cwd" placeholder="e.g., /path/to/working/dir" value={cwd} onChange={(e) => setCwd(e.target.value)} />
                    </div>
                  </CollapsibleContent>
                </Collapsible>
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