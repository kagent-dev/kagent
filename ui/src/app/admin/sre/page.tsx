'use client';

import { useEffect, useMemo, useState } from 'react';
import { useAuth } from '@/hooks/useAuth';
import { useRouter } from 'next/navigation';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';

interface Point { t: string; v: number }
interface SreData {
  slos: {
    availabilityPct: number;
    p95LatencyMs: number;
    errorRatePct: number;
  };
  slis: {
    availabilityPct: Point[];
    p95LatencyMs: Point[];
    errorRatePct: Point[];
  };
  sla: {
    plan: string;
    monthlyUptimePct: number;
    responseTimeSLO: string;
    supportResponse: string;
  };
}

function LineChart({ data, color = '#7c3aed', height = 140 }: { data: Point[]; color?: string; height?: number }) {
  const { points } = useMemo(() => {
    if (!data || data.length === 0) return { points: '' };
    const vals = data.map(d => d.v);
    const min = Math.min(...vals);
    const max = Math.max(...vals);
    const range = max - min || 1;
    const w = 360;
    const h = height;
    const step = w / Math.max(1, data.length - 1);
    const pts = data.map((d, i) => {
      const x = i * step;
      const y = h - ((d.v - min) / range) * h;
      return `${x},${y}`;
    }).join(' ');
    return { points: pts };
  }, [data, height]);

  return (
    <svg viewBox={`0 0 360 ${height}`} className="w-full h-[140px]">
      <polyline fill="none" stroke={color} strokeWidth="2" points={points} />
    </svg>
  );
}

export default function AdminSrePage() {
  const { isAuthenticated, user } = useAuth();
  const router = useRouter();
  const [loading, setLoading] = useState(true);
  const [sre, setSre] = useState<SreData | null>(null);

  useEffect(() => {
    if (!isAuthenticated) return;
    if (user?.role !== 'admin') return;
    (async () => {
      try {
        const res = await fetch('/api/admin/sre', { headers: { 'Authorization': `Bearer ${localStorage.getItem('auth_token')}` } });
        if (!res.ok) throw new Error('Failed to load SRE data');
        const data = await res.json();
        setSre(data);
      } finally {
        setLoading(false);
      }
    })();
  }, [isAuthenticated, user]);

  if (!isAuthenticated) {
    return <div className="container mx-auto px-4 py-8"><p>Please login.</p></div>;
  }
  if (user?.role !== 'admin') {
    return <div className="container mx-auto px-4 py-8"><p>Unauthorized.</p></div>;
  }

  return (
    <div className="container mx-auto px-4 py-8 space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">SRE</h1>
          <p className="text-muted-foreground">SLAs, SLOs, and SLIs overview</p>
        </div>
        <Button variant="outline" onClick={() => router.push('/admin/pricing')}>Pricing & Usage</Button>
      </div>

      {loading || !sre ? (
        <div>Loading...</div>
      ) : (
        <>
          {/* SLA Summary */}
          <div className="grid md:grid-cols-3 gap-4">
            <Card>
              <CardHeader>
                <CardDescription>Plan</CardDescription>
                <CardTitle className="text-xl">{sre.sla.plan}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">Monthly Uptime: {sre.sla.monthlyUptimePct}%</div>
                <div className="text-sm text-muted-foreground">Response SLO: {sre.sla.responseTimeSLO}</div>
                <div className="text-sm text-muted-foreground">Support Response: {sre.sla.supportResponse}</div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardDescription>Target SLOs</CardDescription>
                <CardTitle className="text-xl">Targets</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">Availability ≥ {sre.slos.availabilityPct}%</div>
                <div className="text-sm text-muted-foreground">P95 Latency ≤ {sre.slos.p95LatencyMs} ms</div>
                <div className="text-sm text-muted-foreground">Error Rate ≤ {sre.slos.errorRatePct}%</div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardDescription>Overall Status</CardDescription>
                <CardTitle className="text-xl">{sre.sla.monthlyUptimePct >= sre.slos.availabilityPct ? 'On Track' : 'At Risk'}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="text-sm text-muted-foreground">Compare SLIs to targets below</div>
              </CardContent>
            </Card>
          </div>

          {/* SLI Charts */}
          <div className="grid md:grid-cols-3 gap-6">
            <Card>
              <CardHeader>
                <CardTitle>Availability (%)</CardTitle>
                <CardDescription>Last 30 days</CardDescription>
              </CardHeader>
              <CardContent>
                <LineChart data={sre.slis.availabilityPct} color="#22c55e" />
                <div className="text-xs text-muted-foreground mt-2">Target ≥ {sre.slos.availabilityPct}%</div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>P95 Latency (ms)</CardTitle>
                <CardDescription>Last 30 days</CardDescription>
              </CardHeader>
              <CardContent>
                <LineChart data={sre.slis.p95LatencyMs} color="#3b82f6" />
                <div className="text-xs text-muted-foreground mt-2">Target ≤ {sre.slos.p95LatencyMs} ms</div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>Error Rate (%)</CardTitle>
                <CardDescription>Last 30 days</CardDescription>
              </CardHeader>
              <CardContent>
                <LineChart data={sre.slis.errorRatePct} color="#ef4444" />
                <div className="text-xs text-muted-foreground mt-2">Target ≤ {sre.slos.errorRatePct}%</div>
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </div>
  );
}
