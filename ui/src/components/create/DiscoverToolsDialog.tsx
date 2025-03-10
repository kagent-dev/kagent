import { useState } from "react";
import { DiscoverToolsRequest, SseServerParams, StdioServerParameters } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Loader2, Terminal, Globe, HelpCircle, AlertCircle } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { discoverMCPTools } from "@/app/actions/tools";
import { Component, Tool, ToolConfig } from "@/types/datamodel";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import { Textarea } from "../ui/textarea";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Badge } from "@/components/ui/badge";

interface DiscoverToolsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onShowSelectTools: (tools: Component<ToolConfig>[]) => void;
}

export const DiscoverToolsDialog = ({ open, onOpenChange, onShowSelectTools }: DiscoverToolsDialogProps) => {
  const [activeTab, setActiveTab] = useState<"command" | "url">("command");

  // Command tab state
  const [command, setCommand] = useState("");
  const [args, setArgs] = useState("");
  const [envVars, setEnvVars] = useState("");
  const [stderrType, setStderrType] = useState("inherit");
  const [cwd, setCwd] = useState("");

  // URL tab state
  const [url, setUrl] = useState("");
  const [headers, setHeaders] = useState("");
  const [timeout, setTimeout] = useState("5");
  const [sseReadTimeout, setSseReadTimeout] = useState("300");

  // Shared state
  const [isDiscovering, setIsDiscovering] = useState(false);
  const [error, setError] = useState("");

  const resetState = () => {
    setCommand("");
    setArgs("");
    setEnvVars("");
    setStderrType("inherit");
    setCwd("");
    setUrl("");
    setHeaders("");
    setTimeout("5");
    setSseReadTimeout("300");
    setError("");
    setIsDiscovering(false);
  };

  const handleClose = () => {
    resetState();
    onOpenChange(false);
  };

  const parseEnvVars = (input: string): Record<string, string> => {
    const result: Record<string, string> = {};
    if (!input.trim()) return result;

    try {
      return JSON.parse(input);
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
    } catch (e) {
      // If not valid JSON, try to parse KEY=VALUE format
      const lines = input.split("\n").filter((line) => line.trim());
      for (const line of lines) {
        const [key, ...valueParts] = line.split("=");
        if (key && valueParts.length) {
          result[key.trim()] = valueParts.join("=").trim();
        }
      }
      return result;
    }
  };

  const parseHeaders = (input: string): Record<string, string> => {
    return parseEnvVars(input); // Same parsing logic can be used
  };

  const parseArguments = (input: string): string[] => {
    if (!input.trim()) return [];

    try {
      const parsed = JSON.parse(input);
      return Array.isArray(parsed) ? parsed : [input];
    } catch {
      // If not valid JSON, split by spaces or newlines
      return input.split(/[\s\n]+/).filter((arg) => arg.trim());
    }
  };

  const discoverTools = async () => {
    try {
      setIsDiscovering(true);
      setError("");

      let payload: DiscoverToolsRequest;
      if (activeTab === "command") {
        if (!command.trim()) {
          setError("Command is required");
          return;
        }

        const serverParams: StdioServerParameters = {
          command: command.trim(),
        };

        // Only add non-empty parameters
        const parsedArgs = parseArguments(args);
        if (parsedArgs.length > 0) {
          serverParams.args = parsedArgs;
        }

        const parsedEnv = parseEnvVars(envVars);
        if (Object.keys(parsedEnv).length > 0) {
          serverParams.env = parsedEnv;
        }

        if (stderrType && stderrType !== "inherit") {
          serverParams.stderr = stderrType;
        }

        if (cwd.trim()) {
          serverParams.cwd = cwd.trim();
        }

        payload = {
          type: "stdio",
          server_params: serverParams,
        };
      } else {
        if (!url.trim()) {
          setError("URL is required");
          return;
        }

        const serverParams: SseServerParams = {
          url: url.trim(),
          timeout: parseFloat(timeout) || 5,
          sse_read_timeout: parseFloat(sseReadTimeout) || 300,
        };

        const parsedHeaders = parseHeaders(headers);
        if (Object.keys(parsedHeaders).length > 0) {
          serverParams.headers = parsedHeaders;
        }

        payload = {
          type: "sse",
          server_params: serverParams,
        };
      }

      const response = await discoverMCPTools(payload);

      if (!response.success) {
        throw new Error(response.error || "Failed to discover tools");
      }

      const discoveredToolsData: Tool[] = response.data || [];
      if (discoveredToolsData.length > 0) {
        // Close this dialog and open the select tools dialog
        handleClose();
        onShowSelectTools(discoveredToolsData.map((tool) => tool.component));
      } else {
        setError("No tools were discovered. Please check your input and try again.");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "An unknown error occurred");
    } finally {
      setIsDiscovering(false);
    }
  };

  const FieldLabel = ({ htmlFor, required = false, children, tooltip }: { htmlFor: string; required?: boolean; children: React.ReactNode; tooltip?: string }) => (
    <div className="flex items-center gap-1">
      <Label htmlFor={htmlFor} className="text-sm font-medium">
        {children}
      </Label>
      {required && <span className="text-red-500">*</span>}
      {tooltip && (
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <HelpCircle className="h-3.5 w-3.5 text-muted-foreground cursor-help" />
            </TooltipTrigger>
            <TooltipContent className="max-w-xs">
              <p>{tooltip}</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      )}
    </div>
  );

  return (
    <Dialog
      open={open}
      onOpenChange={(isOpen) => {
        if (!isOpen) handleClose();
      }}
    >
      <DialogContent className="max-w-4xl max-h-[90vh] overflow-auto">
        <DialogHeader className="pb-4 border-b">
          <DialogTitle className="text-xl flex items-center gap-2">
            <Badge variant="outline" className="bg-blue-500/10 text-blue-500 border-blue-500/20">
              MCP
            </Badge>
            Discover Tools
          </DialogTitle>
        </DialogHeader>

        <div className="py-4">
          {/* Quick setup guide */}
          <div className="mb-6">
            <div className="flex items-center justify-between">
              <h3 className="text-lg font-medium mb-2">Discovery Configuration</h3>
            </div>
            <p className="text-muted-foreground text-sm">Configure how to discover available MCP tools using either a command-line interface or a URL endpoint.</p>
          </div>

          <Tabs defaultValue="command" value={activeTab} onValueChange={(value) => setActiveTab(value as "command" | "url")} className="w-full">
            <TabsList className="w-full grid grid-cols-2">
              <TabsTrigger value="command" className="flex items-center gap-2">
                <Terminal className="h-4 w-4" />
                Command Line
              </TabsTrigger>
              <TabsTrigger value="url" className="flex items-center gap-2">
                <Globe className="h-4 w-4" />
                URL Endpoint
              </TabsTrigger>
            </TabsList>

            <TabsContent value="command" className="mt-6 space-y-6">
              <div className="space-y-2">
                <FieldLabel htmlFor="command" required={true} tooltip="The command to execute for MCP tool discovery">
                  Command
                </FieldLabel>
                <Input id="command" placeholder="e.g. npx, uvx, uv" value={command} onChange={(e) => setCommand(e.target.value)} className="font-mono" />
              </div>

              <div className="space-y-2">
                <FieldLabel htmlFor="args" tooltip="Arguments to pass to the command, one per line or as a JSON array">
                  Arguments
                </FieldLabel>
                <Input id="args" placeholder="e.g. mcp-server-kubernetes" value={args} onChange={(e) => setArgs(e.target.value)} className="font-mono" />
              </div>

              <div className="space-y-2">
                <FieldLabel htmlFor="env" tooltip="Environment variables in KEY=VALUE format or as a JSON object">
                  Environment Variables
                </FieldLabel>
                <Textarea
                  id="env"
                  placeholder="API_KEY=your_key_here&#10;DEBUG=true"
                  value={envVars}
                  onChange={(e) => setEnvVars(e.target.value)}
                  className="w-full min-h-[100px] font-mono text-sm"
                />
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <FieldLabel htmlFor="stderr" tooltip="How to handle standard error output from the command">
                    Stderr Handling
                  </FieldLabel>
                  <Select value={stderrType} onValueChange={(value) => setStderrType(value)}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select stderr handling" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="inherit">inherit</SelectItem>
                      <SelectItem value="pipe">pipe</SelectItem>
                      <SelectItem value="ignore">ignore</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                <div className="space-y-2">
                  <FieldLabel htmlFor="cwd" tooltip="Working directory for the command (optional)">
                    Working Directory
                  </FieldLabel>
                  <Input id="cwd" placeholder="e.g. /path/to/working/directory" value={cwd} onChange={(e) => setCwd(e.target.value)} className="font-mono" />
                </div>
              </div>
            </TabsContent>

            <TabsContent value="url" className="mt-6 space-y-6">
              <div className="space-y-2">
                <FieldLabel htmlFor="url" required={true} tooltip="The URL endpoint for MCP tool discovery">
                  URL
                </FieldLabel>
                <Input id="url" placeholder="https://example.com/mcp-tools" value={url} onChange={(e) => setUrl(e.target.value)} className="font-mono" />
              </div>

              <div className="space-y-2">
                <FieldLabel htmlFor="headers" tooltip="HTTP headers in KEY=VALUE format or as a JSON object">
                  Headers
                </FieldLabel>
                <Textarea
                  id="headers"
                  placeholder="Authorization=Bearer token&#10;Content-Type=application/json"
                  value={headers}
                  onChange={(e) => setHeaders(e.target.value)}
                  className="w-full min-h-[100px] font-mono text-sm"
                />
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <FieldLabel htmlFor="timeout" tooltip="Connection timeout in seconds">
                    Timeout (seconds)
                  </FieldLabel>
                  <Input id="timeout" type="number" min="1" step="1" placeholder="5" value={timeout} onChange={(e) => setTimeout(e.target.value)} />
                </div>

                <div className="space-y-2">
                  <FieldLabel htmlFor="sse_read_timeout" tooltip="Server-Sent Events read timeout in seconds">
                    SSE Read Timeout (seconds)
                  </FieldLabel>
                  <Input id="sse_read_timeout" type="number" min="1" step="1" placeholder="300" value={sseReadTimeout} onChange={(e) => setSseReadTimeout(e.target.value)} />
                </div>
              </div>
            </TabsContent>
          </Tabs>

          {error && (
            <Alert variant="destructive" className="mt-6">
              <AlertCircle className="h-4 w-4" />
              <AlertTitle>Error</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
        </div>

        <DialogFooter className="mt-2 pt-4 border-t">
          <div className="flex justify-between w-full items-center">
            <div className="text-xs text-muted-foreground">{activeTab === "command" ? "Discovers tools by executing a local command" : "Discovers tools by querying a remote endpoint"}</div>
            <div className="flex gap-2">
              <Button variant="outline" onClick={handleClose} disabled={isDiscovering}>
                Cancel
              </Button>
              <Button className="bg-violet-500 hover:bg-violet-600 text-white min-w-[140px]" onClick={discoverTools} disabled={isDiscovering}>
                {isDiscovering ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    Discovering...
                  </>
                ) : (
                  "Discover Tools"
                )}
              </Button>
            </div>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
