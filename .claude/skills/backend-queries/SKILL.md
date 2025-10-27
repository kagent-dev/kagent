---
name: Backend Queries
description: Write database queries using GORM for the Kagent backend with type-safe patterns and proper error handling. Use this skill when implementing database CRUD operations in go/internal/database/service.go, when using the generic query functions (list[T], get[T], save[T], delete[T]), when writing custom query logic with GORM's query builder, when implementing user isolation by filtering with UserID, when handling "not found" scenarios by returning nil instead of errors, when using parameterized queries to prevent SQL injection, when working with context.Context for request cancellation, when implementing transactions for related database operations, when adding WHERE clauses for filtering, when using query builder methods like WithContext(), Where(), First(), Find(), Create(), Updates(), or Delete(). This skill ensures queries are secure, performant, and follow the project's patterns for error handling and user data isolation.
---

## When to use this skill

- When implementing database CRUD operations in `go/internal/database/service.go`
- When using generic query functions (`list[T]`, `get[T]`, `save[T]`, `delete[T]`)
- When writing custom queries with GORM's chainable query builder
- When filtering queries by `UserID` for multi-tenant data isolation
- When handling "not found" scenarios (return `nil, nil` not errors)
- When using `context.Context` with `db.WithContext(ctx)` for cancellation
- When implementing transactions for related database operations
- When adding WHERE clauses with parameterized queries
- When distinguishing between `gorm.ErrRecordNotFound` and actual errors
- When implementing conditional query building with filters
- When working with query methods like `First()`, `Find()`, `Create()`, `Updates()`, `Delete()`
- When implementing pagination with `Offset()` and `Limit()`
- When selecting specific fields to optimize query performance

# Backend Queries

This Skill provides Claude Code with specific guidance on how to adhere to coding standards as they relate to how it should handle backend queries.

## Instructions

For details, refer to the information provided in this file:
[backend queries](../../../agent-os/standards/backend/queries.md)
