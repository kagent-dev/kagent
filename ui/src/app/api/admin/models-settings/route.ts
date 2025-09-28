import { NextResponse } from 'next/server';

// In-memory enabled settings for models keyed by full model ref (e.g., namespace/name)
// true => enabled, false => disabled. Missing => default enabled on UI
let enabledModels: Record<string, boolean> = {};

export async function GET() {
  return NextResponse.json({ enabled: enabledModels });
}

export async function PUT(request: Request) {
  try {
    const body = await request.json();
    const { ref, enabled } = body || {};
    if (typeof ref !== 'string' || typeof enabled !== 'boolean') {
      return NextResponse.json({ message: 'Invalid payload' }, { status: 400 });
    }
    enabledModels[ref] = enabled;
    return NextResponse.json({ enabled: enabledModels });
  } catch (e) {
    return NextResponse.json({ message: 'Invalid request' }, { status: 400 });
  }
}
