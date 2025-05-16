import { NextResponse, NextRequest } from 'next/server';
import { getBackendUrl } from '@/lib/utils';
import { getCurrentUserId } from '@/app/actions/utils';


export async function POST(
  request: NextRequest,
  { params }: { params: Promise<{ sessionId: string }> }
) {
    const { sessionId } = await params;
    console.log("Received request to invoke stream");
  try {
    if (!sessionId) {
      return NextResponse.json({ error: 'Session ID is required' }, { status: 400 });
    }

    const userId = await getCurrentUserId(); 
    if (!userId) {
      return NextResponse.json({ error: 'User ID could not be determined' }, { status: 401 });
    }

    // Read the plain text content from the request body
    const content = await request.text();
    if (!content) {
      return NextResponse.json({ error: 'Content is required' }, { status: 400 });
    }

    const backendUrl = getBackendUrl();
    const targetUrl = `${backendUrl}/sessions/${sessionId}/invoke/stream?user_id=${userId}`;

    // Fetch from your actual backend
    const backendResponse = await fetch(targetUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'text/plain', 
        'Accept': 'text/event-stream', 
      },
      body: content,
      signal: request.signal,
    });

    if (!backendResponse.ok) {
      const errorText = await backendResponse.text();
      return new Response(errorText || 'Backend request failed', { status: backendResponse.status });
    }

    if (!backendResponse.body) {
      return new Response('Backend response body is null', { status: 500 });
    }

    const responseHeaders = new Headers({
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      'Connection': 'keep-alive',
    });

    const readableStream = backendResponse.body;
    return new Response(readableStream, {
      headers: responseHeaders,
      status: backendResponse.status,
    });

  } catch (error) {
    console.error('[API /invoke/stream] Error:', error);
    const errorMessage = error instanceof Error ? error.message : 'Internal server error';
    return NextResponse.json({ error: errorMessage }, { status: 500 });
  }
} 