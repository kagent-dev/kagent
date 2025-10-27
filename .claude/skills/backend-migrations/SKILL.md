---
name: Backend Migrations
description: Manage database schema changes using GORM's AutoMigrate feature for the Kagent backend. Use this skill when adding new database models to go/internal/database/models.go, when modifying existing model structs by adding or changing fields, when adding indexes or constraints to GORM model tags, when updating the MigrateModels() function to include new models, when working with database schema evolution, when defining GORM model structs with proper field types and tags, when implementing TableName() methods for explicit table naming, or when handling database migrations during application startup. This skill applies to both SQLite (development) and PostgreSQL (production) database configurations.
---

## When to use this skill

- When adding new database models to `go/internal/database/models.go`
- When modifying existing model structs (adding fields, changing types, adding indexes)
- When updating the `MigrateModels()` function to register new models
- When adding GORM tags for constraints (`gorm:"not null"`, `gorm:"index"`, `gorm:"unique"`)
- When implementing `TableName()` methods for cross-language table name compatibility
- When working with database schema evolution and additive changes
- When setting up composite unique indexes using GORM tags
- When adding JSON fields using `datatypes.JSON` for flexible schemas
- When implementing user isolation with `UserID` fields and indexes
- When handling schema changes that work with both SQLite and PostgreSQL
- When debugging migration issues or understanding the auto-migration process

# Backend Migrations

This Skill provides Claude Code with specific guidance on how to adhere to coding standards as they relate to how it should handle backend migrations.

## Instructions

For details, refer to the information provided in this file:
[backend migrations](../../../agent-os/standards/backend/migrations.md)
