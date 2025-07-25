package database

import (
	"fmt"

	"gorm.io/gorm"
)

type Model interface {
	TableName() string
}

type Clause struct {
	Key   string
	Value interface{}
}

func list[T Model](db *gorm.DB, clauses ...Clause) ([]T, error) {
	var models []T
	query := db

	for _, clause := range clauses {
		query = query.Where(fmt.Sprintf("%s = ?", clause.Key), clause.Value)
	}

	err := query.Order("created_at ASC").Find(&models).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	return models, nil
}

func get[T Model](db *gorm.DB, clauses ...Clause) (*T, error) {
	var model T
	query := db

	for _, clause := range clauses {
		query = query.Where(fmt.Sprintf("%s = ?", clause.Key), clause.Value)
	}

	err := query.First(&model).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get model: %w", err)
	}
	return &model, nil
}

// TODO: Make this upsert actually idempotent
// args:
// - db: the database connection
// - model: the model to save
func save[T Model](db *gorm.DB, model *T) error {
	if err := db.Create(model).Error; err != nil {
		if err == gorm.ErrDuplicatedKey {
			return db.Save(model).Error
		}
		return fmt.Errorf("failed to create model: %w", err)
	}
	return nil
}

func delete[T Model](db *gorm.DB, clauses ...Clause) error {
	t := new(T)
	query := db

	for _, clause := range clauses {
		query = query.Where(fmt.Sprintf("%s = ?", clause.Key), clause.Value)
	}

	result := query.Delete(t)
	if result.Error != nil {
		return fmt.Errorf("failed to delete model: %w", result.Error)
	}
	return nil
}

// BuildWhereClause is deprecated, use individual Where clauses instead
func BuildWhereClause(clauses ...Clause) string {
	clausesStr := ""
	for idx, clause := range clauses {
		if idx > 0 {
			clausesStr += " AND "
		}
		clausesStr += fmt.Sprintf("%s = %v", clause.Key, clause.Value)
	}
	return clausesStr
}
