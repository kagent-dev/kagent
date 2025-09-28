import { NextResponse } from 'next/server';

// In-memory Agent Gateway settings (demo only)
let settings = {
  enabled: false,
  title: 'Agent Gateway',
  description: 'Public entry point to your agents with configurable branding and access.',
  themeColor: '#7c3aed',
  publicUrl: '',
  authMode: 'token' as 'none' | 'token' | 'oauth',
};

export async function GET() {
  return NextResponse.json(settings);
}

export async function PUT(request: Request) {
  try {
    const body = await request.json();
    // Basic validation and merge
    settings = {
      ...settings,
      enabled: Boolean(body.enabled),
      title: typeof body.title === 'string' ? body.title : settings.title,
      description: typeof body.description === 'string' ? body.description : settings.description,
      themeColor: typeof body.themeColor === 'string' ? body.themeColor : settings.themeColor,
      publicUrl: typeof body.publicUrl === 'string' ? body.publicUrl : settings.publicUrl,
      authMode: ['none','token','oauth'].includes(body.authMode) ? body.authMode : settings.authMode,
    };
    return NextResponse.json(settings);
  } catch (e) {
    return NextResponse.json({ message: 'Invalid request' }, { status: 400 });
  }
}
