import { NextRequest, NextResponse } from 'next/server';
import { getBackendUrl } from '@/lib/utils';

export async function POST(
  request: NextRequest,
  { params }: { params: Promise<{ namespace: string; agentName: string }> }
) {
  const { namespace, agentName } = await params;

  try {
    const a2aRequest = await request.json();

    const backendUrl = getBackendUrl();
    const targetUrl = `${backendUrl}/a2a/${namespace}/${agentName}/`;

    const backendResponse = await fetch(targetUrl, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Accept': 'text/event-stream',
        'Cache-Control': 'no-cache',
        'Connection': 'keep-alive',
        'User-Agent': 'kagent-ui',
      },
      body: JSON.stringify(a2aRequest),
    });

    if (!backendResponse.ok) {
      const errorText = await backendResponse.text();
      return new Response(errorText || 'Backend request failed', { 
        status: backendResponse.status,
        headers: {
          'Content-Type': 'text/plain',
        }
      });
    }

    if (!backendResponse.body) {
      return new Response('Backend response body is null', { status: 500 });
    }

    // Stream the response back to the frontend
    const responseHeaders = new Headers({
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      'Connection': 'keep-alive',
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type, Authorization, Accept',
    });

    const KEEP_ALIVE_INTERVAL_MS = 30000; // 30 seconds
    const stream = new ReadableStream({
      start(controller) {
        const reader = backendResponse.body!.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        let isClosed = false;
        let keepAliveTimer: NodeJS.Timeout | null = null;

        // Send keep-alive comment event and reschedule timer
        const sendKeepAlive = () => {
          if (!isClosed) {
            controller.enqueue(new TextEncoder().encode(': keep-alive\n\n'));
            resetKeepAliveTimer(); // Reschedule after sending keep-alive
          }
        };

        // Keep-alive timer logic
        const resetKeepAliveTimer = () => {
          if (keepAliveTimer) clearTimeout(keepAliveTimer);
          keepAliveTimer = setTimeout(sendKeepAlive, KEEP_ALIVE_INTERVAL_MS);
        };

        const pump = (): Promise<void> => {
          return reader.read().then(({ done, value }): Promise<void> => {
            if (done) {
              if (!isClosed) {
                if (keepAliveTimer) clearTimeout(keepAliveTimer);
                controller.close();
                isClosed = true;
                console.log('A2A Proxy: Backend connection closed.');
              }

              return Promise.resolve();
            }

            buffer += decoder.decode(value, { stream: true });

            // Process complete SSE events (delimited by \n\n)
            let eventEndIndex;
            while ((eventEndIndex = buffer.indexOf('\n\n')) >= 0) {
              const eventText = buffer.substring(0, eventEndIndex);
              buffer = buffer.substring(eventEndIndex + 2);

              if (eventText.trim()) {
                const eventData = eventText + '\n\n';
                if (!isClosed) {
                  controller.enqueue(new TextEncoder().encode(eventData));
                  resetKeepAliveTimer(); // Reset keep-alive timer whenever data is sent
                }
              }
            }

            return pump();
          }).catch(error => {
            console.error('A2A Proxy: Error in stream pump:', error);
            if (keepAliveTimer) clearTimeout(keepAliveTimer);
            
            if (!isClosed) {
              controller.error(error);
              isClosed = true;
            }

            return Promise.resolve();
          });
        };

        pump();
      }
    });

    return new Response(stream, {
      headers: responseHeaders,
      status: backendResponse.status,
    });

  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : 'Internal server error';
    return NextResponse.json({ error: errorMessage }, { status: 500 });
  }
}

export async function OPTIONS() {
  return new Response(null, {
    status: 200,
    headers: {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type, Authorization, Accept',
      'Access-Control-Max-Age': '86400',
    },
  });
}
