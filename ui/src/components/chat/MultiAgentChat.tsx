"use client";

import { useEffect, useMemo, useState } from "react";
import { useAgents } from "@/components/AgentsProvider";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Loader2 } from "lucide-react";

interface MultiAgentChatProps {
  namespace: string;
  name: string;
}

interface ChatResult { agent: string; content: string; model?: string; ts: string }

export default function MultiAgentChat({ namespace, name }: MultiAgentChatProps) {
  const { agents, models, loading, error } = useAgents();
  const [enabledMap, setEnabledMap] = useState<Record<string, boolean>>({});
  const [enabledModelsMap, setEnabledModelsMap] = useState<Record<string, boolean>>({});
  const [selectedRefs, setSelectedRefs] = useState<string[]>([]);
  const [message, setMessage] = useState("");
  const [sending, setSending] = useState(false);
  const [results, setResults] = useState<ChatResult[]>([]);
  const [selectedModelRefs, setSelectedModelRefs] = useState<string[]>([]);
  const [showSelectors, setShowSelectors] = useState<boolean>(false);

  const defaultModel = useMemo(() => {
    if (namespace === "kagent" && name === "multiagent") return "gemini-2.5-flash";
    return undefined;
  }, [namespace, name]);

  useEffect(() => {
    const load = async () => {
      try {
        const res = await fetch('/api/admin/agents-settings', { headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` }});
        if (res.ok) {
          const data = await res.json();
          setEnabledMap(data.enabled || {});
        }
      } catch {}
    };
    load();
  }, []);

  // Load models enabled map
  useEffect(() => {
    const load = async () => {
      try {
        const res = await fetch('/api/admin/models-settings', { headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` }});
        if (res.ok) {
          const data = await res.json();
          setEnabledModelsMap(data.enabled || {});
        }
      } catch {}
    };
    load();
  }, []);

  const available = useMemo(() => {
    const list = (agents || []).filter(a => {
      const ns = a.agent.metadata.namespace || '';
      if (ns !== namespace) return false;
      const ref = `${ns}/${a.agent.metadata.name}`;
      const enabled = enabledMap.hasOwnProperty(ref) ? enabledMap[ref] : true;
      // hide this multiagent orchestrator itself to avoid recursion
      const isSelf = a.agent.metadata.name === name && ns === namespace;
      return enabled && !isSelf;
    }).map(a => ({
      ref: `${a.agent.metadata.namespace || ''}/${a.agent.metadata.name}`,
      name: a.agent.metadata.name,
      desc: a.agent.spec?.description || ''
    }));
    return list;
  }, [agents, enabledMap, namespace, name]);

  // Initialize agent selections on first load
  useEffect(() => {
    if (selectedRefs.length === 0 && available.length > 0) {
      setSelectedRefs(available.map(i => i.ref));
    }
  }, [available, selectedRefs.length]);

  // Namespace-scoped models for multi-select
  const availableModels = useMemo(() => {
    const list = (models || []).filter(m => {
      const refNs = (m.ref || '').split('/')[0];
      const enabled = Object.prototype.hasOwnProperty.call(enabledModelsMap, m.ref) ? enabledModelsMap[m.ref] : true;
      return (!namespace || refNs === namespace) && enabled;
    });
    return list;
  }, [models, namespace, enabledModelsMap]);

  // Initialize model selections (default model for orchestrator if present)
  useEffect(() => {
    if (selectedModelRefs.length === 0) {
      const refs: string[] = [];
      if (defaultModel) {
        const found = availableModels.find(m => (m.model === defaultModel) || (m.ref?.includes(defaultModel)));
        if (found?.ref) refs.push(found.ref);
      }
      // if default not found, select first available
      if (refs.length === 0 && availableModels.length > 0) {
        refs.push(availableModels[0].ref);
      }
      if (refs.length > 0) setSelectedModelRefs(refs);
    }
  }, [availableModels, selectedModelRefs.length, defaultModel]);

  const toggleRef = (ref: string, next: boolean) => {
    setSelectedRefs(prev => next ? Array.from(new Set([...prev, ref])) : prev.filter(r => r !== ref));
  };

  const send = async () => {
    if (!message.trim() || selectedRefs.length === 0) return;
    setSending(true);
    try {
      const res = await fetch('/api/agents/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` },
        body: JSON.stringify({ message, agentRefs: selectedRefs, models: selectedModelRefs.length > 0 ? selectedModelRefs : (defaultModel ? [defaultModel] : undefined) })
      });
      if (!res.ok) throw new Error('Failed to send');
      const data = await res.json();
      setResults((prev) => [{ agent: `${namespace}/${name}`, content: `You: ${message}`, ts: new Date().toISOString() }, ...data.results, ...prev]);
      setMessage("");
    } catch (e) {
      setResults((prev) => [{ agent: `${namespace}/${name}`, content: `Error: ${(e as Error).message}`, ts: new Date().toISOString() }, ...prev]);
    } finally {
      setSending(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-[60vh]">
        <Loader2 className="h-6 w-6 animate-spin" />
      </div>
    );
  }
  if (error) {
    return <div className="p-6 text-red-600">{error}</div>;
  }

  return (
    <div className="w-full h-screen flex flex-col justify-center min-w-full items-center transition-all duration-300 ease-in-out">
      <div className="flex-1 w-full overflow-hidden relative">
        <ScrollArea className="w-full h-full py-12">
          <div className="flex flex-col space-y-5 px-4">
            {/* Title row to mirror single-chat style */}
            <div className="px-2">
              <h1 className="text-2xl font-bold">Multi-Agent Chat</h1>
              <p className="text-muted-foreground">Namespace: {namespace} • Agent: {name}</p>
            </div>

            {/* Messages list */}
            <Card>
              <CardHeader>
                <CardTitle>Responses</CardTitle>
                <CardDescription>Latest on top</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {results.length === 0 ? (
                    <div className="text-muted-foreground text-sm">No messages yet</div>
                  ) : (
                    results.map((r, idx) => (
                      <div key={`${r.ts}-${idx}`} className="border rounded-md p-3">
                        <div className="text-xs text-muted-foreground mb-1">{r.agent}{r.model ? ` • ${r.model}` : ''} • {new Date(r.ts).toLocaleTimeString()}</div>
                        <div className="whitespace-pre-wrap text-sm">{r.content}</div>
                      </div>
                    ))
                  )}
                </div>
              </CardContent>
            </Card>
          </div>
        </ScrollArea>
      </div>

      <div className="w-full sticky bg-secondary bottom-0 md:bottom-2 rounded-none md:rounded-lg p-4 border overflow-hidden transition-all duration-300 ease-in-out">
        {/* Simple selectors toggle to mirror uniform style */}
        <div className="flex items-center justify-between mb-3">
          <div className="text-sm text-muted-foreground">
            Selected agents: {selectedRefs.length} • Selected models: {selectedModelRefs.length}
          </div>
          <Button variant="outline" size="sm" onClick={() => setShowSelectors(!showSelectors)}>
            {showSelectors ? 'Hide options' : 'Select agents & models'}
          </Button>
        </div>

        {showSelectors && (
          <div className="grid md:grid-cols-2 gap-4 mb-3">
            <Card>
              <CardHeader>
                <CardTitle>Agents</CardTitle>
                <CardDescription>Select agents to fan-out your message</CardDescription>
              </CardHeader>
              <CardContent>
                <ScrollArea className="h-48 pr-4">
                  {available.length === 0 ? (
                    <div className="text-muted-foreground text-sm">No agents available</div>
                  ) : (
                    <div className="space-y-3">
                      {available.map(item => (
                        <div key={item.ref} className="flex items-center justify-between gap-2">
                          <div className="min-w-0">
                            <div className="font-medium truncate">{item.name} <span className="text-muted-foreground">({item.ref.split('/')[0]})</span></div>
                            {item.desc && <div className="text-sm text-muted-foreground truncate">{item.desc}</div>}
                          </div>
                          <div className="flex items-center gap-2">
                            <Checkbox id={`cb-${item.ref}`} checked={selectedRefs.includes(item.ref)} onCheckedChange={(v) => toggleRef(item.ref, Boolean(v))} />
                            <Label htmlFor={`cb-${item.ref}`}>{selectedRefs.includes(item.ref) ? 'Selected' : 'Select'}</Label>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </ScrollArea>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Models</CardTitle>
                <CardDescription>Select one or more models</CardDescription>
              </CardHeader>
              <CardContent>
                <ScrollArea className="h-48 pr-4">
                  {availableModels.length === 0 ? (
                    <div className="text-muted-foreground text-sm">No models available</div>
                  ) : (
                    <div className="space-y-3">
                      {availableModels.map((m, idx) => (
                        <div key={`${idx}_${m.ref}`} className="flex items-center justify-between gap-2">
                          <div className="min-w-0">
                            <div className="font-medium truncate">{m.model}</div>
                            <div className="text-xs text-muted-foreground truncate">{m.ref}</div>
                          </div>
                          <div className="flex items-center gap-2">
                            <Checkbox id={`mdl-${idx}`} checked={selectedModelRefs.includes(m.ref)} onCheckedChange={(v) => {
                              const next = Boolean(v);
                              setSelectedModelRefs(prev => next ? Array.from(new Set([...prev, m.ref])) : prev.filter(r => r !== m.ref));
                            }} />
                            <Label htmlFor={`mdl-${idx}`}>{selectedModelRefs.includes(m.ref) ? 'Selected' : 'Select'}</Label>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </ScrollArea>
              </CardContent>
            </Card>
          </div>
        )}

        <div className="flex flex-col gap-3">
          <Textarea value={message} onChange={(e) => setMessage(e.target.value)} placeholder="Type your message..." rows={4} className="min-h-[100px] border-0 shadow-none p-0 focus-visible:ring-0 resize-none" />
          <div className="flex items-center justify-end gap-2">
            <Button onClick={send} disabled={sending || !message.trim() || selectedRefs.length === 0}>
              {sending ? 'Sending...' : 'Send to Agents'}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
