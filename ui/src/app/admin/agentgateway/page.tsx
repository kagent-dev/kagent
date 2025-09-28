'use client';

import { useEffect, useState } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { useRouter } from 'next/navigation';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { toast } from 'sonner';

interface Settings {
  enabled: boolean;
  title: string;
  description: string;
  themeColor: string;
  publicUrl: string;
  authMode: 'none' | 'token' | 'oauth';
}

export default function AgentGatewayAdminPage() {
  const { isAuthenticated, user } = useAuth();
  const router = useRouter();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [settings, setSettings] = useState<Settings>({
    enabled: false,
    title: 'Agent Gateway',
    description: '',
    themeColor: '#7c3aed',
    publicUrl: '',
    authMode: 'token',
  });

  useEffect(() => {
    if (!isAuthenticated) return;
    if (user?.role !== 'admin') return;
    (async () => {
      try {
        const res = await fetch('/api/admin/agentgateway', {
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
      const res = await fetch('/api/admin/agentgateway', {
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
        <h1 className="text-3xl font-bold">Agent Gateway</h1>
        <div className="text-sm text-muted-foreground">
          {settings.enabled ? 'Enabled' : 'Disabled'}
        </div>
      </div>

      <Card className="max-w-3xl">
        <CardHeader>
          <CardTitle>Configuration</CardTitle>
          <CardDescription>Enable, disable, and customize the Agent Gateway.</CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div>Loading...</div>
          ) : (
            <form onSubmit={onSave} className="space-y-6">
              <div className="flex items-center space-x-3">
                <Checkbox id="enabled" checked={settings.enabled} onCheckedChange={(v) => setSettings({ ...settings, enabled: Boolean(v) })} />
                <Label htmlFor="enabled">Enable Agent Gateway</Label>
              </div>

              <div className="grid md:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="title">Title</Label>
                  <Input id="title" value={settings.title} onChange={(e) => setSettings({ ...settings, title: e.target.value })} />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="themeColor">Theme color</Label>
                  <Input id="themeColor" type="color" value={settings.themeColor} onChange={(e) => setSettings({ ...settings, themeColor: e.target.value })} />
                </div>
              </div>

              <div className="space-y-2">
                <Label htmlFor="description">Description</Label>
                <Textarea id="description" rows={3} value={settings.description} onChange={(e) => setSettings({ ...settings, description: e.target.value })} />
              </div>

              <div className="grid md:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="publicUrl">Public URL</Label>
                  <Input id="publicUrl" placeholder="https://gateway.example.com" value={settings.publicUrl} onChange={(e) => setSettings({ ...settings, publicUrl: e.target.value })} />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="authMode">Auth mode</Label>
                  <select
                    id="authMode"
                    className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                    value={settings.authMode}
                    onChange={(e) => setSettings({ ...settings, authMode: e.target.value as Settings['authMode'] })}
                  >
                    <option value="none">None</option>
                    <option value="token">Token</option>
                    <option value="oauth">OAuth</option>
                  </select>
                </div>
              </div>

              <div className="flex justify-end gap-3">
                <Button type="button" variant="outline" onClick={() => router.push('/admin/users')}>Back</Button>
                <Button type="submit" disabled={saving}>{saving ? 'Saving...' : 'Save settings'}</Button>
              </div>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
