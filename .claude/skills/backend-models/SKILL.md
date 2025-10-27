---
name: Backend Models
description: Define and structure database models using GORM for the Kagent backend. Use this skill when creating new database model structs in go/internal/database/models.go, when defining table schemas with proper field types and GORM tags, when working with the gorm.Model base fields (ID, CreatedAt, UpdatedAt, DeletedAt), when implementing TableName() methods for explicit table naming, when adding indexes for frequently queried fields, when using datatypes.JSON for flexible nested data structures like agent specifications or status information, when implementing user isolation patterns with UserID fields, when defining relationships between models, or when choosing appropriate data types for different kinds of data (strings, JSON, timestamps, etc.). This skill covers model definition patterns for agents, sessions, tasks, model configs, checkpoints, and other Kagent resources.
---

## When to use this skill

- When creating new database model structs in `go/internal/database/models.go`
- When defining table schemas with GORM field tags and constraints
- When working with `gorm.Model` for standard ID and timestamp fields
- When implementing `TableName()` methods for explicit table naming
- When adding indexes to fields using `gorm:"index"` tags
- When creating composite unique indexes for multi-column constraints
- When using `datatypes.JSON` for flexible/nested data (specs, status, metadata)
- When implementing user isolation with `UserID string` fields and indexes
- When choosing appropriate Go data types for model fields
- When adding NOT NULL constraints via `gorm:"not null"` tags
- When defining models for new Kagent resources (agents, sessions, tasks, etc.)
- When working with model relationships (if needed in the future)

# Backend Models

This Skill provides Claude Code with specific guidance on how to adhere to coding standards as they relate to how it should handle backend models.

## Instructions

For details, refer to the information provided in this file:
[backend models](../../../agent-os/standards/backend/models.md)
