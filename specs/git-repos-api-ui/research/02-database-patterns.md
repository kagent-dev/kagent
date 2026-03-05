# Database Patterns

## ORM & Drivers
- GORM with SQLite (default) or Postgres
- AutoMigrate for schema evolution (alpha stage)

## Models (`go/pkg/database/models.go`)
- All models have `CreatedAt`, `UpdatedAt`, `DeletedAt` (soft delete)
- `TableName()` method defines table name
- JSON fields stored as `*SomeType` with GORM `serializer:json`

## Generic CRUD Helpers (`go/internal/database/service.go`)
```go
list[T Model](db *gorm.DB, clauses ...Clause) ([]T, error)
get[T Model](db *gorm.DB, clauses ...Clause) (*T, error)
save[T Model](db *gorm.DB, model *T) error     // Upsert via OnConflict
delete[T Model](db *gorm.DB, clauses ...Clause) error
```

## Clause Pattern
```go
Clause{Key: "id", Value: agentID}
Clause{Key: "user_id", Value: userID}
```

## Adding a New Entity
1. Define struct in `go/pkg/database/models.go` with GORM tags
2. Add `TableName()` method
3. Add to `Manager.Initialize()` AutoMigrate list (`go/internal/database/manager.go`)
4. Add methods to `Client` interface (`go/pkg/database/client.go`)
5. Implement in `clientImpl` (`go/internal/database/service.go`)

## Interface (`go/pkg/database/client.go`)
- Clean interface in `pkg/` (public)
- Implementation in `internal/database/` (private)
- Fake client in `go/internal/database/fake/client.go` for testing

## Transactions
```go
c.db.Transaction(func(tx *gorm.DB) error { ... })
```
