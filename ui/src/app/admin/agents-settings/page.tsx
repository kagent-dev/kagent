'use client';

import { useEffect, useMemo, useState } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { useRouter } from 'next/navigation';
import { useAgents } from '@/components/AgentsProvider';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Label } from '@/components/ui/label';
import { Button } from '@/components/ui/button';

export default function AgentsSettingsPage() {
  const { isAuthenticated, user } = useAuth();
  const router = useRouter();
  const { agents, loading } = useAgents();
  const [enabledMap, setEnabledMap] = useState<Record<string, boolean>>({});
  const [saving, setSaving] = useState<string | null>(null);
  const [bulkSaving, setBulkSaving] = useState<boolean>(false);

  useEffect(() => {
    if (!isAuthenticated || user?.role !== 'admin') return;
    (async () => {
      try {
        const res = await fetch('/api/admin/agents-settings', { headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` } });
        if (res.ok) {
          const data = await res.json();
          setEnabledMap(data.enabled || {});
        }
      } catch {
        // ignore
      }
    })();
  }, [isAuthenticated, user]);

  const rows = useMemo(() => {
    return (agents || []).map(a => {
      const ns = a.agent.metadata.namespace || '';
      const name = a.agent.metadata.name;
      const ref = `${ns}/${name}`;
      const current = enabledMap.hasOwnProperty(ref) ? enabledMap[ref] : true; // default enabled
      return { ref, ns, name, desc: a.agent.spec?.description || '', enabled: current };
    });
  }, [agents, enabledMap]);

  if (!isAuthenticated) return <div className="container mx-auto px-4 py-8"><p>Please login.</p></div>;
  if (user?.role !== 'admin') return <div className="container mx-auto px-4 py-8"><p>Unauthorized.</p></div>;

  const toggle = async (ref: string, next: boolean) => {
    setSaving(ref);
    try {
      const res = await fetch('/api/admin/agents-settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` },
        body: JSON.stringify({ ref, enabled: next }),
      });
      if (res.ok) {
        const data = await res.json();
        setEnabledMap(data.enabled || {});
      }
    } finally {
      setSaving(null);
    }
  };

  const setAll = async (next: boolean) => {
    if (!rows.length) return;
    setBulkSaving(true);
    try {
      await Promise.all(rows.map(row => fetch('/api/admin/agents-settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` },
        body: JSON.stringify({ ref: row.ref, enabled: next })
      })));
      // Update local map
      const updated: Record<string, boolean> = { ...enabledMap };
      rows.forEach(r => { updated[r.ref] = next; });
      setEnabledMap(updated);
    } finally {
      setBulkSaving(false);
    }
  };

  return (
    <div className="container mx-auto px-4 py-8 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Agents Settings</h1>
          <p className="text-muted-foreground">Enable or disable agents. Disabled agents are hidden from the Agents page.</p>
        </div>
        <Button variant="outline" onClick={() => router.push('/admin')}>Back</Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>All Agents</CardTitle>
          <CardDescription>Toggle visibility for each agent</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-end gap-2 mb-3">
            <Button variant="outline" size="sm" onClick={() => setAll(true)} disabled={bulkSaving || loading}>Enable All</Button>
            <Button variant="outline" size="sm" onClick={() => setAll(false)} disabled={bulkSaving || loading}>Disable All</Button>
          </div>
          {loading ? (
            <div>Loading...</div>
          ) : rows.length === 0 ? (
            <div className="text-muted-foreground">No agents found.</div>
          ) : (
            <div className="divide-y">
              {rows.map((row) => (
                <div key={row.ref} className="py-3 flex items-center justify-between gap-4">
                  <div className="min-w-0">
                    <div className="font-medium truncate">{row.name} <span className="text-muted-foreground">({row.ns})</span></div>
                    {row.desc && <div className="text-sm text-muted-foreground truncate">{row.desc}</div>}
                  </div>
                  <div className="flex items-center gap-2">
                    <Checkbox id={`cb-${row.ref}`} checked={row.enabled} onCheckedChange={(v) => toggle(row.ref, Boolean(v))} disabled={saving === row.ref} />
                    <Label htmlFor={`cb-${row.ref}`}>{row.enabled ? 'Enabled' : 'Disabled'}</Label>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
