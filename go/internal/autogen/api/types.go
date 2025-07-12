package api

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

// JSONMap is a custom type for handling JSON columns in GORM
type JSONMap map[string]interface{}

// Scan implements the sql.Scanner interface
func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = make(JSONMap)
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to scan JSONMap: value is not []byte")
	}

	return json.Unmarshal(bytes, j)
}

// Value implements the driver.Valuer interface
func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

type Component struct {
	Provider         string  `json:"provider"`
	ComponentType    string  `json:"component_type"`
	Version          int     `json:"version"`
	ComponentVersion int     `json:"component_version"`
	Description      string  `json:"description"`
	Label            string  `json:"label"`
	Config           JSONMap `gorm:"type:json" json:"config"`
}

// Scan implements the sql.Scanner interface
func (c *Component) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("failed to scan Component: value is not []byte")
	}

	return json.Unmarshal(bytes, c)
}

// Value implements the driver.Valuer interface
func (c Component) Value() (driver.Value, error) {
	return json.Marshal(c)
}

func (c *Component) ToConfig() (map[string]interface{}, error) {
	if c == nil {
		return nil, nil
	}

	return toConfig(c)
}

func MustToConfig(c ComponentConfig) map[string]interface{} {
	config, err := c.ToConfig()
	if err != nil {
		panic(err)
	}
	return config
}

func MustFromConfig(c ComponentConfig, config map[string]interface{}) {
	err := c.FromConfig(config)
	if err != nil {
		panic(err)
	}
}

type ComponentConfig interface {
	ToConfig() (map[string]interface{}, error)
	FromConfig(map[string]interface{}) error
}

func toConfig(c any) (map[string]interface{}, error) {
	byt, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{})
	err = json.Unmarshal(byt, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func fromConfig(c any, config map[string]interface{}) error {
	byt, err := json.Marshal(config)
	if err != nil {
		return err
	}

	return json.Unmarshal(byt, c)
}
