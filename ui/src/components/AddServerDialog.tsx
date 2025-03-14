import { useState } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Terminal, Globe, Loader2 } from "lucide-react";

// Define a simplified type for the server
interface ServerData {
  user_id?: string;
  server_id: string;
  component: {
    provider: string;
    component_type: string;
    label?: string;
    description?: string;
    config: {
      type: "command" | "url";
      details: string;
    }
  }
}

interface AddServerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onAddServer: (server: ServerData) => void;
}

export function AddServerDialog({ open, onOpenChange, onAddServer }: AddServerDialogProps) {
  const [activeTab, setActiveTab] = useState<"command" | "url">("command");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [serverName, setServerName] = useState("");
  const [serverDetails, setServerDetails] = useState("");
  const [serverId, setServerId] = useState("");
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = () => {
    // Basic validation
    if (!serverName.trim()) {
      setError("Server name is required");
      return;
    }

    if (!serverDetails.trim()) {
      setError(`${activeTab === "command" ? "Command" : "URL"} is required`);
      return;
    }

    // Generate a server ID if not provided
    const serverIdToUse = serverId.trim() || serverName.toLowerCase().replace(/\s+/g, "-");

    setIsSubmitting(true);
    
    // Create the server object
    const newServer: ServerData = {
      server_id: serverIdToUse,
      component: {
        provider: `mcp.server.${serverIdToUse}`,
        component_type: "mcp_server",
        label: serverName,
        description: `MCP Server using ${activeTab}`,
        config: {
          type: activeTab,
          details: serverDetails
        }
      }
    };

    // Submit the server
    onAddServer(newServer);
    
    // Reset the form
    setServerName("");
    setServerDetails("");
    setServerId("");
    setError(null);
    setIsSubmitting(false);
  };

  const handleClose = () => {
    // Reset form on close
    setServerName("");
    setServerDetails("");
    setServerId("");
    setError(null);
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add MCP Server</DialogTitle>
        </DialogHeader>
        
        <div className="py-4">
          {error && (
            <div className="mb-4 p-2 bg-red-50 border border-red-200 text-red-700 rounded text-sm">
              {error}
            </div>
          )}
          
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="server-name">Server Name</Label>
              <Input
                id="server-name"
                placeholder="e.g., Kubernetes MCP Server"
                value={serverName}
                onChange={(e) => setServerName(e.target.value)}
              />
            </div>
            
            <div className="space-y-2">
              <Label htmlFor="server-id">Server ID (optional)</Label>
              <Input
                id="server-id"
                placeholder="e.g., k8s-mcp"
                value={serverId}
                onChange={(e) => setServerId(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Leave empty to generate from name
              </p>
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
              
              <TabsContent value="command" className="pt-4">
                <div className="space-y-2">
                  <Label htmlFor="command">Command to execute</Label>
                  <Input
                    id="command"
                    placeholder="e.g., npx mcp-server-kubernetes"
                    value={serverDetails}
                    onChange={(e) => setServerDetails(e.target.value)}
                  />
                  <p className="text-xs text-muted-foreground">
                    Enter the command that will start the MCP server
                  </p>
                </div>
              </TabsContent>
              
              <TabsContent value="url" className="pt-4">
                <div className="space-y-2">
                  <Label htmlFor="url">Server URL</Label>
                  <Input
                    id="url"
                    placeholder="e.g., https://example.com/mcp-endpoint"
                    value={serverDetails}
                    onChange={(e) => setServerDetails(e.target.value)}
                  />
                  <p className="text-xs text-muted-foreground">
                    Enter the URL of the MCP server endpoint
                  </p>
                </div>
              </TabsContent>
            </Tabs>
          </div>
        </div>
        
        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={isSubmitting}>
            Cancel
          </Button>
          <Button 
            onClick={handleSubmit} 
            disabled={isSubmitting} 
            className="bg-blue-500 hover:bg-blue-600 text-white"
          >
            {isSubmitting ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Adding...
              </>
            ) : (
              'Add Server'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}