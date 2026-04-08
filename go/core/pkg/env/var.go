// Package env provides a centralized registry for environment variables used
// throughout kagent. Variables are self-registering: calling any Register*
// function records the variable's metadata (name, default, description, type,
// component) in a process-wide registry and returns a typed accessor.
//
// This design is inspired by Istio's pkg/env package and enables automatic
// documentation generation via `kagent env`.
package env

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// VarType identifies the data type of an environment variable.
type VarType int

const (
	TypeString VarType = iota
	TypeBool
	TypeInt
	TypeFloat
	TypeDuration
)

// String returns the human-readable name of a VarType.
func (v VarType) String() string {
	switch v {
	case TypeString:
		return "String"
	case TypeBool:
		return "Boolean"
	case TypeInt:
		return "Integer"
	case TypeFloat:
		return "Floating-Point"
	case TypeDuration:
		return "Duration"
	default:
		return "Unknown"
	}
}

// MarshalJSON serializes VarType as its string representation.
func (v VarType) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.String())
}

// Component identifies which part of the kagent system consumes the variable.
type Component string

const (
	ComponentController   Component = "controller"
	ComponentCLI          Component = "cli"
	ComponentAgentRuntime Component = "agent-runtime"
	ComponentTesting      Component = "testing"
	ComponentDatabase     Component = "database"
)

// Var holds the metadata for a single registered environment variable.
type Var struct {
	// Name is the environment variable name (e.g. "KAGENT_NAMESPACE").
	Name string `json:"name"`
	// DefaultValue is the stringified default value.
	DefaultValue string `json:"default"`
	// Description explains what this variable controls.
	Description string `json:"description"`
	// Type is the data type.
	Type VarType `json:"type"`
	// Component identifies which kagent component uses this variable.
	Component Component `json:"component"`
	// Hidden, when true, excludes the variable from generated documentation.
	Hidden bool `json:"-"`
	// Deprecated, when true, marks the variable as deprecated in documentation.
	Deprecated bool `json:"deprecated"`
}

var (
	allVars = make(map[string]Var)
	mu      sync.Mutex
)

func register(v Var) {
	mu.Lock()
	defer mu.Unlock()
	allVars[v.Name] = v
}

// VarDescriptions returns all registered variables sorted by name.
func VarDescriptions() []Var {
	mu.Lock()
	defer mu.Unlock()

	out := make([]Var, 0, len(allVars))
	for _, v := range allVars {
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b Var) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

// VarByName returns the metadata for a registered variable, or false if not found.
func VarByName(name string) (Var, bool) {
	mu.Lock()
	defer mu.Unlock()
	v, ok := allVars[name]
	return v, ok
}

// ---------- StringVar ----------

// StringVar is a registered environment variable that holds a string value.
type StringVar struct {
	v Var
}

// RegisterStringVar registers a string environment variable and returns a typed accessor.
func RegisterStringVar(name, defaultValue, description string, component Component) StringVar {
	v := Var{
		Name:         name,
		DefaultValue: defaultValue,
		Description:  description,
		Type:         TypeString,
		Component:    component,
	}
	register(v)
	return StringVar{v: v}
}

// Get returns the current value of the environment variable, or the default.
func (s StringVar) Get() string {
	if val, ok := os.LookupEnv(s.v.Name); ok {
		return val
	}
	return s.v.DefaultValue
}

// Lookup returns the value and whether the variable was set.
func (s StringVar) Lookup() (string, bool) {
	val, ok := os.LookupEnv(s.v.Name)
	if !ok {
		return s.v.DefaultValue, false
	}
	return val, true
}

// Name returns the environment variable name.
func (s StringVar) Name() string { return s.v.Name }

// DefaultValue returns the default value.
func (s StringVar) DefaultValue() string { return s.v.DefaultValue }

// ---------- BoolVar ----------

// BoolVar is a registered environment variable that holds a boolean value.
type BoolVar struct {
	v            Var
	defaultValue bool
}

// RegisterBoolVar registers a boolean environment variable and returns a typed accessor.
func RegisterBoolVar(name string, defaultValue bool, description string, component Component) BoolVar {
	v := Var{
		Name:         name,
		DefaultValue: strconv.FormatBool(defaultValue),
		Description:  description,
		Type:         TypeBool,
		Component:    component,
	}
	register(v)
	return BoolVar{v: v, defaultValue: defaultValue}
}

// Get returns the current value of the environment variable, or the default.
func (b BoolVar) Get() bool {
	if val, ok := os.LookupEnv(b.v.Name); ok {
		parsed, err := strconv.ParseBool(val)
		if err == nil {
			return parsed
		}
	}
	return b.defaultValue
}

// Lookup returns the value and whether the variable was set.
func (b BoolVar) Lookup() (bool, bool) {
	val, ok := os.LookupEnv(b.v.Name)
	if !ok {
		return b.defaultValue, false
	}
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		return b.defaultValue, false
	}
	return parsed, true
}

// Name returns the environment variable name.
func (b BoolVar) Name() string { return b.v.Name }

// ---------- IntVar ----------

// IntVar is a registered environment variable that holds an integer value.
type IntVar struct {
	v            Var
	defaultValue int
}

// RegisterIntVar registers an integer environment variable and returns a typed accessor.
func RegisterIntVar(name string, defaultValue int, description string, component Component) IntVar {
	v := Var{
		Name:         name,
		DefaultValue: strconv.Itoa(defaultValue),
		Description:  description,
		Type:         TypeInt,
		Component:    component,
	}
	register(v)
	return IntVar{v: v, defaultValue: defaultValue}
}

// Get returns the current value of the environment variable, or the default.
func (i IntVar) Get() int {
	if val, ok := os.LookupEnv(i.v.Name); ok {
		parsed, err := strconv.Atoi(val)
		if err == nil {
			return parsed
		}
	}
	return i.defaultValue
}

// Lookup returns the value and whether the variable was set.
func (i IntVar) Lookup() (int, bool) {
	val, ok := os.LookupEnv(i.v.Name)
	if !ok {
		return i.defaultValue, false
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return i.defaultValue, false
	}
	return parsed, true
}

// Name returns the environment variable name.
func (i IntVar) Name() string { return i.v.Name }

// ---------- DurationVar ----------

// DurationVar is a registered environment variable that holds a time.Duration value.
type DurationVar struct {
	v            Var
	defaultValue time.Duration
}

// RegisterDurationVar registers a duration environment variable and returns a typed accessor.
func RegisterDurationVar(name string, defaultValue time.Duration, description string, component Component) DurationVar {
	v := Var{
		Name:         name,
		DefaultValue: defaultValue.String(),
		Description:  description,
		Type:         TypeDuration,
		Component:    component,
	}
	register(v)
	return DurationVar{v: v, defaultValue: defaultValue}
}

// Get returns the current value of the environment variable, or the default.
func (d DurationVar) Get() time.Duration {
	if val, ok := os.LookupEnv(d.v.Name); ok {
		parsed, err := time.ParseDuration(val)
		if err == nil {
			return parsed
		}
	}
	return d.defaultValue
}

// Lookup returns the value and whether the variable was set.
func (d DurationVar) Lookup() (time.Duration, bool) {
	val, ok := os.LookupEnv(d.v.Name)
	if !ok {
		return d.defaultValue, false
	}
	parsed, err := time.ParseDuration(val)
	if err != nil {
		return d.defaultValue, false
	}
	return parsed, true
}

// Name returns the environment variable name.
func (d DurationVar) Name() string { return d.v.Name }

// ---------- Formatting ----------

// ExportMarkdown generates a markdown document listing all registered variables.
func ExportMarkdown(component string) string {
	vars := VarDescriptions()
	var sb strings.Builder

	sb.WriteString("# Kagent Environment Variables\n\n")

	// Group by component
	grouped := make(map[Component][]Var)
	for _, v := range vars {
		if v.Hidden {
			continue
		}
		if component != "" && component != "all" && string(v.Component) != component {
			continue
		}
		grouped[v.Component] = append(grouped[v.Component], v)
	}

	// Sort component keys for deterministic output
	components := make([]Component, 0, len(grouped))
	for c := range grouped {
		components = append(components, c)
	}
	slices.SortFunc(components, func(a, b Component) int {
		return cmp.Compare(string(a), string(b))
	})

	for _, comp := range components {
		compVars := grouped[comp]
		fmt.Fprintf(&sb, "## %s\n\n", comp)
		sb.WriteString("| Variable | Type | Default | Description |\n")
		sb.WriteString("|----------|------|---------|-------------|\n")
		for _, v := range compVars {
			deprecated := ""
			if v.Deprecated {
				deprecated = " **(deprecated)**"
			}
			defaultVal := v.DefaultValue
			if defaultVal == "" {
				defaultVal = "(none)"
			}
			fmt.Fprintf(&sb, "| `%s` | %s | `%s` | %s%s |\n",
				v.Name, v.Type, defaultVal, v.Description, deprecated)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ExportJSON generates a JSON array of all registered variables.
func ExportJSON(component string) string {
	vars := VarDescriptions()
	out := make([]Var, 0, len(vars))
	for _, v := range vars {
		if v.Hidden {
			continue
		}
		if component != "" && component != "all" && string(v.Component) != component {
			continue
		}
		out = append(out, v)
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "[]\n"
	}
	return string(b) + "\n"
}
