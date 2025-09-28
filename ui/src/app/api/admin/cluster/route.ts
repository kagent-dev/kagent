import { NextResponse } from 'next/server';

// Demo/mock cluster info and timeseries
function generateSeries(len = 24, base = 50, variance = 20) {
  const now = Date.now();
  return Array.from({ length: len }).map((_, i) => ({
    t: new Date(now - (len - 1 - i) * 60 * 60 * 1000).toISOString(),
    v: Math.max(0, base + (Math.random() * variance - variance / 2)),
  }));
}

let cluster = {
  name: 'adolphe-cluster',
  region: 'us-central1',
  nodes: 5,
  agents: 12,
  cpuTotalCores: 20,
  cpuUsedCores: 8.4,
  memTotalGb: 64,
  memUsedGb: 28.2,
  estMonthlyCostUsd: 342.17,
  series: {
    cpu: generateSeries(24, 40, 30),
    mem: generateSeries(24, 45, 25),
    requests: generateSeries(24, 120, 80),
  },
};

export async function GET() {
  return NextResponse.json(cluster);
}
