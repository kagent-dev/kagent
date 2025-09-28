'use client';

import { useEffect, useMemo, useState } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { useRouter } from 'next/navigation';
import { useAgents } from '@/components/AgentsProvider';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Label } from '@/components/ui/label';
import { Button } from '@/components/ui/button';

export default function ModelsSettingsPage() {
  const { isAuthenticated, user } = useAuth();
  const router = useRouter();
  const { models, loading } = useAgents();
  const [enabledMap, setEnabledMap] = useState<Record<string, boolean>>({});
  const [saving, setSaving] = useState<string | null>(null);
  const [bulkSaving, setBulkSaving] = useState<boolean>(false);

  useEffect(() => {
    if (!isAuthenticated || user?.role !== 'admin') return;
    (async () => {
      try {
        const res = await fetch('/api/admin/models-settings', { headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` } });
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
    return (models || []).map((m) => {
      const ref = m.ref;
      const current = enabledMap.hasOwnProperty(ref) ? enabledMap[ref] : true; // default enabled
      return { ref, name: m.model, enabled: current };
    });
  }, [models, enabledMap]);

  if (!isAuthenticated) return <div className="container mx-auto px-4 py-8"><p>Please login.</p></div>;
  if (user?.role !== 'admin') return <div className="container mx-auto px-4 py-8"><p>Unauthorized.</p></div>;

  const toggle = async (ref: string, next: boolean) => {
    setSaving(ref);
    try {
      const res = await fetch('/api/admin/models-settings', {
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
      await Promise.all(rows.map(row => fetch('/api/admin/models-settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` },
        body: JSON.stringify({ ref: row.ref, enabled: next })
      })));
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
          <h1 className="text-3xl font-bold">Models Settings</h1>
          <p className="text-muted-foreground">Enable or disable models. Disabled models are hidden from selection.</p>
        </div>
        <Button variant="outline" onClick={() => router.push('/admin')}>Back</Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>All Models</CardTitle>
          <CardDescription>Toggle availability for each model</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-end gap-2 mb-3">
            <Button variant="outline" size="sm" onClick={() => setAll(true)} disabled={bulkSaving || loading}>Enable All</Button>
            <Button variant="outline" size="sm" onClick={() => setAll(false)} disabled={bulkSaving || loading}>Disable All</Button>
          </div>
          {loading ? (
            <div>Loading...</div>
          ) : rows.length === 0 ? (
            <div className="text-muted-foreground">No models found.</div>
          ) : (
            <div className="divide-y">
              {rows.map((row) => (
                <div key={row.ref} className="py-3 flex items-center justify-between gap-4">
                  <div className="min-w-0">
                    <div className="font-medium truncate">{row.name}</div>
                    <div className="text-xs text-muted-foreground truncate">{row.ref}</div>
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
