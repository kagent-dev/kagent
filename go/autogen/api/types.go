package api

import (
	"encoding/json"
)

type Component struct {
	Provider         string                 `json:"provider"`
	ComponentType    string                 `json:"component_type"`
	Version          int                    `json:"version"`
	ComponentVersion int                    `json:"component_version"`
	Description      string                 `json:"description"`
	Label            string                 `json:"label"`
	Config           map[string]interface{} `json:"config"`
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
