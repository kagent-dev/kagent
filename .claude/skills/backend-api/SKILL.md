---
name: Backend API
description: Design and implement RESTful API endpoints using Go, Gorilla Mux, and GORM for the Kagent backend. Use this skill when creating or modifying HTTP handlers in go/internal/httpserver/, when defining new API routes, when implementing request/response handling logic, when working with JSON encoding/decoding, when setting up middleware for error handling or CORS, when designing resource-based URLs, when implementing CRUD operations for agents, sessions, model configs, or other Kagent resources, when writing handler functions that interact with the database layer, or when defining API path constants and handler methods following the project's naming conventions (APIPath*, Handle*).
---

## When to use this skill

- When creating new API endpoints in `go/internal/httpserver/` directory
- When modifying existing HTTP handlers for agents, sessions, model configs, or other resources
- When implementing RESTful CRUD operations (GET, POST, PUT, PATCH, DELETE)
- When defining API route paths and handler method names
- When working with request/response JSON encoding and decoding
- When setting up error handling middleware or HTTP status codes
- When implementing request validation and input sanitization
- When working with query parameters, path parameters, or request headers
- When designing resource-based URL structures
- When implementing user isolation via `X-User-ID` headers
- When setting up CORS configuration for the web UI
- When integrating new handlers with the ServerConfig dependency injection pattern

# Backend API

This Skill provides Claude Code with specific guidance on how to adhere to coding standards as they relate to how it should handle backend API.

## Instructions

For details, refer to the information provided in this file:
[backend API](../../../agent-os/standards/backend/api.md)
