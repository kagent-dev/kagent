"use client";

import { useState, useEffect } from "react";
import { Share2, Loader2, Copy, Check, Globe, Lock, Eye, EyeOff, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { toast } from "sonner";
import {
  createSessionShare,
  deleteSessionShare,
  listSessionShares,
  type SessionShare,
} from "@/app/actions/sessionShares";

interface ShareButtonProps {
  sessionId: string;
  namespace: string;
  agentName: string;
}

export default function ShareButton({ sessionId, namespace, agentName }: ShareButtonProps) {
  const [open, setOpen] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [shares, setShares] = useState<SessionShare[]>([]);
  const [copiedToken, setCopiedToken] = useState<string | null>(null);
  const [readOnly, setReadOnly] = useState(true);
  const [origin, setOrigin] = useState("");

  useEffect(() => {
    setOrigin(window.location.origin);
  }, []);

  const shareUrl = (token: string) =>
    origin ? `${origin}/agents/${namespace}/${agentName}/chat/${sessionId}?share=${token}` : null;

  const loadShares = async () => {
    const result = await listSessionShares(sessionId);
    setShares(result.data ?? []);
  };

  useEffect(() => {
    let cancelled = false;
    listSessionShares(sessionId).then((result) => {
      if (!cancelled) setShares(result.data ?? []);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [sessionId]);

  const handleOpenChange = async (next: boolean) => {
    setOpen(next);
    if (!next) return;
    setIsLoading(true);
    try {
      await loadShares();
    } catch {
      toast.error("Failed to load share status");
    } finally {
      setIsLoading(false);
    }
  };

  const handleCreate = async () => {
    setIsLoading(true);
    try {
      const result = await createSessionShare(sessionId, readOnly);
      if (result.error || !result.data) {
        toast.error(result.error || "Failed to create share link");
        return;
      }
      setShares((prev) => [...prev, result.data!]);
    } catch {
      toast.error("Something went wrong");
    } finally {
      setIsLoading(false);
    }
  };

  const handleDelete = async (token: string) => {
    setIsLoading(true);
    try {
      const result = await deleteSessionShare(sessionId, token);
      if (result.error) {
        toast.error(result.error);
        return;
      }
      setShares((prev) => prev.filter((s) => s.token !== token));
    } catch {
      toast.error("Something went wrong");
    } finally {
      setIsLoading(false);
    }
  };

  const handleCopy = async (token: string) => {
    const url = shareUrl(token);
    if (!url) return;
    try {
      await navigator.clipboard.writeText(url);
      setCopiedToken(token);
      toast.success("Link copied to clipboard");
      setTimeout(() => setCopiedToken(null), 2000);
    } catch {
      toast.error("Failed to copy link — please copy it manually");
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          title={shares.length > 0 ? "Shared — click to manage" : "Share this chat"}
          className={shares.length > 0 ? "text-primary" : undefined}
        >
          <Share2 className="h-4 w-4" />
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Share chat</DialogTitle>
        </DialogHeader>

        {isLoading && shares.length === 0 ? (
          <div className="flex items-center justify-center py-6">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <div className="space-y-4">
            {shares.length > 0 && (
              <div className="space-y-2">
                <p className="text-xs text-muted-foreground flex items-center gap-1">
                  <Globe className="h-3 w-3 text-primary" />
                  Anyone with a link can access this chat.
                </p>
                <div className="divide-y rounded-md border">
                  {shares.map((share) => {
                    const url = shareUrl(share.token);
                    return (
                      <div key={share.token} className="flex items-center gap-2 px-3 py-2">
                        <span className="flex-1 truncate text-xs text-muted-foreground font-mono">
                          {url ?? share.token}
                        </span>
                        <span className="shrink-0 inline-flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-xs font-medium text-muted-foreground">
                          {share.read_only
                            ? <><EyeOff className="h-3 w-3" /> View</>
                            : <><Eye className="h-3 w-3" /> Interact</>}
                        </span>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-6 w-6 shrink-0"
                          title="Copy link"
                          onClick={() => handleCopy(share.token)}
                          disabled={!url}
                        >
                          {copiedToken === share.token
                            ? <Check className="h-3 w-3 text-green-500" />
                            : <Copy className="h-3 w-3" />}
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-6 w-6 shrink-0 text-muted-foreground hover:text-destructive"
                          title="Revoke link"
                          onClick={() => handleDelete(share.token)}
                          disabled={isLoading}
                        >
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      </div>
                    );
                  })}
                </div>
              </div>
            )}

            <div className="space-y-3">
              {shares.length > 0 && (
                <p className="text-xs font-medium text-muted-foreground">Create another link</p>
              )}
              <div className="flex items-center justify-between">
                <div>
                  <Label htmlFor="read-only-switch" className="text-sm font-medium cursor-pointer">
                    Read-only
                  </Label>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    {readOnly ? "Visitors can view but not interact" : "Visitors can view and interact"}
                  </p>
                </div>
                <Switch
                  id="read-only-switch"
                  checked={readOnly}
                  onCheckedChange={setReadOnly}
                  disabled={isLoading}
                />
              </div>
              <Button className="w-full" onClick={handleCreate} disabled={isLoading}>
                {isLoading ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : <Lock className="h-4 w-4 mr-2" />}
                Create share link
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
