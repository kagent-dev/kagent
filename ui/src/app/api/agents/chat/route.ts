import { NextResponse } from 'next/server';

// Mock multi-agent chat fanout. In production, call your backend orchestrator.
// Request: { message: string, agentRefs: string[], model?: string, models?: string[] }
// Response: { results: Array<{ agent: string, content: string, model?: string, ts: string }> }
export async function POST(request: Request) {
  try {
    const body = await request.json();
    const { message, agentRefs, model, models } = body || {};
    if (typeof message !== 'string' || !Array.isArray(agentRefs) || agentRefs.length === 0) {
      return NextResponse.json({ message: 'Invalid payload' }, { status: 400 });
    }
    const now = new Date().toISOString();
    let results: Array<{ agent: string; content: string; model?: string; ts: string }> = [];
    const modelsArray: string[] | undefined = Array.isArray(models) && models.length > 0 ? models : (model ? [model] : undefined);

    if (modelsArray && modelsArray.length > 0) {
      for (const ref of agentRefs) {
        for (const m of modelsArray) {
          results.push({
            agent: ref,
            model: m,
            ts: now,
            content: `(${ref} • ${m}) echo: ${message}`,
          });
        }
      }
    } else {
      results = agentRefs.map((ref: string) => ({
        agent: ref,
        model: undefined,
        ts: now,
        content: `(${ref}) echo: ${message}`,
      }));
    }
    return NextResponse.json({ results });
  } catch (e) {
    return NextResponse.json({ message: 'Invalid request' }, { status: 400 });
  }
}
