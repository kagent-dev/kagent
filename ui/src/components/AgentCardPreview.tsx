import { useEffect, useState } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { getAgentCard } from "@/app/actions/agents";

interface AgentCardPreviewProps {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    namespace: string;
    name: string;
}

export function AgentCardPreview({ open, onOpenChange, namespace, name }: AgentCardPreviewProps) {
    const [data, setData] = useState<string | undefined>(undefined);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        async function load() {
            if (!open) return;
            setLoading(true);
            setError(null);
            setData(undefined);
            try {
                const resp = await getAgentCard(namespace, name);
                if (resp.error) {
                    throw new Error(resp.error);
                }
                // The API returns a JSON string which may itself be JSON-encoded.
                // Try to parse and pretty print; gracefully fall back to the raw string.
                const raw = resp.data as unknown as string;
                let pretty = raw;
                try {
                    let parsed: unknown = JSON.parse(raw);
                    // Handle double-encoded JSON strings
                    if (typeof parsed === "string") {
                        try {
                            parsed = JSON.parse(parsed);
                        } catch {
                            // leave as string
                        }
                    }
                    pretty = typeof parsed === "string" ? (parsed as string) : JSON.stringify(parsed, null, 2);
                } catch {
                    // not JSON, keep as-is
                }
                setData(pretty);
            } catch (e) {
                setError(e instanceof Error ? e.message : "Failed to load agent card");
            } finally {
                setLoading(false);
            }
        }
        void load();
    }, [open, namespace, name]);

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-2xl max-h-[90vh] overflow-auto">
                <DialogHeader>
                    <DialogTitle>Agent Card</DialogTitle>
                </DialogHeader>
                <Card className="min-w-0 overflow-auto max-h-full flex flex-col">
                    <CardHeader>
                        <CardTitle>{namespace}/{name}</CardTitle>
                    </CardHeader>
                    <CardContent className="py-3 overflow-x-auto overflow-y-auto flex-1">
                        {loading && <p className="text-muted-foreground">Loading agent card...</p>}
                        {error && <p className="text-red-500">{error}</p>}
                        {data && <pre className="w-full h-full max-w-full max-h-full overflow-auto whitespace-pre">{data}</pre>}
                    </CardContent>
                </Card>
            </DialogContent>
        </Dialog>
    );
}