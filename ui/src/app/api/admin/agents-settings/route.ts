import { NextResponse } from 'next/server';

// In-memory enabled settings for agents keyed by "namespace/name".
// true => enabled, false => disabled. If missing, default is enabled on the UI side.
let enabledMap: Record<string, boolean> = {};

export async function GET() {
  return NextResponse.json({ enabled: enabledMap });
}

export async function PUT(request: Request) {
  try {
    const body = await request.json();
    const { ref, enabled } = body || {};
    if (typeof ref !== 'string' || typeof enabled !== 'boolean') {
      return NextResponse.json({ message: 'Invalid payload' }, { status: 400 });
    }
    enabledMap[ref] = enabled;
    return NextResponse.json({ enabled: enabledMap });
  } catch (e) {
    return NextResponse.json({ message: 'Invalid request' }, { status: 400 });
  }
}
