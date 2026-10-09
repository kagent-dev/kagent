// Package env provides a centralized registry for environment variables used
// throughout kagent. Variables are self-registering: calling any Register*
// function records the variable's metadata (name, default, description, type,
// components) in a process-wide registry and returns a typed accessor.
// Supply every consuming component in one registration; at least one is required.
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
	ComponentUI           Component = "ui"
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
	// Components identifies the kagent components that use this variable.
	Components []Component `json:"components"`
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
	if len(v.Components) == 0 || slices.Contains(v.Components, Component("")) {
		panic(fmt.Sprintf("environment variable %s requires non-empty components", v.Name))
	}
	v.Components = slices.Clone(v.Components)
	slices.Sort(v.Components)
	v.Components = slices.Compact(v.Components)
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
		v.Components = slices.Clone(v.Components)
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
	v.Components = slices.Clone(v.Components)
	return v, ok
}

// ---------- StringVar ----------

// StringVar is a registered environment variable that holds a string value.
type StringVar struct {
	v Var
}

// RegisterStringVar registers a string environment variable and returns a typed accessor.
func RegisterStringVar(name, defaultValue, description string, components ...Component) StringVar {
	v := Var{
		Name:         name,
		DefaultValue: defaultValue,
		Description:  description,
		Type:         TypeString,
		Components:   components,
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
func RegisterBoolVar(name string, defaultValue bool, description string, components ...Component) BoolVar {
	v := Var{
		Name:         name,
		DefaultValue: strconv.FormatBool(defaultValue),
		Description:  description,
		Type:         TypeBool,
		Components:   components,
	}
	register(v)
	return BoolVar{v: v, defaultValue: defaultValue}
}

// Get returns the current value of the environment variable, or the default.
func (b BoolVar) Get() bool {
	value, _, _ := b.LookupWithError() // Invalid input uses the registered default.
	return value
}

// Lookup returns the value and whether a nonempty, valid value was set.
func (b BoolVar) Lookup() (bool, bool) {
	value, set, err := b.LookupWithError()
	return value, set && err == nil
}

// LookupWithError distinguishes invalid input from an unset or empty value.
// Boolean values are case-insensitive and ignore surrounding whitespace.
// Unset, empty, and invalid values return the registered default; invalid input
// additionally returns set=true and a parsing error.
func (b BoolVar) LookupWithError() (value, set bool, err error) {
	val, ok := os.LookupEnv(b.v.Name)
	val = strings.TrimSpace(val)
	if !ok || val == "" {
		return b.defaultValue, false, nil
	}
	parsed, err := strconv.ParseBool(strings.ToLower(val))
	if err != nil {
		return b.defaultValue, true, fmt.Errorf("failed to parse %s as a boolean: %w", b.v.Name, err)
	}
	return parsed, true, nil
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
func RegisterIntVar(name string, defaultValue int, description string, components ...Component) IntVar {
	v := Var{
		Name:         name,
		DefaultValue: strconv.Itoa(defaultValue),
		Description:  description,
		Type:         TypeInt,
		Components:   components,
	}
	register(v)
	return IntVar{v: v, defaultValue: defaultValue}
}

// Get returns the current value of the environment variable, or the default.
func (i IntVar) Get() int {
	value, _, _ := i.LookupWithError() // Invalid input uses the registered default.
	return value
}

// Lookup returns the value and whether a nonempty, valid value was set.
func (i IntVar) Lookup() (int, bool) {
	value, set, err := i.LookupWithError()
	return value, set && err == nil
}

// LookupWithError distinguishes invalid input from an unset or empty value.
// Integer values ignore surrounding whitespace. Unset, empty, and invalid values
// return the registered default; invalid input also returns set=true and an error.
func (i IntVar) LookupWithError() (value int, set bool, err error) {
	val, ok := os.LookupEnv(i.v.Name)
	val = strings.TrimSpace(val)
	if !ok || val == "" {
		return i.defaultValue, false, nil
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return i.defaultValue, true, fmt.Errorf("failed to parse %s as an integer: %w", i.v.Name, err)
	}
	return parsed, true, nil
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
func RegisterDurationVar(name string, defaultValue time.Duration, description string, components ...Component) DurationVar {
	v := Var{
		Name:         name,
		DefaultValue: defaultValue.String(),
		Description:  description,
		Type:         TypeDuration,
		Components:   components,
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

// Default returns the registered default.
func (d DurationVar) Default() time.Duration { return d.defaultValue }

// Name returns the environment variable name.
func (d DurationVar) Name() string { return d.v.Name }

// ---------- Formatting ----------

// ExportMarkdown generates a markdown document listing all registered variables.
func ExportMarkdown(component string) string {
	vars := VarDescriptions()
	var sb strings.Builder

	sb.WriteString("# Kagent Environment Variables\n\n")
	sb.WriteString("<!-- Generated by make env-docs. Do not edit directly. -->\n\n")
	sb.WriteString("Generated from `go/core/pkg/env`. Edit the registrations there, run `make env-docs`, " +
		"and commit the result. CI runs `make env-docs-check`.\n\n")
	sb.WriteString("This reference covers user-configurable settings for the controller, CLI, standalone agent runtimes, UI, and tests. " +
		"Controller-generated runtime payloads, credentials, private paths, and other internal process wiring are excluded. " +
		"Build scripts, sample applications, and third-party SDK settings not configured by kagent have their own documentation. " +
		"Defaults describe the application without deployment overrides; Helm or a Harness may supply different values. " +
		"`(none)` means no fixed default; see the description for required values and fallbacks. " +
		"Shared variables appear under each consuming component. " +
		"Only registered metadata is exported, never values from the current process environment.\n\n")

	// Group by component
	grouped := make(map[Component][]Var)
	for _, v := range vars {
		if v.Hidden {
			continue
		}
		for _, comp := range v.Components {
			if component != "" && component != "all" && string(comp) != component {
				continue
			}
			grouped[comp] = append(grouped[comp], v)
		}
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

	return strings.TrimSuffix(sb.String(), "\n")
}

// ExportJSON generates a JSON array of all registered variables.
func ExportJSON(component string) string {
	vars := VarDescriptions()
	out := make([]Var, 0, len(vars))
	for _, v := range vars {
		if v.Hidden {
			continue
		}
		if component != "" && component != "all" && !slices.Contains(v.Components, Component(component)) {
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
