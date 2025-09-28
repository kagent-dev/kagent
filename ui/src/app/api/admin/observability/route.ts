import { NextResponse } from 'next/server';

// In-memory Observability settings (dev/demo only)
let observability = {
  enabled: false,
  provider: 'otel-collector' as 'otel-collector' | 'datadog' | 'honeycomb',
  endpoint: '',
  apiKey: '',
  sampling: 0.1, // 10%
  retentionDays: 7,
};

export async function GET() {
  return NextResponse.json(observability);
}

export async function PUT(request: Request) {
  try {
    const body = await request.json();
    observability = {
      ...observability,
      enabled: Boolean(body.enabled),
      provider: ['otel-collector', 'datadog', 'honeycomb'].includes(body.provider) ? body.provider : observability.provider,
      endpoint: typeof body.endpoint === 'string' ? body.endpoint : observability.endpoint,
      apiKey: typeof body.apiKey === 'string' ? body.apiKey : observability.apiKey,
      sampling: typeof body.sampling === 'number' ? Math.max(0, Math.min(1, body.sampling)) : observability.sampling,
      retentionDays: typeof body.retentionDays === 'number' ? Math.max(1, Math.min(365, body.retentionDays)) : observability.retentionDays,
    };
    return NextResponse.json(observability);
  } catch (e) {
    return NextResponse.json({ message: 'Invalid request' }, { status: 400 });
  }
}
