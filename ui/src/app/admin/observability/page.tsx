'use client';

import { useEffect, useState } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { useRouter } from 'next/navigation';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { toast } from 'sonner';

interface ObservabilitySettings {
  enabled: boolean;
  provider: 'otel-collector' | 'datadog' | 'honeycomb';
  endpoint: string;
  apiKey: string;
  sampling: number;
  retentionDays: number;
}

export default function ObservabilityAdminPage() {
  const { isAuthenticated, user } = useAuth();
  const router = useRouter();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [settings, setSettings] = useState<ObservabilitySettings>({
    enabled: false,
    provider: 'otel-collector',
    endpoint: '',
    apiKey: '',
    sampling: 0.1,
    retentionDays: 7,
  });

  useEffect(() => {
    if (!isAuthenticated) return;
    if (user?.role !== 'admin') return;
    (async () => {
      try {
        const res = await fetch('/api/admin/observability', {
          headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` }
        });
        if (!res.ok) throw new Error('Failed to load settings');
        const data = await res.json();
        setSettings(data);
      } catch (e) {
        toast.error('Failed to load settings');
      } finally {
        setLoading(false);
      }
    })();
  }, [isAuthenticated, user]);

  if (!isAuthenticated) {
    return (
      <div className="container mx-auto px-4 py-8">
        <p>Please login.</p>
      </div>
    );
  }
  if (user?.role !== 'admin') {
    return (
      <div className="container mx-auto px-4 py-8">
        <p>Unauthorized.</p>
      </div>
    );
  }

  const onSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    try {
      const res = await fetch('/api/admin/observability', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` },
        body: JSON.stringify(settings),
      });
      if (!res.ok) throw new Error('Failed to save');
      const data = await res.json();
      setSettings(data);
      toast.success('Settings saved');
    } catch (e) {
      toast.error('Failed to save settings');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="container mx-auto px-4 py-8">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-3xl font-bold">Observability</h1>
        <div className="text-sm text-muted-foreground">
          {settings.enabled ? 'Enabled' : 'Disabled'}
        </div>
      </div>

      <Card className="max-w-3xl">
        <CardHeader>
          <CardTitle>Configuration</CardTitle>
          <CardDescription>Set up tracing and analytics for your agents.</CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div>Loading...</div>
          ) : (
            <form onSubmit={onSave} className="space-y-6">
              <div className="flex items-center space-x-3">
                <Checkbox id="enabled" checked={settings.enabled} onCheckedChange={(v) => setSettings({ ...settings, enabled: Boolean(v) })} />
                <Label htmlFor="enabled">Enable Observability</Label>
              </div>

              <div className="grid md:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="provider">Provider</Label>
                  <select
                    id="provider"
                    className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                    value={settings.provider}
                    onChange={(e) => setSettings({ ...settings, provider: e.target.value as ObservabilitySettings['provider'] })}
                  >
                    <option value="otel-collector">OpenTelemetry Collector</option>
                    <option value="datadog">Datadog</option>
                    <option value="honeycomb">Honeycomb</option>
                  </select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="endpoint">Endpoint</Label>
                  <Input id="endpoint" value={settings.endpoint} onChange={(e) => setSettings({ ...settings, endpoint: e.target.value })} placeholder="https://otel-collector:4318" />
                </div>
              </div>

              <div className="grid md:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="apiKey">API Key (optional)</Label>
                  <Input id="apiKey" value={settings.apiKey} onChange={(e) => setSettings({ ...settings, apiKey: e.target.value })} placeholder="••••••" />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="sampling">Sampling (0-1)</Label>
                  <Input id="sampling" type="number" step="0.01" min={0} max={1} value={settings.sampling} onChange={(e) => setSettings({ ...settings, sampling: Number(e.target.value) })} />
                </div>
              </div>

              <div className="space-y-2">
                <Label htmlFor="retentionDays">Retention days</Label>
                <Input id="retentionDays" type="number" min={1} max={365} value={settings.retentionDays} onChange={(e) => setSettings({ ...settings, retentionDays: Number(e.target.value) })} />
              </div>

              <div className="flex justify-end gap-3">
                <Button type="button" variant="outline" onClick={() => router.push('/admin')}>Back</Button>
                <Button type="submit" disabled={saving}>{saving ? 'Saving...' : 'Save settings'}</Button>
              </div>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
