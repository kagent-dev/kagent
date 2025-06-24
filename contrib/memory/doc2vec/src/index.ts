#!/usr/bin/env node
// src/index.ts
import 'dotenv/config'; // Load .env file
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { SSEServerTransport } from "@modelcontextprotocol/sdk/server/sse.js";
import express, { Request, Response } from "express";
import { z } from "zod";
import { randomUUID } from 'crypto';

import * as sqliteVec from "sqlite-vec";
import Database, { Database as DatabaseType } from "better-sqlite3";
import { OpenAI } from 'openai';
import path from 'path';
import { fileURLToPath } from 'url';
import fs from 'fs'; // Import fs for checking file existence

// --- Configuration & Environment Check ---

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const openAIApiKey = process.env.OPENAI_API_KEY;
const dbDir = process.env.SQLITE_DB_DIR || __dirname; // Default to current dir if not set

const openai = new OpenAI({
    apiKey: openAIApiKey,
});

export interface QueryResult {
    chunk_id: string;
    distance: number;
    content: string;
    url?: string;
    embedding?: Float32Array | number[];
    [key: string]: unknown;
}

//TODO: add ollama support
async function createEmbeddings(text: string): Promise<number[]> {
    try {
        console.error("Calling OpenAI embeddings API...");
        const startTime = Date.now();
        const response = await openai.embeddings.create({
            model: 'text-embedding-3-large', // Or your preferred model
            input: text,
        });
        const duration = Date.now() - startTime;
        console.error(`OpenAI embeddings received in ${duration}ms.`);
        if (!response.data?.[0]?.embedding) {
            throw new Error("Failed to get embedding from OpenAI response.");
        }
        return response.data[0].embedding;
    } catch (error) {
        console.error("Error creating OpenAI embeddings:", error);
        throw new Error(`Failed to create embeddings: ${error instanceof Error ? error.message : String(error)}`);
    }
}

function queryCollection(queryEmbedding: number[], filter: { product_name: string; version?: string }, topK: number = 10): QueryResult[] {
    const dbPath = path.join(dbDir, `${filter.product_name}.db`);

    if (!fs.existsSync(dbPath)) {
        throw new Error(`Database file not found at ${dbPath}`);
    }

    let db: DatabaseType | null = null;
    try {
        db = new Database(dbPath);
        console.error(`[DB ${dbPath}] Opened connection.`);
        sqliteVec.load(db);
        console.error(`[DB ${dbPath}] sqliteVec loaded.`);
        let query = `
              SELECT
                  *,
                  distance
              FROM vec_items
              WHERE embedding MATCH @query_embedding`;
      
        if (filter.product_name) query += ` AND product_name = @product_name`;
        if (filter.version) query += ` AND version = @version`;
      
        query += `
              ORDER BY distance
              LIMIT @top_k;`;
      
        const stmt = db.prepare(query);
        console.error(`[DB ${dbPath}] Query prepared. Executing...`);
        const startTime = Date.now();
        const rows = stmt.all({
          query_embedding: new Float32Array(queryEmbedding),
          product_name: filter.product_name,
          version: filter.version,
          top_k: topK,
        });
        const duration = Date.now() - startTime;
        console.error(`[DB ${dbPath}] Query executed in ${duration}ms. Found ${rows.length} rows.`);
      
        rows.forEach((row: any) => {
          delete row.embedding;
        })
      
        return rows as QueryResult[];
    } catch (error) {
        console.error(`Error querying collection in ${dbPath}:`, error);
        throw new Error(`Database query failed: ${error instanceof Error ? error.message : String(error)}`);
    } finally {
        if (db) {
            db.close();
        }
    }
}

async function queryDocumentation(queryText: string, productName: string, version?: string, limit: number = 4): Promise<{ distance: number, content: string, url?: string }[]> {
    const queryEmbedding = await createEmbeddings(queryText);
    const results = queryCollection(queryEmbedding, { product_name: productName, version: version }, limit);
    return results.map((qr: QueryResult) => ({
        distance: qr.distance,
        content: qr.content,
        ...(qr.url && { url: qr.url }),
    }));
}

// --- MCP Server Setup ---
const serverName = "sqlite-vec-doc-query"; // Store name for logging
const serverVersion = "1.0.0"; // Store version for logging

const server = new McpServer({
    name: serverName,
    version: serverVersion,
    capabilities: {},
});

// --- Define the MCP Tool ---
server.tool(
    "query_Docs",
    "Query documentation stored in a sqlite-vec database using vector search.",
    {
        queryText: z.string().min(1).describe("The natural language query to search for."),
        productName: z.string().min(1).describe("The name of the product documentation database to search within (e.g., 'my-product'). Corresponds to the DB filename without .db."),
        version: z.string().optional().describe("The specific version of the product documentation (e.g., '1.2.0'). Optional."),
        limit: z.number().int().positive().optional().default(4).describe("Maximum number of results to return. Defaults to 4."),
    },
    async ({ queryText, productName, version, limit }: { queryText: string; productName: string; version?: string; limit: number }) => {
        console.error(`Received query: text="${queryText}", product="${productName}", version="${version || 'any'}", limit=${limit}`);

        try {
            const results = await queryDocumentation(queryText, productName, version, limit);

            if (results.length === 0) {
                return {
                    content: [{ type: "text", text: `No relevant documentation found for "${queryText}" in product "${productName}" ${version ? `(version ${version})` : ''}.` }],
                };
            }

            const formattedResults = results.map((r, index) =>
                [
                    `Result ${index + 1}:`,
                    `  Content: ${r.content}`,
                    `  Distance: ${r.distance.toFixed(4)}`,
                    r.url ? `  URL: ${r.url}` : null,
                    "---"
                ].filter(line => line !== null).join("\n")
            ).join("\n");

            const responseText = `Found ${results.length} relevant documentation snippets for "${queryText}" in product "${productName}" ${version ? `(version ${version})` : ''}:\n\n${formattedResults}`;
            console.error(`Handler finished processing. Payload size (approx): ${responseText.length} chars. Returning response object...`);

            return {
                content: [{ type: "text", text: responseText }],
            };
        } catch (error: any) {
            console.error("Error processing 'query_Docs' tool:", error);
            return {
                content: [{ type: "text", text: `Error querying documentation: ${error.message}` }],
            };
        }
    }
);

// --- Transport Setup ---
async function main() {
    const transport_type = process.env.TRANSPORT_TYPE || 'sse';
    
    if (transport_type === 'stdio') {
        // Stdio transport for direct communication
        console.error("Starting MCP server with stdio transport...");
        const transport = new StdioServerTransport();
        await server.connect(transport);
        console.error("MCP server connected via stdio.");
    } else if (transport_type === 'sse') {
        // SSE transport for backward compatibility
        console.error("Starting MCP server with SSE transport...");
        
        const app = express();
        
        // Storage for SSE transports by session ID
        const sseTransports: {[sessionId: string]: SSEServerTransport} = {};

        app.get("/sse", async (_: Request, res: Response) => {
            console.error('Received SSE connection request');
            const transport = new SSEServerTransport('/messages', res);
            sseTransports[transport.sessionId] = transport;
            res.on("close", () => {
                console.error(`SSE connection closed for session ${transport.sessionId}`);
                delete sseTransports[transport.sessionId];
            });
            await server.connect(transport);
        });

        app.post("/messages", async (req: Request, res: Response) => {
            console.error('Received SSE message POST request');
            const sessionId = req.query.sessionId as string;
            const transport = sseTransports[sessionId];
            if (transport) {
                await transport.handlePostMessage(req, res);
            } else {
                console.error(`No SSE transport found for sessionId: ${sessionId}`);
                res.status(400).send('No transport found for sessionId');
            }
        });

        const MCP_PORT = process.env.MCP_PORT || 8085;
        const webserver = app.listen(MCP_PORT, () => {
            console.error(`MCP server is running on port ${MCP_PORT} with SSE transport`);
            console.error(`Connect to: http://localhost:${MCP_PORT}/sse`);
        });
        
        webserver.keepAliveTimeout = 3000;
        
        // Keep the process alive
        webserver.on('error', (error) => {
            console.error('HTTP server error:', error);
        });
        
        // Handle server shutdown
        process.on('SIGINT', async () => {
            console.error('Shutting down SSE server...');
            
            // Close all active SSE transports
            for (const [sessionId, transport] of Object.entries(sseTransports)) {
                try {
                    console.error(`Closing SSE transport for session ${sessionId}`);
                    // SSE transports typically don't have a close method, cleanup happens via res.on("close")
                    delete sseTransports[sessionId];
                } catch (error) {
                    console.error(`Error cleaning up SSE transport for session ${sessionId}:`, error);
                }
            }

            console.error('SSE server shutdown complete');
            process.exit(0);
        });
        
        // Prevent the process from exiting
        process.stdin.resume();
    } else if (transport_type === 'http') {
        // Streamable HTTP transport for web-based communication
        console.error("Starting MCP server with HTTP transport...");
        
        const app = express();
        
        const transports: Map<string, StreamableHTTPServerTransport> = new Map<string, StreamableHTTPServerTransport>();
        
        // Handle POST requests for MCP initialization and method calls
        app.post('/mcp', async (req: Request, res: Response) => {
            console.error('Received MCP POST request');
            try {
                // Check for existing session ID
                const sessionId = req.headers['mcp-session-id'] as string | undefined;
                let transport: StreamableHTTPServerTransport;

                if (sessionId && transports.has(sessionId)) {
                    // Reuse existing transport
                    transport = transports.get(sessionId)!;
                } else if (!sessionId) {
                    // New initialization request
                    transport = new StreamableHTTPServerTransport({
                        sessionIdGenerator: () => randomUUID(),
                        onsessioninitialized: (sessionId: string) => {
                            // Store the transport by session ID when session is initialized
                            console.error(`Session initialized with ID: ${sessionId}`);
                            transports.set(sessionId, transport);
                        }
                    });

                    // Set up onclose handler to clean up transport when closed
                    transport.onclose = async () => {
                        const sid = transport.sessionId;
                        if (sid && transports.has(sid)) {
                            console.error(`Transport closed for session ${sid}, removing from transports map`);
                            transports.delete(sid);
                        }
                    };

                    // Connect the transport to the MCP server BEFORE handling the request
                    await server.connect(transport);

                    await transport.handleRequest(req, res);
                    return; // Already handled
                } else {
                    // Invalid request - no session ID or not initialization request
                    res.status(400).json({
                        jsonrpc: '2.0',
                        error: {
                            code: -32000,
                            message: 'Bad Request: No valid session ID provided',
                        },
                        id: req?.body?.id,
                    });
                    return;
                }

                // Handle the request with existing transport
                await transport.handleRequest(req, res);
            } catch (error) {
                console.error('Error handling MCP request:', error);
                if (!res.headersSent) {
                    res.status(500).json({
                        jsonrpc: '2.0',
                        error: {
                            code: -32603,
                            message: 'Internal server error',
                        },
                        id: req?.body?.id,
                    });
                }
            }
        });

        // Handle GET requests for SSE streams
        app.get('/mcp', async (req: Request, res: Response) => {
            console.error('Received MCP GET request');
            const sessionId = req.headers['mcp-session-id'] as string | undefined;
            if (!sessionId || !transports.has(sessionId)) {
                res.status(400).json({
                    jsonrpc: '2.0',
                    error: {
                        code: -32000,
                        message: 'Bad Request: No valid session ID provided',
                    },
                    id: req?.body?.id,
                });
                return;
            }

            // Check for Last-Event-ID header for resumability
            const lastEventId = req.headers['last-event-id'] as string | undefined;
            if (lastEventId) {
                console.error(`Client reconnecting with Last-Event-ID: ${lastEventId}`);
            } else {
                console.error(`Establishing new SSE stream for session ${sessionId}`);
            }

            const transport = transports.get(sessionId);
            await transport!.handleRequest(req, res);
        });

        // Handle DELETE requests for session termination
        app.delete('/mcp', async (req: Request, res: Response) => {
            const sessionId = req.headers['mcp-session-id'] as string | undefined;
            if (!sessionId || !transports.has(sessionId)) {
                res.status(400).json({
                    jsonrpc: '2.0',
                    error: {
                        code: -32000,
                        message: 'Bad Request: No valid session ID provided',
                    },
                    id: req?.body?.id,
                });
                return;
            }

            console.error(`Received session termination request for session ${sessionId}`);

            try {
                const transport = transports.get(sessionId);
                await transport!.handleRequest(req, res);
            } catch (error) {
                console.error('Error handling session termination:', error);
                if (!res.headersSent) {
                    res.status(500).json({
                        jsonrpc: '2.0',
                        error: {
                            code: -32603,
                            message: 'Error handling session termination',
                        },
                        id: req?.body?.id,
                    });
                }
            }
        });
        
        const MCP_PORT = process.env.MCP_PORT || 8085;
        const webserver = app.listen(MCP_PORT, () => {
            console.error(`MCP server is running on port ${MCP_PORT} with HTTP transport`);
            console.error(`Connect to: http://localhost:${MCP_PORT}/mcp`);
        });
        
        webserver.keepAliveTimeout = 60000; // Set keep-alive timeout to 60 seconds
        
        // Keep the process alive
        webserver.on('error', (error) => {
            console.error('HTTP server error:', error);
        });
        
        // Handle server shutdown
        process.on('SIGINT', async () => {
            console.error('Shutting down server...');

            // Close all active transports to properly clean up resources
            for (const [sessionId, transport] of transports) {
                try {
                    console.error(`Closing transport for session ${sessionId}`);
                    await transport.close();
                    transports.delete(sessionId);
                } catch (error) {
                    console.error(`Error closing transport for session ${sessionId}:`, error);
                }
            }

            console.error('Server shutdown complete');
            process.exit(0);
        });
        
        // Prevent the process from exiting
        process.stdin.resume();
    } else {
        console.error(`Unknown transport type: ${transport_type}. Use 'stdio', 'sse', or 'http'.`);
        process.exit(1);
    }
}

// Run main when this module is executed directly
main().catch((error) => {
    console.error("Failed to start MCP server:", error);
    process.exit(1);
});