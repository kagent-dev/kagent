import { NextResponse } from 'next/server';

// Mock SRE data: SLIs over time, SLO targets, and SLA summaries
function genSeries(len = 30, base = 99.9, variance = 0.2) {
  const now = Date.now();
  return Array.from({ length: len }).map((_, i) => ({
    t: new Date(now - (len - 1 - i) * 24 * 60 * 60 * 1000).toISOString(),
    v: Math.max(0, base + (Math.random() * variance - variance / 2)),
  }));
}

const sre = {
  slos: {
    availabilityPct: 99.9,
    p95LatencyMs: 500,
    errorRatePct: 0.1,
  },
  slis: {
    availabilityPct: genSeries(30, 99.93, 0.1),
    p95LatencyMs: genSeries(30, 450, 80),
    errorRatePct: genSeries(30, 0.08, 0.05),
  },
  sla: {
    plan: 'Pro',
    monthlyUptimePct: 99.9,
    responseTimeSLO: 'P95 < 500ms',
    supportResponse: '24h',
  },
};

export async function GET() {
  return NextResponse.json(sre);
}
