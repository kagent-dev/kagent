import { useState, useEffect } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Loader2 } from "lucide-react";
import { ToolServer } from "@/types/datamodel";

interface EditServerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  serverToEdit: ToolServer;
  onUpdateServer: (updated: ToolServer) => void;
}

export function EditServerDialog({ open, onOpenChange, serverToEdit, onUpdateServer }: EditServerDialogProps) {
  const [serverName, setServerName] = useState("");
  const [command, setCommand] = useState("");
  const [args, setArgs] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    if (serverToEdit) {
      setServerName(serverToEdit.metadata.name || "");
      const stdio = serverToEdit.spec.config?.stdio;
      if (stdio) {
        setCommand(stdio.command || "");
        setArgs((stdio.args || []).join(" "));
      }
    }
  }, [serverToEdit]);

  const handleSubmit = async () => {
    setIsSubmitting(true);
    const updated: ToolServer = {
      id: serverToEdit.id,
      user_id: serverToEdit.user_id,
      metadata: {
        name: serverName,
      },
      spec: {
        description: serverToEdit.spec.description,
        config: {
          stdio: {
            command: command.trim(),
            args: args.trim().split(/\s+/),
          },
        },
      },
    };

    try {
      await onUpdateServer(updated);
      onOpenChange(false);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Edit Tool Server</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="name">Server Name</Label>
            <Input id="name" value={serverName} disabled />
          </div>

          <div className="space-y-2">
            <Label htmlFor="command">Command</Label>
            <Input id="command" value={command} onChange={(e) => setCommand(e.target.value)} />
          </div>

          <div className="space-y-2">
            <Label htmlFor="args">Arguments</Label>
            <Input id="args" value={args} onChange={(e) => setArgs(e.target.value)} />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={isSubmitting} className="bg-blue-500 hover:bg-blue-600 text-white">
            {isSubmitting ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Saving...
              </>
            ) : (
              "Save Changes"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
