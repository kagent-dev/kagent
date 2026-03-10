#!/usr/bin/env node

import { createServer } from './server.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import { SSEServerTransport } from '@modelcontextprotocol/sdk/server/sse.js';
import http from 'http';

async function main() {
  // Check if we should run in HTTP mode (for development)
  const isHttpMode = process.argv.includes('--http') || process.env.MCP_HTTP_MODE === 'true';
  
  if (isHttpMode) {
    // HTTP mode for development
    const port = process.env.PORT ? parseInt(process.env.PORT, 10) : 3000;
    const host = process.env.HOST || 'localhost';

    try {
      const server = await createServer();
      
      const httpServer = http.createServer();
      
      // Set up SSE transport
      httpServer.on('request', async (req, res) => {
        if (req.url === '/mcp') {
          const transport = new SSEServerTransport('/mcp', res);
          await server.connect(transport);
        } else if (req.url === '/health') {
          res.writeHead(200, { 'Content-Type': 'text/plain' });
          res.end('OK');
        } else {
          res.writeHead(404);
          res.end('Not Found');
        }
      });

      httpServer.listen(port, host, () => {
        console.log(`🚀 MCP Server running on http://${host}:${port}`);
        console.log(`📊 Health check available at http://${host}:${port}/health`);
        console.log(`🔌 MCP endpoint available at http://${host}:${port}/mcp`);
      });

      // Graceful shutdown
      process.on('SIGINT', () => {
        console.log('\n🛑 Shutting down server...');
        httpServer.close(() => {
          console.log('✅ Server stopped');
          process.exit(0);
        });
      });

      process.on('SIGTERM', () => {
        console.log('\n🛑 Shutting down server...');
        httpServer.close(() => {
          console.log('✅ Server stopped');
          process.exit(0);
        });
      });

    } catch (error) {
      console.error('❌ Failed to start server:', error);
      process.exit(1);
    }
  } else {
    // Stdio mode for MCP (default)
    try {
      const server = await createServer();
      const transport = new StdioServerTransport();
      await server.connect(transport);
    } catch (error) {
      console.error('❌ Failed to start MCP server:', error);
      process.exit(1);
    }
  }
}

main().catch((error) => {
  console.error('❌ Unhandled error:', error);
  process.exit(1);
});
